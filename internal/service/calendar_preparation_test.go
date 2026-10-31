package service

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"trimlights/internal/pattern"
	"trimlights/internal/schedule"
	"trimlights/internal/storage"
	"trimlights/internal/trimlight"
)

func preparationFixture(t *testing.T) (*storage.Store, *Worker, *trimlight.Snapshot, schedule.Plan, storage.SavedPattern) {
	t.Helper()
	s, w, _, snap := nativeService(t)
	imported, err := s.ImportControllerPatterns(context.Background(), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	p := imported.Imported[0]
	plan := schedule.Default()
	plan.Timezone = "UTC"
	plan.Rules = []schedule.Rule{{ID: "evening", Name: "Evening", Enabled: true, Kind: "weekly", Weekdays: []int{0, 1, 2, 3, 4, 5, 6}, Start: schedule.Boundary{Kind: "clock", Time: "18:00"}, End: schedule.Boundary{Kind: "clock", Time: "23:00"}, Action: schedule.Action{Kind: "pattern", PatternID: p.ID, Revision: 1}}}
	return s, w, snap, plan, p
}
func hasBlocker(r readinessReport, code string) bool {
	for _, b := range r.Blockers {
		if b.Code == code {
			return true
		}
	}
	return false
}
func preparationInputs(t *testing.T, s *storage.Store, revision int, plan schedule.Plan) storage.CalendarInputs {
	t.Helper()
	in, err := s.CalendarInputs(context.Background(), revision, plan)
	if err != nil {
		t.Fatal(err)
	}
	return in
}
func TestReadinessPinsHistoricalRevisionAndDetectsSlotReuse(t *testing.T) {
	s, _, snap, plan, p := preparationFixture(t)
	ctx := context.Background()
	if _, err := s.SaveSchedule(ctx, 1, plan, "fixture"); err != nil {
		t.Fatal(err)
	}
	d := p.Design
	d.Name = "Renamed"
	d.Favorite = true
	if _, err := s.UpdatePattern(ctx, p.ID, 1, d, "fixture"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ArchivePattern(ctx, p.ID, 2, true, "fixture"); err != nil {
		t.Fatal(err)
	}
	in := preparationInputs(t, s, 2, plan)
	if in.Pinned[plan.Rules[0].Action].Revision != 1 {
		t.Fatal("lost historical pin")
	}
	now := time.Date(2026, 10, 28, 20, 0, 0, 0, time.UTC)
	in.Observation.Snapshot.QueriedAt = now
	report, err := prepareReadiness(plan, in, now)
	if err != nil || report.Bindings[0].Status != "matched" || report.ExecutionEnabled || report.Current.Action != plan.Rules[0].Action || report.Next == nil || report.Next.Action.Kind != "off" {
		t.Fatal(report, err)
	}
	for _, code := range []string{"sunday_clock_unverified", "timer_handoff_unverified", "runtime_unavailable"} {
		if !hasBlocker(report, code) {
			t.Fatal(code)
		}
	}
	// A different slot with the original bytes cannot replace the original slot.
	raw, _ := hex.DecodeString(snap.RawHex)
	copy(raw[52+182:52+182+156], raw[52:52+156])
	raw[52] ^= 0xff
	changed, err := trimlight.ParseSnapshot(raw, 3306)
	if err != nil {
		t.Fatal(err)
	}
	changed.QueriedAt = now
	in.Observation.Snapshot = changed
	report, err = prepareReadiness(plan, in, now)
	if err != nil || !hasBlocker(report, "changed_slot") || report.Bindings[0].Status != "changed_slot" {
		t.Fatal(report, err)
	}
}
func TestReadinessInactiveReferencesFreshnessConflictsAndRecovery(t *testing.T) {
	s, _, _, plan, _ := preparationFixture(t)
	custom, err := s.CreatePattern(context.Background(), pattern.Design{Schema: 1, Name: "Custom", Effect: "static", Brightness: 100, Palette: []pattern.Run{{Color: "#ff0000", Count: 1}}}, "fixture")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 28, 20, 0, 0, 0, time.UTC)
	a := schedule.Action{Kind: "pattern", PatternID: custom.ID, Revision: 1}
	r := plan.Rules[0]
	r.ID = "custom"
	r.Name = "Custom"
	r.Action = a
	r.Enabled = false
	plan.Rules = append(plan.Rules, r)
	plan.Override = &schedule.Override{ID: "expired", Action: a, StartsAt: now.Add(-2 * time.Hour), ExpiresAt: now.Add(-time.Hour)}
	in := preparationInputs(t, s, 1, plan)
	in.Observation.Snapshot.QueriedAt = now
	report, err := prepareReadiness(plan, in, now)
	if err != nil || hasBlocker(report, "custom_effect") || report.Bindings[1].Required || report.Bindings[2].Required {
		t.Fatal(report, err)
	}
	plan.Rules[1].Enabled = true
	report, err = prepareReadiness(plan, in, now)
	if err != nil || !hasBlocker(report, "custom_effect") || !hasBlocker(report, "calendar_conflict") || report.Current.Action.Kind != "off" {
		t.Fatal(report, err)
	}
	// Evaluate current intent directly after a hypothetical restart; no past events.
	plan.Rules[1].Enabled = false
	plan.Override = nil
	later := now.Add(4 * time.Hour)
	report, err = prepareReadiness(plan, in, later)
	if err != nil || report.Current.Action.Kind != "off" || !hasBlocker(report, "stale_snapshot") {
		t.Fatal(report, err)
	}
	for _, at := range []time.Time{{}, now.Add(time.Second), now.Add(-6 * time.Minute)} {
		in.Observation.Snapshot.QueriedAt = at
		report, err = prepareReadiness(plan, in, now)
		if err != nil || !hasBlocker(report, "stale_snapshot") {
			t.Fatal(at, report, err)
		}
	}
	in.Observation.LastError = "fixture outage"
	in.Observation.Snapshot = nil
	report, err = prepareReadiness(plan, in, now)
	if err != nil || !hasBlocker(report, "unsupported_snapshot") || !hasBlocker(report, "observation_error") || report.Bindings[0].Status != "unverified" {
		t.Fatal(report, err)
	}
}
func TestPreparationUsesCanonicalBytesAndCopiesDisabledTimers(t *testing.T) {
	s, _, _, _, _ := preparationFixture(t)
	plan := schedule.Default()
	in := preparationInputs(t, s, 1, plan)
	// The cached JSON projection is not trusted: actual values come from raw bytes.
	in.Observation.Snapshot.Schedules = nil
	in.Observation.Snapshot.Patterns = nil
	report, err := prepareNativeDraft(plan, in, "2026-12-31")
	if err != nil || len(report.Entries) != 2 || report.ExecutionEnabled {
		t.Fatal(report, err)
	}
	for _, e := range report.Entries {
		if e.Rule == nil {
			t.Fatal(e)
		}
	}
	if !report.Entries[0].Rule.Enabled || report.Entries[0].Rule.Kind != "weekly" || report.Entries[1].Rule.Enabled || report.Entries[1].Rule.Kind != "exception" || report.Entries[1].Rule.From != "2026-12-31" {
		t.Fatal(report)
	}
	for _, entry := range report.Entries {
		plan.Rules = append(plan.Rules, *entry.Rule)
	}
	if err = plan.Validate(); err != nil {
		t.Fatal(err)
	}
	// Existing rule IDs are protected; review does not modify them or save a revision.
	report, err = prepareNativeDraft(plan, in, "2026-12-31")
	if err != nil || report.Entries[0].Rule != nil || !strings.Contains(report.Entries[0].Reason, "already") {
		t.Fatal(report, err)
	}
	saved, err := s.Schedule(context.Background())
	if err != nil || saved.Revision != 1 || len(saved.Plan.Rules) != 0 {
		t.Fatal(saved, err)
	}
}
func TestNativeDraftRejectsAmbiguitiesAndRetainsEveryEntry(t *testing.T) {
	s, _, snap, _, _ := preparationFixture(t)
	plan := schedule.Default()
	in := preparationInputs(t, s, 1, plan)
	for _, date := range []string{"", "2026-02-30", "2200-01-01", "2026-1-01"} {
		if _, err := prepareNativeDraft(plan, in, date); err == nil {
			t.Fatal(date)
		}
	}
	raw, _ := hex.DecodeString(snap.RawHex)
	raw[3308] = 9                // Unknown repetition remains visible.
	raw[3316], raw[3318] = 23, 1 // Second entry's end precedes start.
	altered, err := trimlight.ParseSnapshot(raw, 3306)
	if err != nil {
		t.Fatal(err)
	}
	in.Observation.Snapshot = altered
	report, err := prepareNativeDraft(plan, in, "2026-12-31")
	if err != nil || len(report.Entries) != 2 || report.Entries[0].Rule != nil || report.Entries[1].Rule != nil || !strings.Contains(report.Entries[0].Reason, "Unknown") || !strings.Contains(report.Entries[1].Reason, "overnight") {
		t.Fatal(report, err)
	}
	in.Observation.Snapshot = snap
	heads := in.Heads
	in.Heads = nil
	report, err = prepareNativeDraft(plan, in, "2026-12-31")
	if err != nil || report.Entries[0].Rule != nil || !strings.Contains(report.Entries[0].Reason, "No current") {
		t.Fatal(report, err)
	}
	in.Heads = append(heads, heads...)
	report, err = prepareNativeDraft(plan, in, "2026-12-31")
	if err != nil || report.Entries[0].Rule != nil || !strings.Contains(report.Entries[0].Reason, "Multiple") {
		t.Fatal(report, err)
	}
	in.Observation.Snapshot.StableSHA256 = strings.Repeat("0", 64)
	if _, err = prepareNativeDraft(plan, in, "2026-12-31"); !errors.Is(err, trimlight.ErrUnsupportedControl) {
		t.Fatal(err)
	}
}

// Neither preparation endpoint has any reason to access a live controller.
type forbiddenPreparationController struct{}

func (forbiddenPreparationController) Snapshot(context.Context, int) (*trimlight.Snapshot, error) {
	panic("preparation contacted hardware")
}
func TestPreparationAPIBoundariesAndNoMutations(t *testing.T) {
	s, _, _, plan, _ := preparationFixture(t)
	w := NewWorker(s, forbiddenPreparationController{}, 3306, nil)
	h := Handler(s, w, strings.Repeat("x", 32), nil)
	ctx := context.Background()
	beforeOps, _ := s.Operations(ctx)
	beforePatterns, _ := s.Patterns(ctx, true)
	beforeObs, _ := s.Observation(ctx)
	call := func(path, raw, revision string, auth bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/api/v1/schedule/"+path, strings.NewReader(raw))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("If-Match", revision)
		if auth {
			r.Header.Set("Authorization", "Bearer "+strings.Repeat("x", 32))
		}
		out := httptest.NewRecorder()
		h.ServeHTTP(out, r)
		return out
	}
	for _, path := range []string{"readiness", "native-draft"} {
		body := map[string]any{"plan": plan}
		if path == "native-draft" {
			body["today_only_date"] = "2026-12-31"
		}
		raw, _ := json.Marshal(body)
		for _, tc := range []struct {
			raw, revision string
			auth          bool
			want          int
		}{
			{string(raw), `"1"`, false, 401}, {string(raw), "", true, 428}, {string(raw), `"2"`, true, 409},
			{"null", `"1"`, true, 400}, {string(raw) + " {}", `"1"`, true, 400}, {strings.Repeat(" ", 128<<10) + string(raw), `"1"`, true, 400},
			{`{"plan":{"schema":1,"timezone":"UTC","rules":[]},"execution_enabled":true}`, `"1"`, true, 400},
			{string(raw), `"1"`, true, 200},
		} {
			out := call(path, tc.raw, tc.revision, tc.auth)
			if out.Code != tc.want {
				t.Fatalf("%s: %d want %d: %s", path, out.Code, tc.want, out.Body.String())
			}
			if out.Code == 200 && (!strings.Contains(out.Body.String(), `"execution_enabled":false`) || strings.Contains(out.Body.String(), `"plain_hex"`)) {
				t.Fatal(out.Body.String())
			}
		}
	}
	plan.Rules[0].Action.PatternID = "missing"
	raw, _ := json.Marshal(map[string]any{"plan": plan})
	if out := call("readiness", string(raw), `"1"`, true); out.Code != 400 {
		t.Fatal(out.Code, out.Body.String())
	}
	afterOps, _ := s.Operations(ctx)
	afterPatterns, _ := s.Patterns(ctx, true)
	afterObs, _ := s.Observation(ctx)
	for _, pair := range [][2]any{{beforeOps, afterOps}, {beforePatterns, afterPatterns}, {beforeObs, afterObs}} {
		a, _ := json.Marshal(pair[0])
		b, _ := json.Marshal(pair[1])
		if !bytes.Equal(a, b) {
			t.Fatal("preparation mutated storage")
		}
	}
	saved, err := s.Schedule(ctx)
	if err != nil || saved.Revision != 1 || len(saved.Plan.Rules) != 0 || w.Status().Queued != 0 {
		t.Fatal(saved, err, w.Status())
	}
}

// A native revision saved before the 2026-10-30 format-character rule fails
// Design.Validate, so its pin reports changed_slot. The API cannot store such a
// name now, so the test edits the loaded pin. Renaming alone keeps the old pin;
// recovery needs the rule to pin the renamed revision.
func TestReadinessLegacyFormatCharacterNameNeedsRepin(t *testing.T) {
	s, _, _, plan, p := preparationFixture(t)
	ctx := context.Background()
	if _, err := s.SaveSchedule(ctx, 1, plan, "fixture"); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 28, 20, 0, 0, 0, time.UTC)
	a := plan.Rules[0].Action
	in := preparationInputs(t, s, 2, plan)
	legacy := in.Pinned[a]
	legacy.Design.Name = "Por\u200bch"
	in.Pinned[a] = legacy
	in.Observation.Snapshot.QueriedAt = now
	report, err := prepareReadiness(plan, in, now)
	if err != nil || report.Bindings[0].Status != "changed_slot" {
		t.Fatal(report, err)
	}
	d := p.Design
	d.Name = "Porch"
	renamed, err := s.UpdatePattern(ctx, p.ID, 1, d, "fixture")
	if err != nil {
		t.Fatal(err)
	}
	in = preparationInputs(t, s, 2, plan)
	if in.Pinned[a].Revision != 1 {
		t.Fatal("rename moved the calendar pin")
	}
	plan.Rules[0].Action.Revision = renamed.Revision
	if _, err = s.SaveSchedule(ctx, 2, plan, "fixture"); err != nil {
		t.Fatal(err)
	}
	in = preparationInputs(t, s, 3, plan)
	in.Observation.Snapshot.QueriedAt = now
	report, err = prepareReadiness(plan, in, now)
	if err != nil || report.Bindings[0].Status != "matched" {
		t.Fatal(report, err)
	}
}
