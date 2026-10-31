// Package webui embeds the frontend build produced by `make web-build`.
package webui

import (
	"embed"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
)

//go:embed all:dist
var assets embed.FS

// Handler serves the embedded frontend. A nil logger uses slog.Default.
func Handler(logger *slog.Logger) http.Handler {
	root, _ := fs.Sub(assets, "dist")
	return handler(root, logger)
}

func handler(root fs.FS, logger *slog.Logger) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}
	files := http.FileServer(http.FS(root))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' blob: data:; connect-src 'self'; object-src 'none'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("Cache-Control", "no-store")
		if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/auth/") {
			http.NotFound(w, r)
			return
		}
		if r.URL.Path != "/" && !strings.HasPrefix(r.URL.Path, "/assets/") {
			http.NotFound(w, r)
			return
		}
		if _, err := fs.Stat(root, "index.html"); err != nil {
			logger.ErrorContext(r.Context(), "request failed", "route", "GET /", "status", 503, "error", "frontend not built")
			http.Error(w, "Frontend not built; run make web-build", 503)
			return
		}
		recorder := &statusWriter{ResponseWriter: w}
		files.ServeHTTP(recorder, r)
		// The file server answers 5xx only for unexpected file system errors, and
		// it does not expose the error. The status and path are enough to look.
		if recorder.status >= 500 {
			logger.ErrorContext(r.Context(), "request failed", "route", "GET /", "path", r.URL.Path, "status", recorder.status, "error", "frontend file error")
		}
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (s *statusWriter) WriteHeader(code int) {
	if s.status == 0 {
		s.status = code
	}
	s.ResponseWriter.WriteHeader(code)
}
func (s *statusWriter) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	return s.ResponseWriter.Write(b)
}
func (s *statusWriter) Unwrap() http.ResponseWriter { return s.ResponseWriter }
