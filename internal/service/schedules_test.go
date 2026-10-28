package service

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"trimlights/internal/schedule"
)

func TestCalendarAPIValidationAndPreview(t *testing.T) {
	store, worker := setup(t, nil)
	token := strings.Repeat("x", 32)
	handler := Handler(store, worker, token)
	call := func(method, path string, body any, rev string) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(body)
		r := httptest.NewRequest(method, path, bytes.NewReader(raw))
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("If-Match", rev)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	if r := call("GET", "/api/v1/schedule", nil, ""); r.Code != 200 || r.Header().Get("ETag") != `"1"` {
		t.Fatal(r.Code, r.Body.String())
	}
	plan := schedule.Default()
	if r := call("PUT", "/api/v1/schedule", plan, ""); r.Code != 428 {
		t.Fatal(r.Code)
	}
	if r := call("PUT", "/api/v1/schedule", plan, `"1"`); r.Code != 200 || r.Header().Get("ETag") != `"2"` {
		t.Fatal(r.Code, r.Body.String())
	}
	if r := call("PUT", "/api/v1/schedule", plan, `"1"`); r.Code != 409 {
		t.Fatal(r.Code)
	}
	plan.Rules = []schedule.Rule{{ID: "test", Name: "Test", Enabled: true, Kind: "weekly", Weekdays: []int{3}, Start: schedule.Boundary{Kind: "clock", Time: "19:30"}, End: schedule.Boundary{Kind: "clock", Time: "22:59"}, Action: schedule.Action{Kind: "pattern", PatternID: "missing", Revision: 1}}}
	if r := call("PUT", "/api/v1/schedule", plan, `"2"`); r.Code != 400 {
		t.Fatal(r.Code, r.Body.String())
	}
	// A draft preview neither persists nor opens the worker/controller.
	preview := map[string]any{"plan": plan, "from": "2026-10-28T23:00:00Z", "until": "2026-10-29T04:00:00Z"}
	r := call("POST", "/api/v1/schedule/preview", preview, "")
	if r.Code != 200 || !strings.Contains(r.Body.String(), `"execution_enabled":false`) {
		t.Fatal(r.Code, r.Body.String())
	}
	saved, err := store.Schedule(context.Background())
	if err != nil || saved.Revision != 2 || len(saved.Plan.Rules) != 0 {
		t.Fatal(saved, err)
	}
	if worker.Status().Queued != 0 || worker.Status().Enabled {
		t.Fatal(worker.Status())
	}
	preview["until"] = "2027-01-01T00:00:00Z"
	if r := call("POST", "/api/v1/schedule/preview", preview, ""); r.Code != 400 {
		t.Fatal(r.Code)
	}
	if r := call("GET", "/api/v1/schedule/revisions", nil, ""); r.Code != 200 || !strings.Contains(r.Body.String(), `"revision":2`) {
		t.Fatal(r.Code, r.Body.String())
	}
	for _, path := range []string{"/api/v1/schedule", "/api/v1/schedule/revisions", "/api/v1/schedule/preview"} {
		r := httptest.NewRequest("GET", path, nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != 401 {
			t.Fatal(path, w.Code)
		}
	}
}

func TestCalendarImportAPI(t *testing.T) {
	store, worker := setup(t, nil)
	handler := Handler(store, worker, strings.Repeat("x", 32))
	call := func(raw, rev string, authorized bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/api/v1/schedule/validate", strings.NewReader(raw))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("If-Match", rev)
		if authorized {
			r.Header.Set("Authorization", "Bearer "+strings.Repeat("x", 32))
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	raw := `{"schema":1,"timezone":"America/New_York","rules":null}`
	for _, tc := range []struct {
		body, rev string
		auth      bool
		status    int
	}{
		{raw, `"1"`, false, 401}, {raw, "", true, 428}, {raw, `"2"`, true, 409},
		{"null", `"1"`, true, 400}, {"{", `"1"`, true, 400},
		{`{"schema":1,"timezone":"Invalid/Zone","rules":[]}`, `"1"`, true, 400},
		{`{"schema":1,"timezone":"UTC","rules":[],"execution_enabled":true}`, `"1"`, true, 400},
		{raw + " {}", `"1"`, true, 400}, {strings.Repeat(" ", 128<<10) + raw, `"1"`, true, 400},
	} {
		if r := call(tc.body, tc.rev, tc.auth); r.Code != tc.status {
			t.Fatalf("got %d want %d: %s", r.Code, tc.status, r.Body.String())
		}
	}
	r := call(raw, `"1"`, true)
	if r.Code != 200 || !strings.Contains(r.Body.String(), `"rules":[]`) || !strings.Contains(r.Body.String(), `"execution_enabled":false`) {
		t.Fatal(r.Code, r.Body.String())
	}
	plan := schedule.Default()
	plan.Rules = []schedule.Rule{{ID: "exception", Name: "One evening", Enabled: true, Kind: "exception", From: "2026-12-31", Through: "2026-12-31", Start: schedule.Boundary{Kind: "clock", Time: "19:00"}, End: schedule.Boundary{Kind: "clock", Time: "22:00"}, Action: schedule.Action{Kind: "off"}}}
	encoded, _ := json.Marshal(plan)
	r = call(string(encoded), `"1"`, true)
	if r.Code != 200 || !strings.Contains(r.Body.String(), `"weekdays":[]`) {
		t.Fatal(r.Code, r.Body.String())
	}
	plan.Rules[0].Action = schedule.Action{Kind: "pattern", PatternID: "missing", Revision: 1}
	encoded, _ = json.Marshal(plan)
	r = call(string(encoded), `"1"`, true)
	if r.Code != 400 || !strings.Contains(r.Body.String(), "matching household backup") {
		t.Fatal(r.Code, r.Body.String())
	}
	history, err := store.ScheduleHistory(context.Background())
	if err != nil || len(history) != 1 {
		t.Fatal("validation persisted", err)
	}
	ops, err := store.Operations(context.Background())
	if err != nil || len(ops) != 0 || worker.Status().Queued != 0 {
		t.Fatal("validation invoked worker", err)
	}
}
