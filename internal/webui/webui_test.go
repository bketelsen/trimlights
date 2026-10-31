package webui

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
)

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
	h := handler(root)
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
	handler(fstest.MapFS{}).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/manifest.webmanifest", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status %d, want 503", rec.Code)
	}
}
