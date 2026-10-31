package webui

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

// quiet discards the logs of tests that do not check them.
var quiet = slog.New(slog.DiscardHandler)

// brokenFS has an index page but fails every other open with a plain error,
// which the file server answers with 500.
type brokenFS struct{ fstest.MapFS }

func (b brokenFS) Open(name string) (fs.File, error) {
	if name == "index.html" || name == "." {
		return b.MapFS.Open(name)
	}
	return nil, errors.New("disk failure")
}

func TestFrontendServerErrorsAreLogged(t *testing.T) {
	for _, tc := range []struct {
		name, path string
		root       fs.FS
		status     int
		body       string
		cause      string
	}{
		{"not built", "/", fstest.MapFS{}, 503, "Frontend not built; run make web-build", "frontend not built"},
		{"file error", "/assets/app.js", brokenFS{fstest.MapFS{"index.html": {Data: []byte("<!doctype html>")}}}, 500, "", "frontend file error"},
		{"served", "/", fstest.MapFS{"index.html": {Data: []byte("<!doctype html>")}}, 200, "<!doctype html>", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var logs bytes.Buffer
			h := handler(tc.root, slog.New(slog.NewJSONHandler(&logs, nil)))
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, httptest.NewRequest("GET", tc.path, nil))
			if rr.Code != tc.status || (tc.body != "" && strings.TrimSpace(rr.Body.String()) != tc.body) {
				t.Fatal(rr.Code, rr.Body.String())
			}
			if rr.Code >= 400 && rr.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("error cached", rr.Header().Get("Cache-Control"))
			}
			if tc.cause == "" {
				if logs.Len() != 0 {
					t.Fatal("success logged", logs.String())
				}
				return
			}
			lines := strings.Split(strings.TrimSpace(logs.String()), "\n")
			var entry map[string]any
			if len(lines) != 1 || json.Unmarshal([]byte(lines[0]), &entry) != nil {
				t.Fatal("want exactly one JSON log line", logs.String())
			}
			if entry["level"] != "ERROR" || entry["error"] != tc.cause || entry["status"] != float64(tc.status) {
				t.Fatal(entry)
			}
		})
	}
}

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
	}, quiet)
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
	}, quiet)
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
	h := handler(fstest.MapFS{"assets/index-AbC123.js": {Data: []byte("x")}}, quiet)
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
	rr := serve(t, Handler(quiet), "GET", "/")
	if rr.Code != 200 && rr.Code != 503 {
		t.Fatal(rr.Code)
	}
	if rr := serve(t, Handler(quiet), "GET", "/assets/"); rr.Code != 404 {
		t.Fatal(rr.Code)
	}
}

func TestHandlerServesOnlyKnownPaths(t *testing.T) {
	root := fstest.MapFS{
		"index.html":           {Data: []byte("<!doctype html>")},
		"manifest.webmanifest": {Data: []byte(`{"name":"Trimlights"}`)},
		"apple-touch-icon.png": {Data: []byte("\x89PNG\r\n\x1a\n")},
		"icon-192.png":         {Data: []byte("\x89PNG\r\n\x1a\n")},
		"icon-512.png":         {Data: []byte("\x89PNG\r\n\x1a\n")},
		"assets/app.js":        {Data: []byte("export {}")},
		"private.txt":          {Data: []byte("not public")},
	}
	h := handler(root, slog.New(slog.DiscardHandler))
	for _, tc := range []struct {
		path, contentType string
		status            int
	}{
		{"/", "text/html; charset=utf-8", http.StatusOK},
		{"/assets/app.js", "", http.StatusOK},
		{"/manifest.webmanifest", "application/manifest+json", http.StatusOK},
		{"/apple-touch-icon.png", "image/png", http.StatusOK},
		{"/icon-192.png", "image/png", http.StatusOK},
		{"/icon-512.png", "image/png", http.StatusOK},
		{"/private.txt", "", http.StatusNotFound},
		{"/house", "", http.StatusNotFound},
		{"/api/v1/status", "", http.StatusNotFound},
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tc.path, nil))
		if rec.Code != tc.status {
			t.Errorf("%s: status %d, want %d", tc.path, rec.Code, tc.status)
		}
		if tc.contentType != "" && rec.Header().Get("Content-Type") != tc.contentType {
			t.Errorf("%s: Content-Type %q, want %q", tc.path, rec.Header().Get("Content-Type"), tc.contentType)
		}
		if rec.Header().Get("X-Content-Type-Options") != "nosniff" || rec.Header().Get("Content-Security-Policy") == "" {
			t.Errorf("%s: security headers missing", tc.path)
		}
	}
}

func TestHandlerReportsMissingBuild(t *testing.T) {
	rec := httptest.NewRecorder()
	handler(fstest.MapFS{}, slog.New(slog.DiscardHandler)).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/manifest.webmanifest", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status %d, want 503", rec.Code)
	}
}

