package storage

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"trimlights/internal/schedule"
	"trimlights/internal/trimlight"
)

func runtimeFixture(t *testing.T) (*Store, string, Observation) {
	t.Helper()
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	raw, err := os.ReadFile("../trimlight/testdata/snapshot.bin")
	if err != nil {
		t.Fatal(err)
	}
	raw[3] = 0
	snap, err := trimlight.ParseSnapshot(raw, 3306)
	if err != nil {
		t.Fatal(err)
	}
	snap.QueriedAt = time.Now().UTC()
	o := Observation{Snapshot: snap, Source: "fixture"}
	ctx := context.Background()
	if err = s.Record(ctx, o, Operation{ID: "initial", Started: snap.QueriedAt, Finished: snap.QueriedAt}); err != nil {
		t.Fatal(err)
	}
	if err = s.ClaimCalendar(ctx, 1, snap.StableSHA256, "fixture", snap.QueriedAt); err != nil {
		t.Fatal(err)
	}
	return s, dir, o
}
func pendingCalendar(o Observation) Operation {
	now := o.Snapshot.QueriedAt
	return Operation{ID: "pending", Actor: "calendar", Started: now, Finished: now, Control: &ControlAudit{Request: trimlight.ControlRequest{Action: "off", BaselineSHA256: o.Snapshot.StableSHA256}, Outcome: "pending", Calendar: &CalendarDelivery{Version: 2, Revision: 1, Action: schedule.Action{Kind: "off"}, From: now, Until: now.Add(time.Hour)}}}
}
func TestRuntimeMigrationAndInterruptedDelivery(t *testing.T) {
	s, dir, o := runtimeFixture(t)
	ctx := context.Background()
	// Reconstruct released schema 6 and preserve its data through migration.
	if _, err := s.db.Exec("DROP TABLE calendar_runtime; PRAGMA user_version=6"); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	backups, _ := filepath.Glob(filepath.Join(dir, "before-schema-6-*.db"))
	if len(backups) != 1 {
		t.Fatal(backups)
	}
	state, err := s.CalendarRuntime(ctx)
	if err != nil || state.Status != "disabled" || state.Version != 1 {
		t.Fatal(state, err)
	}
	if err = s.ClaimCalendar(ctx, 1, o.Snapshot.StableSHA256, "fixture", o.Snapshot.QueriedAt); err != nil {
		t.Fatal(err)
	}
	op := pendingCalendar(o)
	if err = s.Record(ctx, o, op); err != nil {
		t.Fatal(err)
	}
	state, _ = s.CalendarRuntime(ctx)
	if state.PendingID != op.ID || state.Version != 3 {
		t.Fatal(state)
	}
	s.Close()
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = s.RecoverControlIntents(ctx); err != nil {
		t.Fatal(err)
	}
	state, err = s.CalendarRuntime(ctx)
	if err != nil || state.Status != "paused" || state.PendingID != "" || state.LastOperation != op.ID {
		t.Fatal(state, err)
	}
	ops, err := s.Operations(ctx)
	if err != nil || ops[0].Control.Outcome != "uncertain" || ops[0].Control.Calendar.Revision != 1 {
		t.Fatal(ops, err)
	}
	if err = s.CompleteControl(ctx, o, Operation{ID: op.ID, Actor: op.Actor, Control: &ControlAudit{Outcome: "verified", Calendar: op.Control.Calendar}}); err == nil {
		t.Fatal("recovered operation completed again")
	}
}
func TestRuntimePendingAndManualPauseAreAtomic(t *testing.T) {
	s, _, o := runtimeFixture(t)
	ctx := context.Background()
	op := pendingCalendar(o)
	if _, err := s.SaveSchedule(ctx, 1, schedule.Default(), "other"); err != nil {
		t.Fatal(err)
	}
	if err := s.Record(ctx, o, op); !errors.Is(err, ErrCalendarOwnership) {
		t.Fatal(err)
	}
	state, _ := s.CalendarRuntime(ctx)
	ops, _ := s.Operations(ctx)
	if state.Version != 2 || state.PendingID != "" || len(ops) != 1 {
		t.Fatal("stale revision partially committed", state, ops)
	}
	op.Control.Calendar.Revision = 2
	if _, err := s.db.Exec("CREATE TRIGGER fail_operation BEFORE INSERT ON operations BEGIN SELECT RAISE(ABORT,'fixture failure'); END"); err != nil {
		t.Fatal(err)
	}
	if err := s.Record(ctx, o, op); err == nil {
		t.Fatal("failed pending audit accepted")
	}
	state, _ = s.CalendarRuntime(ctx)
	if state.Version != 2 || state.PendingID != "" {
		t.Fatal("ownership committed without audit", state)
	}
	op.Control.Calendar = nil
	if err := s.Record(ctx, o, op); err == nil {
		t.Fatal("manual pending audit failure accepted")
	}
	state, _ = s.CalendarRuntime(ctx)
	if state.Status != "active" {
		t.Fatal("manual pause committed without audit")
	}
	if _, err := s.db.Exec("DROP TRIGGER fail_operation"); err != nil {
		t.Fatal(err)
	}
	if err := s.Record(ctx, o, op); err != nil {
		t.Fatal(err)
	}
	state, _ = s.CalendarRuntime(ctx)
	if state.Status != "paused" {
		t.Fatal("manual action failed to pause owner")
	}
}
func TestRuntimeCleanRestartGateAndStaleClaim(t *testing.T) {
	s, _, o := runtimeFixture(t)
	ctx := context.Background()
	if err := s.ClaimCalendar(ctx, 1, o.Snapshot.StableSHA256, "other", o.Snapshot.QueriedAt); !errors.Is(err, ErrCalendarOwnership) {
		t.Fatal(err)
	}
	if err := s.SuspendCalendar(ctx); err != nil {
		t.Fatal(err)
	}
	state, _ := s.CalendarRuntime(ctx)
	if state.Status != "paused" {
		t.Fatal(state)
	}
	if err := s.ClaimCalendar(ctx, state.Version, o.Snapshot.StableSHA256, "other", o.Snapshot.QueriedAt.Add(6*time.Minute)); err == nil {
		t.Fatal("stale claim accepted")
	}
}

func TestRuntimePendingDeliveryActionMatchesControl(t *testing.T) {
	s, _, o := runtimeFixture(t)
	ctx := context.Background()
	for _, mismatch := range []func(*Operation){
		func(op *Operation) { op.Control.Request.Action, op.Control.Request.PatternID = "play", 1 },
		func(op *Operation) { op.Control.Request.Action = "timer" },
		func(op *Operation) {
			op.Control.Calendar.Action = schedule.Action{Kind: "pattern", PatternID: "fixture", Revision: 1}
		},
	} {
		op := pendingCalendar(o)
		mismatch(&op)
		if err := s.Record(ctx, o, op); err == nil {
			t.Fatal("mismatched delivery accepted", op.Control.Request, op.Control.Calendar.Action)
		}
		state, _ := s.CalendarRuntime(ctx)
		if state.Version != 2 || state.PendingID != "" {
			t.Fatal("mismatched delivery committed", state)
		}
	}
	if err := s.Record(ctx, o, pendingCalendar(o)); err != nil {
		t.Fatal(err)
	}
}
