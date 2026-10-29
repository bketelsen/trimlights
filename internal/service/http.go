// Package service provides the controller-owning process and its local API.
package service

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"trimlights/internal/auth"
	"trimlights/internal/storage"
	"trimlights/internal/webui"
)

type Config struct {
	Listen, DataDir, APIToken string
	Controller                Controller
	ScheduleOffset            int
	OIDC                      *auth.Config
}

func Validate(c Config) error {
	host, _, err := net.SplitHostPort(c.Listen)
	if err != nil {
		return err
	}
	ip := net.ParseIP(host)
	if c.OIDC != nil {
		if c.APIToken != "" {
			return errors.New("OIDC and local bearer authentication cannot be combined")
		}
		if err := c.OIDC.Validate(); err != nil {
			return err
		}
	} else {
		if ip == nil || !ip.IsLoopback() {
			return errors.New("development API must listen on a literal loopback address until household login is configured")
		}
		if len(c.APIToken) < 32 {
			return errors.New("API token must contain at least 32 characters")
		}
	}
	if c.DataDir == "" {
		return errors.New("data directory is required")
	}
	return nil
}

func Handler(store *storage.Store, worker *Worker, token string) http.Handler {
	mux := apiHandler(store, worker)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			header := r.Header.Get("Authorization")
			if !strings.HasPrefix(header, "Bearer ") || subtle.ConstantTimeCompare([]byte(strings.TrimPrefix(header, "Bearer ")), []byte(token)) != 1 {
				jsonError(w, 401, "authentication required")
				return
			}
		}
		mux.ServeHTTP(w, r)
	})
}

func apiHandler(store *storage.Store, worker *Worker) http.Handler {
	mux := http.NewServeMux()
	registerPatterns(mux, store)
	registerSchedules(mux, store)
	registerHouse(mux, store)
	registerControls(mux, worker)
	mux.Handle("GET /", webui.Handler())
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { jsonResponse(w, 200, map[string]string{"status": "ok"}) })
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		if _, err := store.Observation(r.Context()); err != nil {
			jsonError(w, 503, "storage unavailable")
			return
		}
		jsonResponse(w, 200, map[string]string{"status": "ready"})
	})
	mux.HandleFunc("GET /api/v1/status", func(w http.ResponseWriter, r *http.Request) {
		state, err := store.Observation(r.Context())
		if err != nil {
			jsonError(w, 500, "cannot read state")
			return
		}
		var age *float64
		if state.Snapshot != nil {
			seconds := time.Since(state.Snapshot.QueriedAt).Seconds()
			age = &seconds
		}
		controls, reason := worker.ControlAvailability(state)
		jsonResponse(w, 200, map[string]any{"observation": state, "worker": worker.Status(), "snapshot_age_seconds": age, "capabilities": map[string]any{"snapshot": worker.controller != nil, "clock_sync_possible": true, "power": controls, "pattern_apply": controls, "native_controls": controls, "schedule_write": false, "reason": reason}})
	})
	mux.HandleFunc("GET /api/v1/operations", func(w http.ResponseWriter, r *http.Request) {
		ops, err := store.Operations(r.Context())
		if err != nil {
			jsonError(w, 500, "cannot read operation history")
			return
		}
		jsonResponse(w, 200, map[string]any{"operations": ops})
	})
	mux.HandleFunc("POST /api/v1/controller/refresh", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Content-Type") != "application/json" {
			jsonError(w, 415, "Content-Type must be application/json")
			return
		}
		var input struct {
			AcknowledgeClockSync bool `json:"acknowledge_clock_sync"`
		}
		d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024))
		d.DisallowUnknownFields()
		if err := d.Decode(&input); err != nil || !input.AcknowledgeClockSync {
			jsonError(w, 400, "acknowledge_clock_sync must be true")
			return
		}
		if err := d.Decode(new(any)); err != io.EOF {
			jsonError(w, 400, "expected one JSON object")
			return
		}
		actor := "local-api"
		if identity, ok := auth.Identity(r.Context()); ok {
			actor = identity.Subject
		}
		id, done, err := worker.Enqueue(r.Context(), actor)
		if err != nil {
			jsonError(w, 503, err.Error())
			return
		}
		select {
		case err = <-done:
			if err != nil {
				jsonResponse(w, 502, map[string]any{"error": err.Error(), "operation_id": id})
				return
			}
			jsonResponse(w, 200, map[string]string{"operation_id": id, "status": "verified"})
		case <-r.Context().Done():
			return
		}
	})
	return mux
}
func jsonResponse(w http.ResponseWriter, code int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(value)
}
func jsonError(w http.ResponseWriter, code int, message string) {
	jsonResponse(w, code, map[string]string{"error": message})
}

// RestoreGuard is deliberately independent of runtime host flags and environment.
func RestoreGuard(dir string) (bool, error) {
	_, err := os.Lstat(filepath.Join(dir, "controller-disabled"))
	if os.IsNotExist(err) {
		return false, nil
	}
	return err == nil, err
}
func Run(ctx context.Context, c Config) error {
	guarded, err := RestoreGuard(c.DataDir)
	if err != nil {
		return err
	}
	if guarded {
		c.Controller = nil
	}

	if err := Validate(c); err != nil {
		return err
	}
	store, err := storage.Open(c.DataDir)
	if err != nil {
		return err
	}
	defer store.Close()
	if err := store.RecoverControlIntents(ctx); err != nil {
		return err
	}
	worker := NewWorker(store, c.Controller, c.ScheduleOffset)
	var handler http.Handler = Handler(store, worker, c.APIToken)
	if c.OIDC != nil {
		login, err := auth.New(ctx, *c.OIDC, store)
		if err != nil {
			return err
		}
		api := apiHandler(store, worker)
		household := login.Handler(api)
		handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/healthz" || r.URL.Path == "/readyz" {
				w.Header().Set("Cache-Control", "no-store")
				api.ServeHTTP(w, r)
				return
			}
			household.ServeHTTP(w, r)
		})
	}
	workerCtx, cancel := context.WithCancel(ctx)
	workerDone := make(chan struct{})
	go func() { defer close(workerDone); worker.Run(workerCtx) }()
	defer func() { cancel(); <-workerDone }()
	listener, err := net.Listen("tcp", c.Listen)
	if err != nil {
		return err
	}
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 60 * time.Second, WriteTimeout: 90 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10, BaseContext: func(net.Listener) context.Context { return ctx }}
	serveDone := make(chan error, 1)
	go func() { serveDone <- server.Serve(listener) }()
	select {
	case err = <-serveDone:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdown, c := context.WithTimeout(context.Background(), 5*time.Second)
		defer c()
		err = server.Shutdown(shutdown)
		if err != nil {
			_ = server.Close()
		}
		<-serveDone
		return err
	}
}
