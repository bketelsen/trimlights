// Package webui embeds the frontend build produced by `make web-build`.
package webui

import (
	"embed"
	"io"
	"io/fs"
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

// Handler serves the embedded frontend build.
func Handler() http.Handler {
	root, err := fs.Sub(assets, "dist")
	if err != nil {
		panic(err)
	}
	return handler(root)
}

// handler serves "/" as index.html and regular files under /assets/ from
// root. It never lists a directory.
func handler(root fs.FS) http.Handler {
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
			http.Error(w, "Frontend not built; run make web-build", http.StatusServiceUnavailable)
			return
		}
		f, err := root.Open(name)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer func() { _ = f.Close() }()
		info, err := f.Stat()
		if err != nil || !info.Mode().IsRegular() {
			http.NotFound(w, r)
			return
		}
		content, ok := f.(io.ReadSeeker)
		if !ok {
			http.Error(w, "Frontend file is not seekable", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Cache-Control", cache)
		http.ServeContent(noStoreErrors{w}, r, name, info.ModTime(), content)
	})
}

// noStoreErrors makes every error status from http.ServeContent, such as 412
// or 416, use no-store instead of the cache policy of the file.
type noStoreErrors struct{ http.ResponseWriter }

func (w noStoreErrors) WriteHeader(status int) {
	if status >= 400 {
		w.Header().Set("Cache-Control", "no-store")
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w noStoreErrors) Unwrap() http.ResponseWriter { return w.ResponseWriter }

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
