package storage

import (
	"context"
	"errors"
	"testing"
	"time"

	"trimlights/internal/schedule"
)

func TestHandoffPendingChecksCalendarAndOwnershipAtomically(t *testing.T) {
	s, _, o := runtimeFixture(t)
	ctx := context.Background()
	if err := s.PauseCalendar(ctx, 2, "review takeover"); err != nil {
		t.Fatal(err)
	}
	op := pendingCalendar(o)
	op.Control.Handoff = &CalendarHandoff{Direction: "calendar", Version: 3, Revision: 1, Action: schedule.Action{Kind: "off"}, From: o.Snapshot.QueriedAt, Until: o.Snapshot.QueriedAt.Add(time.Minute)}
	op.Control.Calendar = nil
	// A save after service validation but before pending commit must still reject.
	if _, err := s.SaveSchedule(ctx, 1, schedule.Default(), "other-phone"); err != nil {
		t.Fatal(err)
	}
	if err := s.Record(ctx, o, op); !errors.Is(err, ErrCalendarOwnership) {
		t.Fatal(err)
	}
	state, _ := s.CalendarRuntime(ctx)
	ops, _ := s.Operations(ctx)
	if state.Version != 3 || state.PendingID != "" || len(ops) != 1 {
		t.Fatal("rejected transfer partially committed", state, ops)
	}
	op.Control.Handoff.Revision = 2
	if err := s.Record(ctx, o, op); err != nil {
		t.Fatal(err)
	}
	state, _ = s.CalendarRuntime(ctx)
	if state.Status != "paused" || state.PendingID != op.ID || state.Version != 4 {
		t.Fatal(state)
	}
	// A competing transfer with the original review cannot join a pending handoff.
	other := op
	other.ID = "competing"
	if err := s.Record(ctx, o, other); !errors.Is(err, ErrCalendarOwnership) {
		t.Fatal(err)
	}
	if err := s.PauseCalendar(ctx, 4, "explicit pause"); err != nil {
		t.Fatal(err)
	}
	op.Control.Outcome = "verified"
	if err := s.CompleteControl(ctx, o, op); err != nil {
		t.Fatal(err)
	}
	state, _ = s.CalendarRuntime(ctx)
	if state.Status != "paused" || state.Reason != "explicit pause" || state.PendingID != "" || state.OwnedSHA256 != o.Snapshot.StableSHA256 {
		t.Fatal("completion overwrote pause", state)
	}
}

func TestHandoffInterruptedTransferRetainsProvenance(t *testing.T) {
	s, dir, o := runtimeFixture(t)
	ctx := context.Background()
	op := pendingCalendar(o)
	op.Control.Request.Action = "timer"
	op.Control.Handoff = &CalendarHandoff{Direction: "timer", Version: 2, From: o.Snapshot.QueriedAt, Until: o.Snapshot.QueriedAt.Add(time.Minute)}
	op.Control.Calendar = nil
	if err := s.Record(ctx, o, op); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = s.RecoverControlIntents(ctx); err != nil {
		t.Fatal(err)
	}
	state, err := s.CalendarRuntime(ctx)
	if err != nil || state.Status != "paused" || state.PendingID != "" || state.LastOperation != op.ID {
		t.Fatal(state, err)
	}
	ops, err := s.Operations(ctx)
	if err != nil || ops[0].Control.Outcome != "uncertain" || ops[0].Control.Handoff == nil || *ops[0].Control.Handoff != *op.Control.Handoff {
		t.Fatal("interrupted handoff provenance lost", ops, err)
	}
	op.Control.Outcome = "verified"
	if err = s.CompleteControl(ctx, o, op); err == nil {
		t.Fatal("late completion revived recovered ownership")
	}
}
