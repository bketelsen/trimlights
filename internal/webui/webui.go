// Package webui embeds the frontend build produced by `make web-build`.
package webui

import (
	"embed"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
)

//go:embed all:dist
var assets embed.FS

// Cache policies. Vite names /assets/ files by content hash, so a changed file
// always has a new URL. index.html keeps its name and must revalidate.
const (
	cacheHashed = "public, max-age=31536000, immutable"
	cacheFixed  = "no-cache"
)

// Handler serves the embedded frontend. A nil logger uses slog.Default.
func Handler(logger *slog.Logger) http.Handler {
	root, err := fs.Sub(assets, "dist")
	if err != nil {
		panic(err)
	}
	return handler(root, logger)
}

// handler serves "/" as index.html and regular files under /assets/ from
// root. It never lists a directory. It logs the 503 for a missing build and
// any 5xx from the serving path once.
func handler(root fs.FS, logger *slog.Logger) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' blob: data:; connect-src 'self'; object-src 'none'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("Cache-Control", "no-store")
		name, cache, ok := route(r.URL.Path)
		if !ok {
			http.NotFound(w, r)
			return
		}
		if _, err := fs.Stat(root, "index.html"); err != nil {
			logger.ErrorContext(r.Context(), "request failed", "route", "GET /", "status", 503, "error", "frontend not built")
			http.Error(w, "Frontend not built; run make web-build", http.StatusServiceUnavailable)
			return
		}
		recorder := &statusWriter{ResponseWriter: w}
		serveFile(recorder, r, root, name, cache)
		// The status and path are enough to look; the file system error itself
		// is not exposed to the client.
		if recorder.status >= 500 {
			logger.ErrorContext(r.Context(), "request failed", "route", "GET /", "path", r.URL.Path, "status", recorder.status, "error", "frontend file error")
		}
	})
}

// serveFile answers 404 for a missing file or a directory and 500 for any
// other file system error.
func serveFile(w http.ResponseWriter, r *http.Request, root fs.FS, name, cache string) {
	f, err := root.Open(name)
	if errors.Is(err, fs.ErrNotExist) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if !info.Mode().IsRegular() {
		http.NotFound(w, r)
		return
	}
	content, ok := f.(io.ReadSeeker)
	if !ok {
		http.Error(w, "Frontend file is not seekable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Cache-Control", cache)
	http.ServeContent(w, r, name, info.ModTime(), content)
}

// route maps a URL path to a file name in the build and its cache policy.
func route(path string) (name, cache string, ok bool) {
	if path == "/" {
		return "index.html", cacheFixed, true
	}
	name = strings.TrimPrefix(path, "/")
	if strings.HasPrefix(name, "assets/") && name != "assets/" && !strings.HasSuffix(name, "/") && fs.ValidPath(name) {
		return name, cacheHashed, true
	}
	return "", "", false
}

// statusWriter records the first status for logging. Every error status,
// such as a 412 or 416 from http.ServeContent, uses no-store instead of the
// cache policy of the file.
type statusWriter struct {
	http.ResponseWriter
	status int
}

func (s *statusWriter) WriteHeader(code int) {
	if s.status == 0 {
		s.status = code
		if code >= 400 {
			s.Header().Set("Cache-Control", "no-store")
		}
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
