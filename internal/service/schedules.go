package service

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"
	"trimlights/internal/schedule"
	"trimlights/internal/storage"
)

func scheduleResponse(w http.ResponseWriter, s storage.SavedSchedule) {
	w.Header().Set("ETag", `"`+strconv.Itoa(s.Revision)+`"`)
	jsonResponse(w, 200, s)
}
func registerSchedules(mux *http.ServeMux, store *storage.Store, logger *slog.Logger) {
	mux.HandleFunc("GET /api/v1/schedule", func(w http.ResponseWriter, r *http.Request) {
		saved, err := store.Schedule(r.Context())
		if err != nil {
			serverError(w, r, logger, 500, "cannot read calendar", err)
			return
		}
		scheduleResponse(w, saved)
	})
	mux.HandleFunc("POST /api/v1/schedule/validate", func(w http.ResponseWriter, r *http.Request) {
		expected, ok := revision(w, r)
		if !ok {
			return
		}
		var p schedule.Plan
		if !readJSONLimit(w, r, &p, 128<<10) {
			return
		}
		if err := p.Validate(); err != nil {
			jsonError(w, 400, err.Error())
			return
		}
		if err := store.ValidateSchedule(r.Context(), expected, p); err != nil {
			switch {
			case errors.Is(err, storage.ErrScheduleConflict):
				jsonError(w, 409, err.Error())
			case errors.Is(err, storage.ErrScheduleReference):
				jsonError(w, 400, err.Error()+"; restore the matching household backup or choose an available pattern before importing")
			default:
				serverError(w, r, logger, 500, "cannot validate calendar", err)
			}
			return
		}
		// JSON null arrays are valid empty collections in Go. Return arrays that
		// the browser can render, including exceptions without weekdays.
		if p.Rules == nil {
			p.Rules = []schedule.Rule{}
		}
		for i := range p.Rules {
			if p.Rules[i].Weekdays == nil {
				p.Rules[i].Weekdays = []int{}
			}
		}
		jsonResponse(w, 200, map[string]any{"plan": p, "execution_enabled": false})
	})
	mux.HandleFunc("PUT /api/v1/schedule", func(w http.ResponseWriter, r *http.Request) {
		expected, ok := revision(w, r)
		if !ok {
			return
		}
		var p schedule.Plan
		if !readJSONLimit(w, r, &p, 128<<10) {
			return
		}
		if err := p.Validate(); err != nil {
			jsonError(w, 400, err.Error())
			return
		}
		saved, err := store.SaveSchedule(r.Context(), expected, p, actor(r))
		if errors.Is(err, storage.ErrScheduleConflict) {
			jsonError(w, 409, err.Error())
			return
		}
		if errors.Is(err, storage.ErrScheduleReference) {
			jsonError(w, 400, err.Error())
			return
		}
		if err != nil {
			serverError(w, r, logger, 500, "cannot save calendar", err)
			return
		}
		scheduleResponse(w, saved)
	})
	mux.HandleFunc("GET /api/v1/schedule/revisions", func(w http.ResponseWriter, r *http.Request) {
		history, err := store.ScheduleHistory(r.Context())
		if err != nil {
			serverError(w, r, logger, 500, "cannot read calendar history", err)
			return
		}
		jsonResponse(w, 200, map[string]any{"revisions": history})
	})
	mux.HandleFunc("POST /api/v1/schedule/preview", func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			FromDate string        `json:"from_date"`
			Days     int           `json:"days"`
			Plan     schedule.Plan `json:"plan"`
			From     time.Time     `json:"from"`
			Until    time.Time     `json:"until"`
		}
		if !readJSONLimit(w, r, &input, 128<<10) {
			return
		}
		var preview schedule.Preview
		var err error
		if input.FromDate != "" {
			if !input.From.IsZero() || !input.Until.IsZero() {
				jsonError(w, 400, "choose local dates or absolute bounds, not both")
				return
			}
			preview, err = schedule.EvaluateDates(input.Plan, input.FromDate, input.Days)
		} else {
			if input.Days != 0 {
				jsonError(w, 400, "days requires from_date")
				return
			}
			preview, err = schedule.Evaluate(input.Plan, input.From, input.Until)
		}
		if err != nil {
			jsonError(w, 400, err.Error())
			return
		}
		jsonResponse(w, 200, preview)
	})
	mux.HandleFunc("GET /api/v1/patterns/{id}/schedule-references", func(w http.ResponseWriter, r *http.Request) {
		if _, err := store.Pattern(r.Context(), r.PathValue("id")); err != nil {
			patternError(w, r, logger, err)
			return
		}
		refs, err := store.PatternScheduleReferences(r.Context(), r.PathValue("id"))
		if err != nil {
			serverError(w, r, logger, 500, "cannot read calendar references", err)
			return
		}
		jsonResponse(w, 200, map[string]any{"references": refs})
	})
}
