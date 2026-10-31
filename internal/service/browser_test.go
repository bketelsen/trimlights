package service

import (
	"context"
	"crypto/rand"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"testing"
	"time"

	"trimlights/internal/storage"
	"trimlights/internal/trimlight"
)

// TestBrowserPreview is an explicit, isolated manual UI fixture. It never ships
// in the application binary, uses a temporary database and has no controller.
// TRIMLIGHT_BROWSER_PREVIEW=1 go test ./internal/service -run '^TestBrowserPreview$' -v -count=1 -timeout=12m
func TestBrowserPreview(t *testing.T) {
	if os.Getenv("TRIMLIGHT_BROWSER_PREVIEW") != "1" {
		t.Skip("manual browser fixture")
	}
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	raw, err := os.ReadFile("../trimlight/testdata/snapshot.bin")
	if err != nil {
		t.Fatal(err)
	}
	snap, err := trimlight.ParseSnapshot(raw, 3306)
	if err != nil {
		t.Fatal(err)
	}
	snap.QueriedAt = time.Now().UTC()
	if err = store.Record(context.Background(), storage.Observation{Snapshot: snap, Source: "browser-test-fixture"}, storage.Operation{ID: "fixture", Started: snap.QueriedAt, Finished: snap.QueriedAt, Actor: "fixture"}); err != nil {
		t.Fatal(err)
	}
	token := rand.Text() + rand.Text()
	csrf := rand.Text()
	worker := NewWorker(store, nil, -1, nil)
	if os.Getenv("TRIMLIGHT_BROWSER_NATIVE") == "1" {
		worker = NewWorker(store, newNativeStub(t), 3306, nil)
	}
	workerCtx, workerCancel := context.WithCancel(context.Background())
	workerDone := make(chan struct{})
	go func() { defer close(workerDone); worker.Run(workerCtx) }()
	defer func() { workerCancel(); <-workerDone }()
	base := Handler(store, worker, token, nil)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != "127.0.0.1:8789" {
			http.Error(w, "Unexpected host", 421)
			return
		}
		if r.Method != "GET" && r.Method != "HEAD" && (r.Header.Get("Origin") != "http://127.0.0.1:8789" || r.Header.Get("X-CSRF-Token") != csrf) {
			http.Error(w, "Invalid fixture mutation", 403)
			return
		}
		if r.URL.Path == "/api/v1/me" {
			jsonResponse(w, 200, storage.Session{Subject: "browser-test", Name: "Browser test fixture", CSRF: csrf, Expires: time.Now().Add(time.Hour)})
			return
		}
		// Opt-in latency for checking edits made while a pattern save is pending.
		// This handler exists only in the isolated manual test, never production.
		if os.Getenv("TRIMLIGHT_BROWSER_SLOW_SAVE") == "1" &&
			(r.Method == "POST" || r.Method == "PUT") && strings.HasPrefix(r.URL.Path, "/api/v1/patterns") {
			select {
			case <-time.After(2 * time.Second):
			case <-r.Context().Done():
				return
			}
		}
		r.Header.Set("Authorization", "Bearer "+token)
		base.ServeHTTP(w, r)
	})
	listener, err := net.Listen("tcp", "127.0.0.1:8789")
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second}
	defer server.Close()
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	t.Log("Isolated fixture UI: http://127.0.0.1:8789 — no physical controller; optional native controls use in-memory synthetic state")
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	select {
	case <-ctx.Done():
	case <-time.After(10 * time.Minute):
	case err := <-done:
		t.Fatal(err)
	}
}
