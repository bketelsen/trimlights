package service

import (
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
)

// Every JSON endpoint shares one media type check: parameters such as charset
// are accepted, and any other media type is refused with 415.
func TestJSONEndpointsShareMediaTypeCheck(t *testing.T) {
	s, w, c, snap := nativeService(t)
	token := strings.Repeat("x", 32)
	h := Handler(s, w, token, nil)
	call := func(path, ctype, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", path, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+token)
		if ctype != "" {
			r.Header.Set("Content-Type", ctype)
		}
		out := httptest.NewRecorder()
		h.ServeHTTP(out, r)
		return out
	}
	control := fmt.Sprintf(`{"action":"off","baseline_sha256":%q,"acknowledge_clock_sync":true}`, snap.StableSHA256)
	endpoints := []struct{ path, body string }{
		{"/api/v1/controller/refresh", `{"acknowledge_clock_sync":true}`},
		{"/api/v1/controller/control", control},
		{"/api/v1/patterns", `{}`},
	}
	for _, e := range endpoints {
		for _, ctype := range []string{"", "text/plain", "application/jsonx", "application/json; charset", "text/json; charset=utf-8"} {
			if out := call(e.path, ctype, e.body); out.Code != 415 || !strings.Contains(out.Body.String(), "Content-Type must be application/json") {
				t.Fatalf("%s %q: %d %s", e.path, ctype, out.Code, out.Body.String())
			}
		}
	}
	c.mu.Lock()
	writes := c.writes
	c.mu.Unlock()
	if writes != 0 {
		t.Fatal("refused media type reached the controller")
	}
	for _, ctype := range []string{"application/json; charset=utf-8", "Application/JSON;charset=UTF-8"} {
		if out := call("/api/v1/controller/refresh", ctype, `{"acknowledge_clock_sync":true}`); out.Code != 200 {
			t.Fatalf("refresh %q: %d %s", ctype, out.Code, out.Body.String())
		}
		if out := call("/api/v1/patterns", ctype, `{}`); out.Code == 415 {
			t.Fatalf("validate %q: %d %s", ctype, out.Code, out.Body.String())
		}
	}
	if out := call("/api/v1/controller/control", "application/json; charset=utf-8", control); out.Code != 200 {
		t.Fatal(out.Code, out.Body.String())
	}
}
