package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"trimlights/internal/storage"
	"trimlights/internal/trimlight"
)

// logBuffer is a test slog sink that is safe for the worker goroutine.
type logBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *logBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}
func (b *logBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
func (b *logBuffer) entries(t *testing.T) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(b.String()), "\n") {
		if line == "" {
			continue
		}
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatal("log line is not JSON", line)
		}
		out = append(out, entry)
	}
	return out
}
func testLogger(level slog.Level) (*slog.Logger, *logBuffer) {
	b := &logBuffer{}
	return slog.New(slog.NewJSONHandler(b, &slog.HandlerOptions{Level: level})), b
}

func TestServerErrorsLogCauseAndKeepResponse(t *testing.T) {
	s, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	logger, logs := testLogger(slog.LevelInfo)
	token := strings.Repeat("a", 32)
	h := Handler(s, NewWorker(s, nil, -1, logger), token, logger)
	// A closed database makes every storage read fail behind a generic message.
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ path, route, body string }{
		{"/api/v1/status", "GET /api/v1/status", `{"error":"cannot read state"}`},
		{"/api/v1/patterns?include_archived=1", "GET /api/v1/patterns", `{"error":"could not save or read the pattern library"}`},
		{"/api/v1/house", "GET /api/v1/house", `{"error":"could not read or save house photos and mapping"}`},
		{"/api/v1/schedule", "GET /api/v1/schedule", `{"error":"cannot read calendar"}`},
		{"/readyz", "GET /readyz", `{"error":"storage unavailable"}`},
	} {
		before := len(logs.entries(t))
		r := httptest.NewRequest("GET", tc.path, nil)
		r.Header.Set("Authorization", "Bearer "+token)
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, r)
		if rr.Code < 500 || strings.TrimSpace(rr.Body.String()) != tc.body {
			t.Fatal(tc.path, rr.Code, rr.Body.String())
		}
		entries := logs.entries(t)
		if len(entries) != before+1 {
			t.Fatal(tc.path, "want exactly one log line", logs.String())
		}
		got := entries[before]
		if got["level"] != "ERROR" || got["msg"] != "request failed" || got["route"] != tc.route || got["status"] != float64(rr.Code) || !strings.Contains(got["error"].(string), "closed") {
			t.Fatal(tc.path, got)
		}
	}
}

func TestControllerFailureLogsWithoutAddress(t *testing.T) {
	const address = "192.0.2.44:8586"
	remote, err := net.ResolveTCPAddr("tcp", address)
	if err != nil {
		t.Fatal(err)
	}
	transport := fmt.Errorf("connect: %w", &net.OpError{Op: "dial", Net: "tcp", Addr: remote, Err: fmt.Errorf("refused")})
	s, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	logger, logs := testLogger(slog.LevelDebug)
	w := NewWorker(s, controllerFunc(func(context.Context, int) (*trimlight.Snapshot, error) { return nil, transport }), -1, logger)
	w.pace = 0
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); w.Run(ctx) }()
	t.Cleanup(func() { cancel(); <-done })
	token := strings.Repeat("a", 32)
	h := requestLog(logger, Handler(s, w, token, logger))
	r := httptest.NewRequest("POST", "/api/v1/controller/refresh?debug=1", strings.NewReader(`{"acknowledge_clock_sync":true}`))
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, r)
	if rr.Code != 502 {
		t.Fatal(rr.Code, rr.Body.String())
	}
	if text := logs.String(); strings.Contains(text, "192.0.2.44") || strings.Contains(text, "debug=1") {
		t.Fatal("log contains the controller address or the query", text)
	}
	var operation, failure, request map[string]any
	for _, entry := range logs.entries(t) {
		switch entry["msg"] {
		case "controller operation finished":
			operation = entry
		case "request failed":
			failure = entry
		case "request":
			request = entry
		}
	}
	if operation == nil || operation["kind"] != "snapshot" || operation["outcome"] != "failed" || operation["level"] != "WARN" || operation["error"] != "controller network dial failed" {
		t.Fatal(operation, logs.String())
	}
	if _, ok := operation["duration_ms"]; !ok {
		t.Fatal("operation lacks duration", operation)
	}
	if failure == nil || failure["route"] != "POST /api/v1/controller/refresh" || failure["status"] != float64(502) || failure["operation_id"] != operation["operation_id"] {
		t.Fatal(failure, logs.String())
	}
	if request == nil || request["path"] != "/api/v1/controller/refresh" || request["status"] != float64(502) || request["method"] != "POST" {
		t.Fatal(request, logs.String())
	}
}

func TestSafeErrorAndRequestLogLevel(t *testing.T) {
	if got := safeError(fmt.Errorf("connect: %w", &net.DNSError{Name: "controller.home.invalid", Err: "no such host"})); got != "controller name lookup failed" {
		t.Fatal(got)
	}
	if got := safeError(fmt.Errorf("plain")); got != "plain" {
		t.Fatal(got)
	}
	logger, logs := testLogger(slog.LevelInfo)
	s, w := setup(t, nil)
	h := requestLog(logger, Handler(s, w, strings.Repeat("a", 32), logger))
	for _, path := range []string{"/healthz", "/readyz"} {
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, httptest.NewRequest("GET", path, nil))
		if rr.Code != 200 {
			t.Fatal(path, rr.Code)
		}
	}
	if logs.String() != "" {
		t.Fatal("info level logged probe requests", logs.String())
	}
}
