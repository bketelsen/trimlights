package webui

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
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
