package service

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"trimlights/internal/auth"
	"trimlights/internal/storage"
	"trimlights/internal/trimlight"
)

// Native UI/API fixture; its only state is a wholly synthetic in-memory snapshot.
type nativeStub struct {
	mu     sync.Mutex
	raw    []byte
	writes int
}

func newNativeStub(t *testing.T) *nativeStub {
	t.Helper()
	raw, err := os.ReadFile("../trimlight/testdata/snapshot.bin")
	if err != nil {
		t.Fatal(err)
	}
	return &nativeStub{raw: raw}
}
func (c *nativeStub) Snapshot(context.Context, int) (*trimlight.Snapshot, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	s, err := trimlight.ParseSnapshot(c.raw, 3306)
	if err == nil {
		s.QueriedAt = time.Now().UTC()
	}
	return s, err
}
func (c *nativeStub) Control(ctx context.Context, input trimlight.ControlRequest, record func(*trimlight.Snapshot) error) (*trimlight.ControlResult, error) {
	s, err := c.Snapshot(ctx, 3306)
	if err != nil {
		return nil, err
	}
	if s.StableSHA256 != input.BaselineSHA256 {
		return &trimlight.ControlResult{Snapshot: s, Outcome: "rejected"}, trimlight.ErrStaleBaseline
	}
	if err = record(s); err != nil {
		return &trimlight.ControlResult{Snapshot: s, Outcome: "rejected"}, err
	}
	c.mu.Lock()
	c.writes++
	switch input.Action {
	case "off":
		c.raw[3] = 0
	case "timer":
		c.raw[3] = 2
	case "play":
		c.raw[3] = 1
		start := 52 + (int(input.PatternID)-1)*182
		copy(c.raw[3320:3476], c.raw[start:start+156])
	}
	c.mu.Unlock()
	s, err = c.Snapshot(ctx, 3306)
	return &trimlight.ControlResult{Snapshot: s, Outcome: "verified", WriteAttempted: true}, err
}
func nativeService(t *testing.T) (*storage.Store, *Worker, *nativeStub, *trimlight.Snapshot) {
	t.Helper()
	c := newNativeStub(t)
	s, w := setup(t, c)
	w.offset = 3306
	snap, err := c.Snapshot(context.Background(), 3306)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Record(context.Background(), storage.Observation{Snapshot: snap, Source: "fixture"}, storage.Operation{ID: "initial", Started: snap.QueriedAt, Finished: snap.QueriedAt}); err != nil {
		t.Fatal(err)
	}
	return s, w, c, snap
}

func TestControlAPIValidationAuditAndStaleSecondUser(t *testing.T) {
	s, w, c, snap := nativeService(t)
	token := strings.Repeat("x", 32)
	h := Handler(s, w, token)
	call := func(body, authz string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/api/v1/controller/control", strings.NewReader(body))
		r.Header.Set("Authorization", authz)
		r.Header.Set("Content-Type", "application/json")
		out := httptest.NewRecorder()
		h.ServeHTTP(out, r)
		return out
	}
	body := fmt.Sprintf(`{"action":"play","pattern_id":9,"baseline_sha256":%q,"acknowledge_clock_sync":true}`, snap.StableSHA256)
	if out := call(body, ""); out.Code != 401 {
		t.Fatal(out.Code)
	}
	for _, invalid := range []string{`null`, `{}`, `{"action":"brightness","acknowledge_clock_sync":true}`, strings.Replace(body, `"pattern_id":9`, `"pattern_id":256`, 1), strings.Replace(body, `"acknowledge_clock_sync":true`, `"acknowledge_clock_sync":false`, 1), body + `{}`, strings.Replace(body, `"action":"play"`, `"action":"play","actor":"fake"`, 1)} {
		if out := call(invalid, "Bearer "+token); out.Code != 400 {
			t.Fatal(invalid, out.Code, out.Body.String())
		}
	}
	if out := call(body, "Bearer "+token); out.Code != 200 {
		t.Fatal(out.Code, out.Body.String())
	}
	if out := call(body, "Bearer "+token); out.Code != 409 {
		t.Fatal("stale second user", out.Code, out.Body.String())
	}
	c.mu.Lock()
	writes := c.writes
	c.mu.Unlock()
	if writes != 1 {
		t.Fatal(writes)
	}
	ops, err := s.Operations(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(ops) != 2 || ops[0].Control == nil || ops[0].Control.Outcome != "verified" || ops[0].Actor != "local-api" {
		t.Fatal(ops)
	}
	state, _ := s.Observation(context.Background())
	if state.Snapshot.Playback.Mode != "manual" || state.Snapshot.Playback.MatchingPatternIDs[0] != 9 {
		t.Fatal(state)
	}
}

func TestHouseholdControlCSRFAndActor(t *testing.T) {
	s, w, _, snap := nativeService(t)
	var provider *httptest.Server
	provider = httptest.NewTLSServer(http.HandlerFunc(func(out http.ResponseWriter, r *http.Request) {
		jsonResponse(out, 200, map[string]any{"issuer": provider.URL, "authorization_endpoint": provider.URL + "/authorize", "token_endpoint": provider.URL + "/token", "jwks_uri": provider.URL + "/keys"})
	}))
	defer provider.Close()
	old := http.DefaultTransport
	http.DefaultTransport = provider.Client().Transport
	t.Cleanup(func() { http.DefaultTransport = old })
	cfg := auth.Config{Origin: "https://lights.example.test", Issuer: provider.URL, ClientID: "fixture", ClientSecret: "fixture-secret", AllowedSubjects: []string{"owner"}}
	a, err := auth.New(context.Background(), cfg, s)
	if err != nil {
		t.Fatal(err)
	}
	cookie := "fixture-cookie"
	sum := sha256.Sum256([]byte(cfg.Issuer + "\x00" + cfg.ClientID + "\x00" + cfg.Origin + "\x00" + cookie))
	if err = s.SaveSession(context.Background(), storage.Session{Hash: hex.EncodeToString(sum[:]), Subject: "owner", CSRF: "fixture-csrf", Expires: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{"action": "play", "pattern_id": 9, "baseline_sha256": snap.StableSHA256, "acknowledge_clock_sync": true})
	for _, tc := range []struct {
		origin, csrf string
		want         int
	}{{cfg.Origin, "", 403}, {"https://evil.example.test", "fixture-csrf", 403}, {cfg.Origin, "fixture-csrf", 200}} {
		r := httptest.NewRequest("POST", cfg.Origin+"/api/v1/controller/control", strings.NewReader(string(body)))
		r.AddCookie(&http.Cookie{Name: "__Host-trimlight-session", Value: cookie})
		r.Header.Set("Origin", tc.origin)
		r.Header.Set("X-CSRF-Token", tc.csrf)
		r.Header.Set("Content-Type", "application/json")
		out := httptest.NewRecorder()
		a.Handler(apiHandler(s, w)).ServeHTTP(out, r)
		if out.Code != tc.want {
			t.Fatal(out.Code, out.Body.String())
		}
	}
	ops, err := s.Operations(context.Background())
	if err != nil || ops[0].Actor != "owner" {
		t.Fatal(ops, err)
	}
}

func TestControlsDisabledAfterPersistenceFailure(t *testing.T) {
	dir := t.TempDir()
	s, err := storage.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	c := newNativeStub(t)
	w := NewWorker(s, c, 3306)
	w.pace = 0
	ctx, cancel := context.WithCancel(context.Background())
	doneWorker := make(chan struct{})
	go func() { defer close(doneWorker); w.Run(ctx) }()
	defer func() { cancel(); <-doneWorker }()
	snap, _ := c.Snapshot(ctx, 3306)
	if err = s.Record(ctx, storage.Observation{Snapshot: snap, Source: "fixture"}, storage.Operation{ID: "initial", Started: snap.QueriedAt, Finished: snap.QueriedAt}); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", filepath.Join(dir, "trimlights.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.Exec("CREATE TRIGGER fail_audit BEFORE INSERT ON operations BEGIN SELECT RAISE(ABORT,'fixture audit failure'); END"); err != nil {
		t.Fatal(err)
	}
	_, done, err := w.EnqueueControl(ctx, "owner", trimlight.ControlRequest{Action: "play", PatternID: 9, BaselineSHA256: snap.StableSHA256})
	if err != nil {
		t.Fatal(err)
	}
	if err = waitResult(t, done); err == nil {
		t.Fatal("audit failure hidden")
	}
	c.mu.Lock()
	writes := c.writes
	c.mu.Unlock()
	if writes != 0 {
		t.Fatal("wrote without audit")
	}
	if _, err = db.Exec("DROP TRIGGER fail_audit"); err != nil {
		t.Fatal(err)
	}
	_, done, err = w.Enqueue(ctx, "owner")
	if err != nil {
		t.Fatal(err)
	}
	if err = waitResult(t, done); err != nil {
		t.Fatal(err)
	}
	state, _ := s.Observation(ctx)
	if allowed, _ := w.ControlAvailability(state); allowed {
		t.Fatal("refresh cleared persistence block")
	}
}
