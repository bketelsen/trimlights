package service

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"trimlights/internal/auth"
	"trimlights/internal/trimlight"
)

func registerControls(mux *http.ServeMux, worker *Worker) {
	mux.HandleFunc("POST /api/v1/controller/control", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Content-Type") != "application/json" {
			jsonError(w, 415, "Content-Type must be application/json")
			return
		}
		var input struct {
			trimlight.ControlRequest
			AcknowledgeClockSync bool `json:"acknowledge_clock_sync"`
		}
		d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024))
		d.DisallowUnknownFields()
		if err := d.Decode(&input); err != nil || !input.AcknowledgeClockSync {
			jsonError(w, 400, "valid control and acknowledge_clock_sync:true are required")
			return
		}
		if err := d.Decode(new(any)); err != io.EOF {
			jsonError(w, 400, "expected one JSON object")
			return
		}
		if err := input.ControlRequest.Validate(); err != nil {
			jsonError(w, 400, err.Error())
			return
		}
		actor := "local-api"
		if identity, ok := auth.Identity(r.Context()); ok {
			actor = identity.Subject
		}
		id, done, err := worker.EnqueueControl(r.Context(), actor, input.ControlRequest)
		if err != nil {
			code := 503
			if errors.Is(err, trimlight.ErrStaleBaseline) {
				code = 409
			}
			jsonError(w, code, err.Error())
			return
		}
		select {
		case err = <-done:
			if err != nil {
				code := 502
				if errors.Is(err, trimlight.ErrStaleBaseline) {
					code = 409
				}
				jsonResponse(w, code, map[string]any{"error": err.Error(), "operation_id": id})
				return
			}
			jsonResponse(w, 200, map[string]string{"status": "verified", "operation_id": id})
		case <-r.Context().Done():
		}
	})
}
