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
