package storage

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"
	"trimlights/internal/schedule"
)

func calendar(p SavedPattern) schedule.Plan {
	plan := schedule.Default()
	plan.Rules = []schedule.Rule{{ID: "evenings", Name: "Evenings", Enabled: true, Kind: "weekly", Weekdays: []int{0, 1, 2, 3, 4, 5, 6}, Start: schedule.Boundary{Kind: "clock", Time: "19:30"}, End: schedule.Boundary{Kind: "clock", Time: "22:59"}, Action: schedule.Action{Kind: "pattern", PatternID: p.ID, Revision: p.Revision}}}
	return plan
}
func TestCalendarPinnedReferencesConcurrentEditsAndRestore(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	p, err := s.CreatePattern(ctx, design(), "owner")
	if err != nil {
		t.Fatal(err)
	}
	plan := calendar(p)
	saved, err := s.SaveSchedule(ctx, 1, plan, "owner")
	if err != nil || saved.Revision != 2 {
		t.Fatal(saved, err)
	}
	d := p.Design
	d.Name = "Changed pattern"
	if _, err = s.UpdatePattern(ctx, p.ID, 1, d, "wife"); err != nil {
		t.Fatal(err)
	}
	pinned, err := s.Schedule(ctx)
	if err != nil || pinned.Plan.Rules[0].Action.Revision != 1 {
		t.Fatal(pinned, err)
	}
	if _, err = s.ArchivePattern(ctx, p.ID, 2, true, "owner"); err != nil {
		t.Fatal(err)
	}
	// Existing references survive archive and unrelated calendar edits.
	if _, err = s.SaveSchedule(ctx, 2, plan, "owner"); err != nil {
		t.Fatal(err)
	}
	bad := calendar(p)
	duplicate := bad.Rules[0]
	duplicate.ID = "new"
	bad.Rules = append(bad.Rules, duplicate)
	if _, err = s.SaveSchedule(ctx, 3, bad, "owner"); !errors.Is(err, ErrScheduleReference) {
		t.Fatal(err)
	}
	refs, err := s.PatternScheduleReferences(ctx, p.ID)
	if err != nil || len(refs) != 1 || refs[0].Action.Revision != 1 {
		t.Fatal(refs, err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, actor := range []string{"owner", "wife"} {
		wg.Add(1)
		go func() { defer wg.Done(); _, err := s.SaveSchedule(ctx, 3, plan, actor); results <- err }()
	}
	wg.Wait()
	close(results)
	successes, conflicts := 0, 0
	for err := range results {
		if err == nil {
			successes++
		} else if errors.Is(err, ErrScheduleConflict) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatal(successes, conflicts)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	history, err := s.ScheduleHistory(ctx)
	if err != nil || len(history) != 4 || history[0].Revision != 4 || history[3].Revision != 1 {
		t.Fatal(history, err)
	}
	// The retained old pattern body and FK must remain available after reopening.
	patterns, err := s.PatternHistory(ctx, p.ID)
	if err != nil || len(patterns) != 3 || patterns[2].Design.Name != p.Design.Name {
		t.Fatal(patterns, err)
	}
}
func TestSchemaThreeUpgradePreservesPatternsAndSessions(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	p, err := s.CreatePattern(ctx, design(), "owner")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.SaveSession(ctx, Session{Hash: "session", Subject: "owner", Expires: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec("DROP TABLE schedule_references; DROP TABLE schedule_revisions; PRAGMA user_version=3"); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err = s.Pattern(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Session(ctx, "session", time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err = s.SaveSchedule(ctx, 1, calendar(p), "owner"); err != nil {
		t.Fatal(err)
	}
	paths, err := filepath.Glob(filepath.Join(dir, "before-schema-3-*.db"))
	if err != nil || len(paths) != 1 {
		t.Fatal(paths, err)
	}
}

func TestCalendarImportValidationRetainsReferencesWithoutWriting(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	p, err := s.CreatePattern(ctx, design(), "owner")
	if err != nil {
		t.Fatal(err)
	}
	plan := calendar(p)
	plan.Location = &schedule.Location{Latitude: 40, Longitude: -75}
	plan.Override = &schedule.Override{ID: "temporary", Action: plan.Rules[0].Action, StartsAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), ExpiresAt: time.Date(2026, 1, 1, 1, 0, 0, 0, time.UTC)}
	if err = s.ValidateSchedule(ctx, 1, plan); err != nil {
		t.Fatal(err)
	}
	history, err := s.ScheduleHistory(ctx)
	if err != nil || len(history) != 1 {
		t.Fatal("validation wrote history", err)
	}
	refs, err := s.PatternScheduleReferences(ctx, p.ID)
	if err != nil || len(refs) != 0 {
		t.Fatal("validation wrote references", err)
	}
	if _, err = s.SaveSchedule(ctx, 1, plan, "owner"); err != nil {
		t.Fatal(err)
	}
	if err = s.ValidateSchedule(ctx, 1, plan); !errors.Is(err, ErrScheduleConflict) {
		t.Fatal("stale base accepted", err)
	}
	if _, err = s.ArchivePattern(ctx, p.ID, 1, true, "owner"); err != nil {
		t.Fatal(err)
	}
	if err = s.ValidateSchedule(ctx, 2, plan); err != nil {
		t.Fatal("retained archived reference rejected", err)
	}
	duplicate := calendar(p)
	duplicate.Rules[0].ID = "new-rule"
	if err = s.ValidateSchedule(ctx, 2, duplicate); !errors.Is(err, ErrScheduleReference) {
		t.Fatal("new archived reference accepted", err)
	}
	missing := calendar(p)
	missing.Rules[0].Action.Revision = 999
	if err = s.ValidateSchedule(ctx, 2, missing); !errors.Is(err, ErrScheduleReference) {
		t.Fatal("missing pinned revision accepted", err)
	}
	if _, err = s.SaveSchedule(ctx, 2, schedule.Default(), "owner"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.SaveSchedule(ctx, 2, plan, "owner"); !errors.Is(err, ErrScheduleConflict) {
		t.Fatal("save after intervening change accepted", err)
	}
	if err = s.ValidateSchedule(ctx, 3, plan); !errors.Is(err, ErrScheduleReference) {
		t.Fatal("removed archived references accepted", err)
	}
	history, err = s.ScheduleHistory(ctx)
	if err != nil || len(history) != 3 || !history[1].Plan.Override.ExpiresAt.Equal(plan.Override.ExpiresAt) {
		t.Fatal("history or override expiry changed", err)
	}
}
