package service

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strconv"

	"trimlights/internal/auth"
	"trimlights/internal/pattern"
	"trimlights/internal/storage"
)

func actor(r *http.Request) string {
	if identity, ok := auth.Identity(r.Context()); ok {
		return identity.Subject
	}
	return "local-api"
}
func readJSON(w http.ResponseWriter, r *http.Request, value any) bool {
	return readJSONLimit(w, r, value, 32<<10)
}
func readJSONLimit(w http.ResponseWriter, r *http.Request, value any, limit int64) bool {
	kind, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || kind != "application/json" {
		jsonError(w, 415, "Content-Type must be application/json")
		return false
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, limit))
	if err != nil {
		jsonError(w, 400, "JSON body is too large or unreadable")
		return false
	}
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || raw[0] != '{' {
		jsonError(w, 400, "expected one JSON object")
		return false
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err = d.Decode(value); err != nil {
		jsonError(w, 400, "invalid JSON: "+err.Error())
		return false
	}
	if err = d.Decode(new(any)); err != io.EOF {
		jsonError(w, 400, "expected one JSON object")
		return false
	}
	return true
}
func revision(w http.ResponseWriter, r *http.Request) (int, bool) {
	match := r.Header.Get("If-Match")
	if match == "" {
		jsonError(w, 428, "If-Match revision is required")
		return 0, false
	}
	if len(match) < 3 || match[0] != '"' || match[len(match)-1] != '"' {
		jsonError(w, 400, "If-Match must contain one quoted revision number")
		return 0, false
	}
	n, err := strconv.ParseInt(match[1:len(match)-1], 10, 32)
	if err != nil || n < 1 {
		jsonError(w, 400, "invalid revision")
		return 0, false
	}
	return int(n), true
}
func patternError(w http.ResponseWriter, r *http.Request, logger *slog.Logger, err error) {
	switch {
	case errors.Is(err, storage.ErrMissing):
		jsonError(w, 404, "pattern or cached snapshot not found")
	case errors.Is(err, storage.ErrConflict):
		jsonError(w, 409, storage.ErrConflict.Error())
	case errors.Is(err, storage.ErrNativeEdit):
		jsonError(w, 400, storage.ErrNativeEdit.Error())
	case errors.Is(err, storage.ErrNativeSource):
		jsonError(w, 400, storage.ErrNativeSource.Error())
	case errors.Is(err, pattern.ErrInvalid):
		jsonError(w, 400, err.Error())
	default:
		serverError(w, r, logger, 500, "could not save or read the pattern library", err)
	}
}
func patternResponse(w http.ResponseWriter, code int, p storage.SavedPattern) {
	w.Header().Set("ETag", `"`+strconv.Itoa(p.Revision)+`"`)
	jsonResponse(w, code, p)
}
func registerPatterns(mux *http.ServeMux, store *storage.Store, logger *slog.Logger) {
	mux.HandleFunc("GET /api/v1/patterns", func(w http.ResponseWriter, r *http.Request) {
		values, err := store.Patterns(r.Context(), r.URL.Query().Get("include_archived") == "1")
		if err != nil {
			patternError(w, r, logger, err)
			return
		}
		jsonResponse(w, 200, map[string]any{"patterns": values})
	})
	mux.HandleFunc("POST /api/v1/patterns", func(w http.ResponseWriter, r *http.Request) {
		var design pattern.Design
		if !readJSON(w, r, &design) {
			return
		}
		if err := design.Validate(); err != nil {
			jsonError(w, 400, err.Error())
			return
		}
		saved, err := store.CreatePattern(r.Context(), design, actor(r))
		if err != nil {
			patternError(w, r, logger, err)
			return
		}
		w.Header().Set("Location", "/api/v1/patterns/"+saved.ID)
		patternResponse(w, 201, saved)
	})
	mux.HandleFunc("GET /api/v1/patterns/{id}", func(w http.ResponseWriter, r *http.Request) {
		p, err := store.Pattern(r.Context(), r.PathValue("id"))
		if err != nil {
			patternError(w, r, logger, err)
			return
		}
		patternResponse(w, 200, p)
	})
	mux.HandleFunc("PUT /api/v1/patterns/{id}", func(w http.ResponseWriter, r *http.Request) {
		version, ok := revision(w, r)
		if !ok {
			return
		}
		var design pattern.Design
		if !readJSON(w, r, &design) {
			return
		}
		if err := design.Validate(); err != nil {
			jsonError(w, 400, err.Error())
			return
		}
		saved, err := store.UpdatePattern(r.Context(), r.PathValue("id"), version, design, actor(r))
		if err != nil {
			patternError(w, r, logger, err)
			return
		}
		patternResponse(w, 200, saved)
	})
	for _, action := range []string{"archive", "restore"} {
		mux.HandleFunc("POST /api/v1/patterns/{id}/"+action, func(w http.ResponseWriter, r *http.Request) {
			version, ok := revision(w, r)
			if !ok {
				return
			}
			var body struct{}
			if !readJSON(w, r, &body) {
				return
			}
			p, err := store.ArchivePattern(r.Context(), r.PathValue("id"), version, action == "archive", actor(r))
			if err != nil {
				patternError(w, r, logger, err)
				return
			}
			patternResponse(w, 200, p)
		})
	}
	mux.HandleFunc("GET /api/v1/patterns/{id}/revisions", func(w http.ResponseWriter, r *http.Request) {
		history, err := store.PatternHistory(r.Context(), r.PathValue("id"))
		if err != nil {
			patternError(w, r, logger, err)
			return
		}
		jsonResponse(w, 200, map[string]any{"revisions": history})
	})
	mux.HandleFunc("POST /api/v1/patterns/import-controller", func(w http.ResponseWriter, r *http.Request) {
		var body struct{}
		if !readJSON(w, r, &body) {
			return
		}
		result, err := store.ImportControllerPatterns(r.Context(), actor(r))
		if err != nil {
			patternError(w, r, logger, err)
			return
		}
		jsonResponse(w, 200, result)
	})
}
