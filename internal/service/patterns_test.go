package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"trimlights/internal/pattern"
	"trimlights/internal/storage"
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
