package service

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"trimlights/internal/auth"
	"trimlights/internal/trimlight"
)

func registerControls(mux *http.ServeMux, worker *Worker, logger *slog.Logger) {
	mux.HandleFunc("POST /api/v1/controller/control", func(w http.ResponseWriter, r *http.Request) {
		if !requireJSON(w, r) {
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
			if errors.Is(err, trimlight.ErrStaleBaseline) {
				jsonError(w, 409, trimlight.ErrStaleBaseline.Error())
				return
			}
			serverError(w, r, logger, 503, enqueueMessage(err), err)
			return
		}
		select {
		case err = <-done:
			if err != nil {
				if errors.Is(err, trimlight.ErrStaleBaseline) {
					jsonResponse(w, 409, map[string]any{"error": trimlight.ErrStaleBaseline.Error(), "operation_id": id})
					return
				}
				logFailure(r, logger, 502, err, "operation_id", id)
				jsonResponse(w, 502, map[string]any{"error": operationMessage(err, msgControlFailed), "operation_id": id})
				return
			}
			jsonResponse(w, 200, map[string]string{"status": "verified", "operation_id": id})
		case <-r.Context().Done():
		}
	})
}

// Stable client messages for controller failures. The detail goes to the log
// and the operation record, never to the client: it can quote the controller
// address or storage text.
const (
	msgControllerUnavailable = "controller is unavailable; try again later"
	msgRefreshFailed         = "controller refresh failed; see the operation record"
	msgControlFailed         = "controller did not verify the change; refresh before trying again"
)

// enqueueMessage answers a refused enqueue. Worker refusals carry fixed text;
// any other cause, such as a storage error, gets a generic message.
func enqueueMessage(err error) string {
	var unavailable controlsUnavailable
	switch {
	case errors.Is(err, ErrDisabled), errors.Is(err, ErrQueueFull):
		return err.Error()
	case errors.As(err, &unavailable):
		return unavailable.reason
	}
	return msgControllerUnavailable
}

// operationMessage answers a failed controller operation. The web UI matches
// "Sunday" to explain the Sunday refusal, so that case keeps its fixed text.
func operationMessage(err error, fallback string) string {
	var unavailable controlsUnavailable
	switch {
	case errors.Is(err, trimlight.ErrSundayClock):
		return trimlight.ErrSundayClock.Error()
	case errors.As(err, &unavailable):
		return unavailable.reason
	}
	return fallback
}
