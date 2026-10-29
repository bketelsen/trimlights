package service

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"trimlights/internal/schedule"
	"trimlights/internal/storage"
	"trimlights/internal/trimlight"
)

func handoffFixture(t *testing.T) (*calendarHandoff, *calendarRunner, *calendarController, *calendarClock, schedule.Plan) {
	t.Helper()
	r, c, clock, plan := runnerFixture(t)
	ctx := context.Background()
	if err := r.store.PauseCalendar(ctx, runtimeState(t, r).Version, "fixture native ownership"); err != nil {
		t.Fatal(err)
	}
	c.raw[3] = 2
	snap, err := c.Snapshot(ctx, 3306)
	if err != nil {
		t.Fatal(err)
	}
	if err = r.store.Record(ctx, storage.Observation{Snapshot: snap}, storage.Operation{ID: "native-state", Started: clock.Now(), Finished: clock.Now()}); err != nil {
		t.Fatal(err)
	}
	h := newCalendarHandoff(r.store, r.worker)
	h.now = clock.Now
	return h, r, c, clock, plan
}

func reviewed(t *testing.T, h *calendarHandoff, direction string) handoffReview {
	t.Helper()
	review, err := h.review(context.Background(), direction)
	if err != nil {
		t.Fatal(err)
	}
	return review
}

func TestCalendarHandoffReviewTakeoverAndReturn(t *testing.T) {
	h, r, c, _, plan := handoffFixture(t)
	ctx := context.Background()
	native := bytes.Clone(c.raw[26:3320]) // entire saved library and native timers
	state := runtimeState(t, r)
	reads, writes := c.reads, c.writes
	ops, _ := r.store.Operations(ctx)
	review := reviewed(t, h, "calendar")
	after, _ := r.store.Operations(ctx)
	if runtimeState(t, r) != state || c.reads != reads || c.writes != writes || !reflect.DeepEqual(ops, after) {
		t.Fatal("review mutated state or accessed hardware")
	}
	if review.transfer.Action != plan.Rules[0].Action || review.transfer.Revision != 2 || review.command.Action != "play" || len(review.schedules) != 2 {
		t.Fatal(review)
	}
	if err := h.apply(ctx, "owner", review); err != nil {
		t.Fatal(err)
	}
	if c.writes != 1 || c.raw[3] != 1 || runtimeState(t, r).Status != "active" || runtimeState(t, r).Actor != "owner" {
		t.Fatal(c.writes, runtimeState(t, r))
	}
	step(t, r)
	if c.writes != 1 {
		t.Fatal("runner repeated the takeover command")
	}
	if _, err := h.review(ctx, "calendar"); !errors.Is(err, storage.ErrCalendarOwnership) {
		t.Fatal("active takeover accepted", err)
	}
	if err := h.apply(ctx, "other-phone", review); !errors.Is(err, storage.ErrCalendarOwnership) {
		t.Fatal("review replay accepted", err)
	}
	if err := h.apply(ctx, "owner", reviewed(t, h, "timer")); err != nil {
		t.Fatal(err)
	}
	state = runtimeState(t, r)
	if c.writes != 2 || c.raw[3] != 2 || state.Status != "disabled" || state.PendingID != "" || !bytes.Equal(native, c.raw[26:3320]) {
		t.Fatal("native return failed or changed saved timers/library", state)
	}
	reads = c.reads
	step(t, r)
	if c.reads != reads || c.writes != 2 {
		t.Fatal("disabled runner accessed hardware")
	}
	ops, _ = r.store.Operations(ctx)
	directions := map[string]bool{}
	for _, op := range ops {
		if op.Control != nil && op.Control.Handoff != nil {
			if op.Control.Outcome != "verified" || op.Actor != "owner" || op.Control.Calendar != nil {
				t.Fatal(op)
			}
			directions[op.Control.Handoff.Direction] = true
		}
	}
	if len(directions) != 2 {
		t.Fatal("missing handoff audit", directions)
	}
}

func TestCalendarHandoffRejectsChangesWhileQueued(t *testing.T) {
	for _, change := range []string{"calendar", "ownership", "expiry", "clock-backward", "controller"} {
		t.Run(change, func(t *testing.T) {
			h, r, c, clock, plan := handoffFixture(t)
			ctx := context.Background()
			review := reviewed(t, h, "calendar")
			c.beforeControl = func() {
				switch change {
				case "calendar":
					plan.Rules = nil
					if _, err := r.store.SaveSchedule(ctx, 2, plan, "other-phone"); err != nil {
						t.Error(err)
					}
				case "ownership":
					if err := r.store.PauseCalendar(ctx, review.transfer.Version, "other-phone pause"); err != nil {
						t.Error(err)
					}
				case "expiry":
					clock.Set(clock.Now().Add(time.Minute))
				case "clock-backward":
					clock.Set(clock.Now().Add(-time.Second))
				case "controller":
					c.raw[3307] = 2 // external native timer edit
				}
			}
			if err := h.apply(ctx, "owner", review); err == nil {
				t.Fatal("stale review accepted")
			}
			if c.writes != 0 || runtimeState(t, r).Status == "active" || runtimeState(t, r).PendingID != "" {
				t.Fatal("stale transfer reached hardware", runtimeState(t, r))
			}
		})
	}
}

func TestCalendarHandoffOffIntentAndBoundaryExpiry(t *testing.T) {
	h, r, c, clock, plan := handoffFixture(t)
	ctx := context.Background()
	plan.Override = &schedule.Override{ID: "short", Action: schedule.Action{Kind: "off"}, StartsAt: clock.Now(), ExpiresAt: clock.Now().Add(20 * time.Second)}
	if _, err := r.store.SaveSchedule(ctx, 2, plan, "owner"); err != nil {
		t.Fatal(err)
	}
	review := reviewed(t, h, "calendar")
	if review.command.Action != "off" || !review.transfer.Until.Equal(plan.Override.ExpiresAt) {
		t.Fatal("review extended across an intent boundary", review)
	}
	if err := h.apply(ctx, "owner", review); err != nil {
		t.Fatal(err)
	}
	if c.raw[3] != 0 || c.writes != 1 || runtimeState(t, r).Status != "active" {
		t.Fatal("OFF takeover failed")
	}
	clock.Set(plan.Override.ExpiresAt)
	step(t, r)
	if c.writes != 2 || c.raw[3] != 1 {
		t.Fatal("takeover did not hand control to the runner")
	}
}

func TestCalendarHandoffReviewRejectsStaleAndUnsupportedPlans(t *testing.T) {
	h, r, c, clock, plan := handoffFixture(t)
	ctx := context.Background()
	clock.Set(clock.Now().Add(6 * time.Minute))
	if _, err := h.review(ctx, "calendar"); err == nil {
		t.Fatal("stale snapshot accepted")
	}
	clock.Set(clock.Now().Add(-6 * time.Minute))
	duplicate := plan.Rules[0]
	duplicate.ID, duplicate.Action = "conflict", schedule.Action{Kind: "off"}
	plan.Rules = append(plan.Rules, duplicate)
	if _, err := r.store.SaveSchedule(ctx, 2, plan, "owner"); err != nil {
		t.Fatal(err)
	}
	if _, err := h.review(ctx, "calendar"); err == nil {
		t.Fatal("conflicting plan accepted")
	}
	// Getting out of Calendar must remain possible with an invalid calendar.
	if err := h.apply(ctx, "owner", reviewed(t, h, "timer")); err != nil {
		t.Fatal(err)
	}
	if runtimeState(t, r).Status != "disabled" || c.raw[3] != 2 {
		t.Fatal("invalid plan prevented native return")
	}
}

func TestCalendarHandoffUncertaintyAndPersistenceFailures(t *testing.T) {
	for _, failure := range []string{"uncertain", "pending", "completion"} {
		t.Run(failure, func(t *testing.T) {
			h, r, c, _, _ := handoffFixture(t)
			ctx := context.Background()
			review := reviewed(t, h, "calendar")
			db, err := sql.Open("sqlite", filepath.Join(c.dataDir, "trimlights.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			if failure == "uncertain" {
				c.uncertain = true
			} else {
				condition := ""
				if failure == "completion" {
					condition = " WHEN COALESCE(json_extract(NEW.body,'$.pending_operation'),'')=''"
				}
				if _, err = db.Exec("CREATE TRIGGER fail_handoff BEFORE UPDATE ON calendar_runtime" + condition + " BEGIN SELECT RAISE(ABORT,'simulated storage failure'); END"); err != nil {
					t.Fatal(err)
				}
			}
			if err = h.apply(ctx, "owner", review); err == nil {
				t.Fatal("failed handoff reported success")
			}
			wantWrites := 1
			if failure == "pending" {
				wantWrites = 0
			}
			if c.writes != wantWrites || runtimeState(t, r).Status == "active" {
				t.Fatal(c.writes, runtimeState(t, r))
			}
			if failure != "uncertain" {
				if _, err = db.Exec("DROP TRIGGER fail_handoff"); err != nil {
					t.Fatal(err)
				}
			}
			if err = r.store.RecoverControlIntents(ctx); err != nil {
				t.Fatal(err)
			}
			step(t, r)
			if c.writes != wantWrites || runtimeState(t, r).Status != "paused" || runtimeState(t, r).PendingID != "" {
				t.Fatal("failed handoff was replayed", runtimeState(t, r))
			}
		})
	}
}

func TestCalendarHandoffCancellationAndTransportExpiry(t *testing.T) {
	h, r, c, clock, plan := handoffFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	review := reviewed(t, h, "calendar")
	cancel()
	if err := h.apply(ctx, "owner", review); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	ctx = context.Background()
	plan.Override = &schedule.Override{ID: "short", Action: plan.Rules[0].Action, StartsAt: clock.Now(), ExpiresAt: clock.Now().Add(50 * time.Millisecond)}
	if _, err := r.store.SaveSchedule(ctx, 2, plan, "owner"); err != nil {
		t.Fatal(err)
	}
	c.waitForDeadline = true
	if err := h.apply(ctx, "owner", reviewed(t, h, "calendar")); err == nil {
		t.Fatal("expired transport reported success")
	}
	if c.writes != 0 || runtimeState(t, r).Status == "active" {
		t.Fatal("canceled transfer reached hardware")
	}
}

// Pausing after pending commit but before readback must survive successful control.
type pausingHandoffController struct {
	*calendarController
	pause func() error
}

func (c *pausingHandoffController) Control(ctx context.Context, input trimlight.ControlRequest, record func(*trimlight.Snapshot) error) (*trimlight.ControlResult, error) {
	return c.calendarController.Control(ctx, input, func(s *trimlight.Snapshot) error {
		if err := record(s); err != nil {
			return err
		}
		return c.pause()
	})
}

func TestCalendarHandoffConcurrentPauseWinsCompletion(t *testing.T) {
	h, r, c, _, _ := handoffFixture(t)
	ctx := context.Background()
	r.worker.controller = &pausingHandoffController{calendarController: c, pause: func() error {
		state, err := r.store.CalendarRuntime(ctx)
		if err != nil {
			return err
		}
		return r.store.PauseCalendar(ctx, state.Version, "owner paused during transfer")
	}}
	if err := h.apply(ctx, "owner", reviewed(t, h, "calendar")); !errors.Is(err, storage.ErrCalendarOwnership) {
		t.Fatal("concurrent pause reported as activation", err)
	}
	state := runtimeState(t, r)
	if c.writes != 1 || state.Status != "paused" || state.PendingID != "" || state.Reason != "owner paused during transfer" {
		t.Fatal(c.writes, state)
	}
}
