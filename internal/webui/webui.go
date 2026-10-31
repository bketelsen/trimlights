// Package webui embeds the frontend build produced by `make web-build`.
package webui

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed all:dist
var assets embed.FS

// rootFiles are the public files that browsers request at fixed root paths for
// home-screen use. The value is an explicit Content-Type, or empty to detect it.
var rootFiles = map[string]string{
	"/manifest.webmanifest": "application/manifest+json",
	"/apple-touch-icon.png": "",
	"/icon-192.png":         "",
	"/icon-512.png":         "",
}

func Handler() http.Handler {
	root, _ := fs.Sub(assets, "dist")
	return handler(root)
}

func handler(root fs.FS) http.Handler {
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
		contentType, rootFile := rootFiles[r.URL.Path]
		if r.URL.Path != "/" && !rootFile && !strings.HasPrefix(r.URL.Path, "/assets/") {
			http.NotFound(w, r)
			return
		}
		if _, err := fs.Stat(root, "index.html"); err != nil {
			http.Error(w, "Frontend not built; run make web-build", http.StatusServiceUnavailable)
			return
		}
		if contentType != "" {
			w.Header().Set("Content-Type", contentType)
		}
		files.ServeHTTP(w, r)
	})
}
