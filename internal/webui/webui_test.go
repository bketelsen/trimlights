package webui

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
)

func serve(t *testing.T, h http.Handler, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(method, path, nil))
	if rr.Header().Get("X-Content-Type-Options") != "nosniff" || rr.Header().Get("Content-Security-Policy") == "" {
		t.Fatalf("%s %s: missing security headers", method, path)
	}
	return rr
}

func TestCachePolicyAllowlistAndDirectories(t *testing.T) {
	h := handler(fstest.MapFS{
		"index.html":              {Data: []byte("<!doctype html>")},
		"assets/index-AbC123.js":  {Data: []byte("console.log(1)")},
		"assets/index-AbC123.css": {Data: []byte("body{}")},
		"assets/nested/chunk.js":  {Data: []byte("x")},
		"secret.txt":              {Data: []byte("not public")},
		"assets/dir.js/inner.js":  {Data: []byte("x")},
	})
	for _, c := range []struct {
		path, cache, kind string
	}{
		{"/", "no-cache", "text/html; charset=utf-8"},
		{"/assets/index-AbC123.js", "public, max-age=31536000, immutable", "text/javascript; charset=utf-8"},
		{"/assets/index-AbC123.css", "public, max-age=31536000, immutable", "text/css; charset=utf-8"},
		{"/assets/nested/chunk.js", "public, max-age=31536000, immutable", "text/javascript; charset=utf-8"},
	} {
		rr := serve(t, h, "GET", c.path)
		if rr.Code != 200 || rr.Header().Get("Cache-Control") != c.cache || rr.Header().Get("Content-Type") != c.kind {
			t.Fatalf("%s: %d %q %q", c.path, rr.Code, rr.Header().Get("Cache-Control"), rr.Header().Get("Content-Type"))
		}
	}
	if rr := serve(t, h, "HEAD", "/assets/index-AbC123.js"); rr.Code != 200 || rr.Body.Len() != 0 {
		t.Fatal("HEAD", rr.Code)
	}
	for _, path := range []string{
		"/assets/", "/assets", "/assets/nested", "/assets/nested/", "/assets/dir.js",
		"/assets/missing.js", "/index.html", "/secret.txt",
		"/api/v1/status", "/auth/login", "/schedule", "/assets/../secret.txt",
	} {
		rr := serve(t, h, "GET", path)
		if rr.Code != 404 {
			t.Fatalf("%s: %d", path, rr.Code)
		}
		if rr.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("%s: error cached: %q", path, rr.Header().Get("Cache-Control"))
		}
	}
}

func TestConditionalAndRangeStatuses(t *testing.T) {
	h := handler(fstest.MapFS{
		"index.html":             {Data: []byte("<!doctype html>")},
		"assets/index-AbC123.js": {Data: []byte("console.log(1)")},
	})
	for _, c := range []struct {
		header, value string
		status        int
		cache         string
	}{
		{"Range", "bytes=100-200", 416, "no-store"},
		{"If-Match", `"other"`, 412, "no-store"},
		{"Range", "bytes=0-3", 206, "public, max-age=31536000, immutable"},
	} {
		r := httptest.NewRequest("GET", "/assets/index-AbC123.js", nil)
		r.Header.Set(c.header, c.value)
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, r)
		if rr.Code != c.status || rr.Header().Get("Cache-Control") != c.cache {
			t.Fatalf("%s %s: %d %q", c.header, c.value, rr.Code, rr.Header().Get("Cache-Control"))
		}
	}
}

func TestMissingBuild(t *testing.T) {
	h := handler(fstest.MapFS{"assets/index-AbC123.js": {Data: []byte("x")}})
	for _, path := range []string{"/", "/assets/index-AbC123.js"} {
		rr := serve(t, h, "GET", path)
		if rr.Code != 503 || rr.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("%s: %d %q", path, rr.Code, rr.Header().Get("Cache-Control"))
		}
	}
	if rr := serve(t, h, "GET", "/api/v1/status"); rr.Code != 404 {
		t.Fatal(rr.Code)
	}
}

func TestEmbeddedHandlerServes(t *testing.T) {
	// The embedded build is empty unless make web-build ran first.
	rr := serve(t, Handler(), "GET", "/")
	if rr.Code != 200 && rr.Code != 503 {
		t.Fatal(rr.Code)
	}
	if rr := serve(t, Handler(), "GET", "/assets/"); rr.Code != 404 {
		t.Fatal(rr.Code)
	}
}
