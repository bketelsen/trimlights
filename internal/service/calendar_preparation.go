package service

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"trimlights/internal/pattern"
	"trimlights/internal/schedule"
	"trimlights/internal/storage"
	"trimlights/internal/trimlight"
)

type preparationBlocker struct {
	Code        string `json:"code"`
	Message     string `json:"message"`
	ReferenceID string `json:"reference_id,omitempty"`
}
type calendarBinding struct {
	ID       string          `json:"id"`
	Name     string          `json:"name"`
	Required bool            `json:"required"`
	Action   schedule.Action `json:"action"`
	Status   string          `json:"status"`
	Message  string          `json:"message"`
	Slot     byte            `json:"slot,omitempty"`
}
type calendarSource struct {
	SnapshotAt     time.Time `json:"snapshot_at"`
	BaselineSHA256 string    `json:"baseline_sha256,omitempty"`
}
type readinessReport struct {
	Timezone         string               `json:"timezone"`
	ExecutionEnabled bool                 `json:"execution_enabled"`
	CheckedAt        time.Time            `json:"checked_at"`
	Until            time.Time            `json:"until"`
	Revision         int                  `json:"schedule_revision"`
	Source           calendarSource       `json:"source"`
	Blockers         []preparationBlocker `json:"blockers"`
	Bindings         []calendarBinding    `json:"bindings"`
	Current          schedule.Segment     `json:"current"`
	Next             *schedule.Segment    `json:"next"`
	Issues           []schedule.Issue     `json:"issues"`
}
type nativeDraftEntry struct {
	Native trimlight.Schedule `json:"native"`
	Reason string             `json:"reason"`
	Rule   *schedule.Rule     `json:"rule,omitempty"`
}
type nativeDraftReport struct {
	Timezone         string             `json:"timezone"`
	ExecutionEnabled bool               `json:"execution_enabled"`
	Revision         int                `json:"schedule_revision"`
	Source           calendarSource     `json:"source"`
	Entries          []nativeDraftEntry `json:"entries"`
}

func sourceOf(s *trimlight.Snapshot) calendarSource {
	if s == nil {
		return calendarSource{}
	}
	return calendarSource{s.QueriedAt, s.StableSHA256}
}
func sameParameters(a, b string) bool {
	x, e := hex.DecodeString(a)
	y, f := hex.DecodeString(b)
	return e == nil && f == nil && len(x) == 156 && bytes.Equal(x, y)
}
func nativeDesignMatches(d pattern.Design, s *trimlight.Snapshot) bool {
	if d.Validate() != nil || d.Effect != "native" || d.Native == nil || s == nil {
		return false
	}
	for _, p := range s.Patterns {
		if p.ID == d.Native.ID && sameParameters(p.ParametersHex, d.Native.ParametersHex) {
			return true
		}
	}
	return false
}
func prepareReadiness(plan schedule.Plan, input storage.CalendarInputs, now time.Time) (readinessReport, error) {
	until := now.Add(7 * 24 * time.Hour)
	preview, err := schedule.Evaluate(plan, now, until)
	if err != nil {
		return readinessReport{}, err
	}
	out := readinessReport{Timezone: plan.Timezone, CheckedAt: now, Until: until, Revision: input.Revision, Source: sourceOf(input.Observation.Snapshot), Bindings: []calendarBinding{}, Issues: preview.Issues, Current: preview.Segments[0], Blockers: []preparationBlocker{
		{Code: "sunday_clock_unverified", Message: "Sunday controller clock encoding still needs verification before seven-day scheduling."},
		{Code: "timer_handoff_unverified", Message: "Native timer boundaries still need testing in MANUAL/OFF before Calendar can own the lights."},
		{Code: "runtime_unavailable", Message: "Automatic scheduling cannot be enabled yet; controller protocol and handoff verification are still required."},
	}}
	if len(preview.Segments) > 1 {
		out.Next = &preview.Segments[1]
	}
	snap, e := trimlight.ValidatedNativeSnapshot(input.Observation.Snapshot)
	if e != nil {
		out.Blockers = append(out.Blockers, preparationBlocker{Code: "unsupported_snapshot", Message: "Use Refresh controller on Home to load the verified controller layout."})
	} else {
		at := snap.QueriedAt
		if at.IsZero() || at.After(now) || now.Sub(at) > 5*time.Minute {
			out.Blockers = append(out.Blockers, preparationBlocker{Code: "stale_snapshot", Message: "The cached controller observation is missing a valid time or is older than five minutes. Refresh on Home, then check again."})
		}
	}
	if input.Observation.LastError != "" {
		out.Blockers = append(out.Blockers, preparationBlocker{Code: "observation_error", Message: "The last controller operation failed. Refresh on Home and resolve the error before handoff."})
	}
	bind := func(id, name string, a schedule.Action, required bool) {
		b := calendarBinding{ID: id, Name: name, Required: required, Action: a}
		switch {
		case a.Kind == "off":
			b.Status = "off"
			b.Message = "Uses the verified OFF command."
		case input.Pinned[a].Design.Effect != "native":
			b.Status = "custom_effect"
			b.Message = "This software design has no verified controller playback command. Choose an imported native pattern."
		case snap == nil:
			b.Status = "unverified"
			b.Message = "A supported controller snapshot is required to check this pinned pattern."
		case !nativeDesignMatches(input.Pinned[a].Design, snap):
			b.Status = "changed_slot"
			b.Message = "The original controller slot no longer matches this pinned pattern. No substitute slot was selected."
		default:
			b.Status = "matched"
			b.Slot = input.Pinned[a].Design.Native.ID
			b.Message = "Pinned parameters match the original controller slot."
		}
		if required && b.Status != "off" && b.Status != "matched" {
			out.Blockers = append(out.Blockers, preparationBlocker{Code: b.Status, Message: b.Name + ": " + b.Message, ReferenceID: id})
		}
		out.Bindings = append(out.Bindings, b)
	}
	for _, r := range plan.Rules {
		bind(r.ID, r.Name, r.Action, r.Enabled)
	}
	if o := plan.Override; o != nil {
		bind(o.ID, "Temporary override", o.Action, o.ExpiresAt.After(now))
	}
	for _, s := range preview.Segments {
		if s.Conflict {
			out.Blockers = append(out.Blockers, preparationBlocker{Code: "calendar_conflict", Message: "Equal-priority rules conflict within the next seven elapsed days; the evaluator proposes OFF. Review the timeline."})
			break
		}
	}
	if len(preview.Issues) > 0 {
		out.Blockers = append(out.Blockers, preparationBlocker{Code: "calendar_adjustments", Message: "The next seven elapsed days include skipped or adjusted occurrences. Review the evaluator notices and timeline."})
	}
	return out, nil
}

func prepareNativeDraft(plan schedule.Plan, input storage.CalendarInputs, today string) (nativeDraftReport, error) {
	date, err := time.Parse("2006-01-02", today)
	if err != nil || len(today) != 10 || date.Year() < 1970 || date.Year() > 2199 {
		return nativeDraftReport{}, errors.New("today_only_date must be an explicit YYYY-MM-DD between 1970 and 2199")
	}
	snap, err := trimlight.ValidatedNativeSnapshot(input.Observation.Snapshot)
	if err != nil {
		return nativeDraftReport{}, trimlight.ErrUnsupportedControl
	}
	out := nativeDraftReport{Timezone: plan.Timezone, Revision: input.Revision, Source: sourceOf(snap), Entries: []nativeDraftEntry{}}
	for _, n := range snap.Schedules {
		entry := nativeDraftEntry{Native: n}
		id := fmt.Sprintf("native-timer-%d", n.Slot)
		switch {
		case n.Repetition > 1:
			entry.Reason = "Unknown repetition; recreate this entry manually after checking the official app."
		case n.Start >= n.Off:
			entry.Reason = "Equal or overnight native times are not verified; review them manually."
		default:
			matches := []storage.SavedPattern{}
			for _, p := range input.Heads {
				if !p.Archived && p.Design.Native != nil && p.Design.Native.ID == n.PatternID && nativeDesignMatches(p.Design, snap) {
					matches = append(matches, p)
				}
			}
			if len(matches) == 0 {
				entry.Reason = "No current Library pattern matches this slot. Import controller patterns in Library, or resolve a changed/archived import, then check again."
				break
			}
			if len(matches) > 1 {
				entry.Reason = "Multiple Library patterns match this slot. Choose a pattern explicitly in a new rule instead."
				break
			}
			for _, r := range plan.Rules {
				if r.ID == id {
					entry.Reason = "This timer's rule ID is already in the draft; it will not be overwritten."
					break
				}
			}
			if plan.Override != nil && plan.Override.ID == id {
				entry.Reason = "This timer's rule ID is already used by the override; it will not be overwritten."
			}
			if entry.Reason != "" {
				break
			}
			if len(plan.Rules) >= 100 {
				entry.Reason = "The draft already has the maximum 100 rules."
				break
			}
			p := matches[0]
			rule := schedule.Rule{ID: id, Name: fmt.Sprintf("Controller timer %d", n.Slot), Enabled: n.Enabled, Kind: "weekly", Weekdays: []int{0, 1, 2, 3, 4, 5, 6}, Start: schedule.Boundary{Kind: "clock", Time: n.Start}, End: schedule.Boundary{Kind: "clock", Time: n.Off}, Action: schedule.Action{Kind: "pattern", PatternID: p.ID, Revision: p.Revision}}
			entry.Reason = "Copy as a daily rule; review times and priority before saving."
			if n.Repetition == 0 {
				rule.Kind = "exception"
				rule.Weekdays = []int{}
				rule.From = today
				rule.Through = today
				entry.Reason = "Copy as a one-day rule on the date you chose. The snapshot does not establish the original date."
			}
			entry.Rule = &rule
		}
		out.Entries = append(out.Entries, entry)
	}
	return out, nil
}

func registerCalendarPreparation(mux *http.ServeMux, store *storage.Store, logger *slog.Logger) {
	for _, kind := range []string{"readiness", "native-draft"} {
		mux.HandleFunc("POST /api/v1/schedule/"+kind, func(w http.ResponseWriter, r *http.Request) {
			expected, ok := revision(w, r)
			if !ok {
				return
			}
			var plan schedule.Plan
			var today string
			if kind == "readiness" {
				var in struct {
					Plan schedule.Plan `json:"plan"`
				}
				if !readJSONLimit(w, r, &in, 128<<10) {
					return
				}
				plan = in.Plan
			} else {
				var in struct {
					Plan  schedule.Plan `json:"plan"`
					Today string        `json:"today_only_date"`
				}
				if !readJSONLimit(w, r, &in, 128<<10) {
					return
				}
				plan, today = in.Plan, in.Today
			}
			if err := plan.Validate(); err != nil {
				jsonError(w, 400, err.Error())
				return
			}
			input, err := store.CalendarInputs(r.Context(), expected, plan)
			if err != nil {
				switch {
				case errors.Is(err, storage.ErrScheduleConflict):
					jsonError(w, 409, err.Error())
				case errors.Is(err, storage.ErrScheduleReference):
					jsonError(w, 400, err.Error())
				default:
					serverError(w, r, logger, 500, "cannot read calendar preparation inputs", err)
				}
				return
			}
			var result any
			if kind == "readiness" {
				result, err = prepareReadiness(plan, input, time.Now().UTC())
			} else {
				result, err = prepareNativeDraft(plan, input, today)
			}
			if errors.Is(err, trimlight.ErrUnsupportedControl) {
				jsonError(w, 409, "Refresh controller on Home to load a supported snapshot before copying native timers.")
				return
			}
			if err != nil {
				jsonError(w, 400, err.Error())
				return
			}
			jsonResponse(w, 200, result)
		})
	}
}
