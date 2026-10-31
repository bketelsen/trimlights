package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"trimlights/internal/pattern"
	"trimlights/internal/storage"
	"trimlights/internal/trimlight"
)

func TestPatternAPIValidationAndConflicts(t *testing.T) {
	store, w := setup(t, nil)
	token := strings.Repeat("x", 32)
	h := Handler(store, w, token, nil)
	call := func(method, path string, body any, revision string) *httptest.ResponseRecorder {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(method, path, bytes.NewReader(raw))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("If-Match", revision)
		response := httptest.NewRecorder()
		h.ServeHTTP(response, req)
		return response
	}
	d := pattern.Design{Schema: 1, Name: "Night", Effect: "static", Brightness: 100, Palette: []pattern.Run{{Color: "#123456", Count: 2}}}
	response := call("POST", "/api/v1/patterns", d, "")
	if response.Code != 201 {
		t.Fatal(response.Code, response.Body.String())
	}
	var saved storage.SavedPattern
	if err := json.Unmarshal(response.Body.Bytes(), &saved); err != nil {
		t.Fatal(err)
	}
	if saved.Revision != 1 || response.Header().Get("ETag") != `"1"` {
		t.Fatal(saved, response.Header())
	}
	path := "/api/v1/patterns/" + saved.ID
	for _, v := range []struct {
		revision string
		code     int
	}{{"", 428}, {"1", 400}, {`"0"`, 400}, {`"999"`, 409}} {
		if r := call("PUT", path, d, v.revision); r.Code != v.code {
			t.Fatal(v, r.Code, r.Body.String())
		}
	}
	d.Name = "Saved change"
	if r := call("PUT", path, d, `"1"`); r.Code != 200 {
		t.Fatal(r.Code, r.Body.String())
	}
	if r := call("PUT", path, d, `"1"`); r.Code != 409 {
		t.Fatal(r.Code)
	}
	if r := call("POST", path+"/archive", map[string]any{}, `"2"`); r.Code != 200 {
		t.Fatal(r.Code, r.Body.String())
	}
	if r := call("POST", path+"/restore", map[string]any{}, `"3"`); r.Code != 200 {
		t.Fatal(r.Code, r.Body.String())
	}
	if r := call("GET", path+"/revisions", nil, ""); r.Code != 200 || !strings.Contains(r.Body.String(), "Saved change") || !strings.Contains(r.Body.String(), "Night") {
		t.Fatal(r.Code, r.Body.String())
	}
	for _, body := range []any{nil, map[string]any{"schema": 1, "name": "bad", "unknown": true}, map[string]any{"schema": 999}, map[string]any{"name": strings.Repeat("x", 40000)}} {
		if r := call("POST", "/api/v1/patterns", body, ""); r.Code != 400 {
			t.Fatal(r.Code, fmt.Sprint(body))
		}
	}
	r := httptest.NewRequest(http.MethodPost, "/api/v1/patterns", strings.NewReader(`{}`))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, r)
	if rr.Code != 401 {
		t.Fatal(rr.Code)
	}
	if w.Status().Enabled {
		t.Fatal("library unexpectedly enabled controller")
	}
}

func TestControllerImportErrorsAreClientErrors(t *testing.T) {
	store, w := setup(t, nil)
	token := strings.Repeat("x", 32)
	h := Handler(store, w, token)
	stamp := time.Now().UTC()
	snap := &trimlight.Snapshot{QueriedAt: stamp, Patterns: []trimlight.Pattern{{ID: 1, Name: "", ParametersHex: strings.Repeat("00", 156)}, {ID: 2, Name: "Broken", ParametersHex: "bad"}}}
	if err := store.Record(context.Background(), storage.Observation{Snapshot: snap}, storage.Operation{ID: "fixture", Started: stamp, Finished: stamp}); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/patterns/import-controller", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	h.ServeHTTP(response, req)
	if response.Code != 400 || !strings.Contains(response.Body.String(), "invalid captured native pattern") {
		t.Fatal(response.Code, response.Body.String())
	}
}

func TestCreateRejectsForgedNativeDesign(t *testing.T) {
	store, w := setup(t, nil)
	token := strings.Repeat("x", 32)
	h := Handler(store, w, token)
	body := `{"schema":1,"name":"Forged","favorite":false,"effect":"native","brightness":100,"speed":0,"reverse":false,"palette":[],"native":{"id":1,"name":"Forged","parameters_hex":"` + strings.Repeat("00", 156) + `","snapshot_at":"2026-10-30T12:00:00Z"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/patterns", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	h.ServeHTTP(response, req)
	if response.Code != 400 || !strings.Contains(response.Body.String(), "latest controller snapshot") {
		t.Fatal(response.Code, response.Body.String())
	}
	if values, err := store.Patterns(context.Background(), true); err != nil || len(values) != 0 {
		t.Fatal(values, err)
	}
}
