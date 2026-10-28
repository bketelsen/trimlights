package service

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"trimlights/internal/storage"
	"trimlights/internal/trimlight"
)

type controllerFunc func(context.Context, int) (*trimlight.Snapshot, error)

func (f controllerFunc) Snapshot(ctx context.Context, offset int) (*trimlight.Snapshot, error) {
	return f(ctx, offset)
}
func setup(t *testing.T, c Controller) (*storage.Store, *Worker) {
	t.Helper()
	s, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	w := NewWorker(s, c, -1)
	w.pace = 0
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); w.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		<-done
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	})
	return s, w
}
func waitResult(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(3 * time.Second):
		t.Fatal("worker stuck")
		return nil
	}
}

func TestWorkerPreservesSnapshotOnFailure(t *testing.T) {
	var calls atomic.Int32
	stamp := time.Now().UTC()
	s, w := setup(t, controllerFunc(func(context.Context, int) (*trimlight.Snapshot, error) {
		if calls.Add(1) > 1 {
			return nil, errors.New("offline")
		}
		return &trimlight.Snapshot{QueriedAt: stamp}, nil
	}))
	for i := 0; i < 2; i++ {
		_, done, err := w.Enqueue(context.Background(), "owner")
		if err != nil {
			t.Fatal(err)
		}
		got := waitResult(t, done)
		if (got != nil) != (i == 1) {
			t.Fatal(got)
		}
	}
	state, err := s.Observation(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if state.Snapshot == nil || !state.Snapshot.QueriedAt.Equal(stamp) || state.LastError != "offline" {
		t.Fatal(state)
	}
	ops, err := s.Operations(context.Background())
	if err != nil || len(ops) != 2 || ops[0].Error != "offline" {
		t.Fatal(ops, err)
	}
}
func TestQueueBackpressureAndCancellation(t *testing.T) {
	started := make(chan struct{})
	var calls atomic.Int32
	_, w := setup(t, controllerFunc(func(ctx context.Context, _ int) (*trimlight.Snapshot, error) {
		calls.Add(1)
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	}))
	active, cancelActive := context.WithCancel(context.Background())
	defer cancelActive()
	_, done, err := w.Enqueue(active, "owner")
	if err != nil {
		t.Fatal(err)
	}
	<-started
	queued, cancelQueued := context.WithCancel(context.Background())
	results := []<-chan error{}
	for i := 0; i < 8; i++ {
		_, done, err := w.Enqueue(queued, "owner")
		if err != nil {
			t.Fatal(err)
		}
		results = append(results, done)
	}
	if _, _, err = w.Enqueue(queued, "owner"); !errors.Is(err, ErrQueueFull) {
		t.Fatal(err)
	}
	cancelQueued()
	cancelActive()
	if err = waitResult(t, done); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	for _, done := range results {
		if err = waitResult(t, done); !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	}
	if calls.Load() != 1 {
		t.Fatal("canceled queued work reached controller")
	}
}
func TestHTTPAuthAcknowledgmentAndCachedStatus(t *testing.T) {
	var calls atomic.Int32
	s, w := setup(t, controllerFunc(func(context.Context, int) (*trimlight.Snapshot, error) {
		calls.Add(1)
		return &trimlight.Snapshot{QueriedAt: time.Now().UTC()}, nil
	}))
	token := strings.Repeat("a", 32)
	h := Handler(s, w, token)
	request := func(method, path, body, credential, ctype string) int {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Authorization", credential)
		r.Header.Set("Content-Type", ctype)
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, r)
		if rr.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("missing no-store")
		}
		return rr.Code
	}
	if got := request("GET", "/api/v1/status", "", "", " "); got != 401 {
		t.Fatal(got)
	}
	if got := request("GET", "/readyz", "", "", ""); got != 200 {
		t.Fatal(got)
	}
	if got := request("GET", "/api/v1/status", "", "Bearer "+token, ""); got != 200 {
		t.Fatal(got)
	}
	if calls.Load() != 0 {
		t.Fatal("cached status queried hardware")
	}
	for _, body := range []string{`{}`, `{"acknowledge_clock_sync":false}`, `{"acknowledge_clock_sync":true,"extra":1}`, `{"acknowledge_clock_sync":true}{}`} {
		if got := request("POST", "/api/v1/controller/refresh", body, "Bearer "+token, "application/json"); got != 400 {
			t.Fatal(body, got)
		}
	}
	if calls.Load() != 0 {
		t.Fatal("invalid request reached hardware")
	}
	if got := request("POST", "/api/v1/controller/refresh", `{"acknowledge_clock_sync":true}`, "Bearer "+token, "application/json"); got != 200 {
		t.Fatal(got)
	}
	if calls.Load() != 1 {
		t.Fatal(calls.Load())
	}
}
func TestDisabledAndPublicBindRejected(t *testing.T) {
	s, w := setup(t, nil)
	_ = s
	if _, _, err := w.Enqueue(context.Background(), "test"); !errors.Is(err, ErrDisabled) {
		t.Fatal(err)
	}
	for _, listen := range []string{"0.0.0.0:8787", ":8787", "localhost:8787", "192.0.2.10:8787"} {
		if err := Validate(Config{Listen: listen, DataDir: "test", APIToken: strings.Repeat("x", 32)}); err == nil {
			t.Fatal(listen)
		}
	}
	if err := Validate(Config{Listen: "127.0.0.1:8787", DataDir: "test", APIToken: strings.Repeat("x", 32)}); err != nil {
		t.Fatal(err)
	}
}

var _ http.Handler = Handler(nil, nil, "")
