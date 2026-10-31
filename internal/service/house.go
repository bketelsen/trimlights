package service

import (
	"bytes"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"trimlights/internal/house"
	"trimlights/internal/storage"
)

func houseError(w http.ResponseWriter, r *http.Request, logger *slog.Logger, err error) {
	switch {
	case errors.Is(err, storage.ErrMissing):
		jsonError(w, 404, "photo not found")
	case errors.Is(err, storage.ErrHouseConflict):
		jsonError(w, 409, err.Error())
	case errors.Is(err, storage.ErrPhotoLimit):
		jsonError(w, 409, err.Error())
	case errors.Is(err, storage.ErrPhotoReference):
		jsonError(w, 400, err.Error())
	default:
		serverError(w, r, logger, 500, "could not read or save house photos and mapping", err)
	}
}
func houseResponse(w http.ResponseWriter, h storage.SavedHouse) {
	w.Header().Set("ETag", `"`+strconv.Itoa(h.Revision)+`"`)
	jsonResponse(w, 200, h)
}
func registerHouse(mux *http.ServeMux, store *storage.Store, logger *slog.Logger) {
	decoding := make(chan struct{}, 1)
	mux.HandleFunc("GET /api/v1/photos", func(w http.ResponseWriter, r *http.Request) {
		p, err := store.Photos(r.Context())
		if err != nil {
			houseError(w, r, logger, err)
			return
		}
		jsonResponse(w, 200, map[string]any{"photos": p})
	})
	mux.HandleFunc("POST /api/v1/photos", func(w http.ResponseWriter, r *http.Request) {
		select {
		case decoding <- struct{}{}:
			defer func() { <-decoding }()
		default:
			jsonError(w, 429, "another photo is processing; retry in a moment")
			return
		}
		raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, house.MaxUpload))
		if err != nil {
			jsonError(w, 413, "photo exceeds 12 MiB or could not be read; export a smaller JPEG")
			return
		}
		p, err := house.Sanitize(raw)
		if err != nil {
			jsonError(w, 400, err.Error())
			return
		}
		if r.Context().Err() != nil {
			return
		}
		saved, err := store.AddPhoto(r.Context(), p, actor(r))
		if err != nil {
			houseError(w, r, logger, err)
			return
		}
		w.Header().Set("Location", "/api/v1/photos/"+saved.ID+"/image")
		jsonResponse(w, 201, saved)
	})
	for _, kind := range []string{"image", "thumbnail"} {
		mux.HandleFunc("GET /api/v1/photos/{id}/"+kind, func(w http.ResponseWriter, r *http.Request) {
			raw, err := store.PhotoBytes(r.Context(), r.PathValue("id"), kind == "thumbnail")
			if err != nil {
				houseError(w, r, logger, err)
				return
			}
			w.Header().Set("Content-Type", "image/jpeg")
			w.Header().Set("Content-Disposition", `inline; filename="house.jpg"`)
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("Content-Length", strconv.Itoa(len(raw)))
			_, _ = io.Copy(w, bytes.NewReader(raw))
		})
	}
	mux.HandleFunc("GET /api/v1/house", func(w http.ResponseWriter, r *http.Request) {
		h, err := store.House(r.Context())
		if err != nil {
			houseError(w, r, logger, err)
			return
		}
		houseResponse(w, h)
	})
	mux.HandleFunc("POST /api/v1/house/validate", func(w http.ResponseWriter, r *http.Request) {
		var l house.Layout
		if !readJSONLimit(w, r, &l, 512<<10) {
			return
		}
		if err := l.Validate(); err != nil {
			jsonError(w, 400, err.Error())
			return
		}
		if err := store.ValidateHouse(r.Context(), l); err != nil {
			houseError(w, r, logger, err)
			return
		}
		jsonResponse(w, 200, map[string]any{"layout": l})
	})
	mux.HandleFunc("PUT /api/v1/house", func(w http.ResponseWriter, r *http.Request) {
		v, ok := revision(w, r)
		if !ok {
			return
		}
		var l house.Layout
		if !readJSONLimit(w, r, &l, 512<<10) {
			return
		}
		if err := l.Validate(); err != nil {
			jsonError(w, 400, err.Error())
			return
		}
		h, err := store.SaveHouse(r.Context(), v, l, actor(r))
		if err != nil {
			houseError(w, r, logger, err)
			return
		}
		houseResponse(w, h)
	})
	mux.HandleFunc("GET /api/v1/house/revisions", func(w http.ResponseWriter, r *http.Request) {
		h, err := store.HouseHistory(r.Context())
		if err != nil {
			houseError(w, r, logger, err)
			return
		}
		jsonResponse(w, 200, map[string]any{"revisions": h})
	})
}
