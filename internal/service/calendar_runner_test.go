package service

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"trimlights/internal/schedule"
	"trimlights/internal/storage"
	"trimlights/internal/trimlight"
)

type calendarClock struct{ nanos atomic.Int64 }

func (c *calendarClock) Now() time.Time  { return time.Unix(0, c.nanos.Load()).UTC() }
func (c *calendarClock) Set(t time.Time) { c.nanos.Store(t.UnixNano()) }

type calendarController struct {
	*nativeStub
	clock           *calendarClock
	offline         bool
	beforeControl   func()
	uncertain       bool
	waitForDeadline bool
	reads           int
	dataDir         string
}

func (c *calendarController) Snapshot(ctx context.Context, offset int) (*trimlight.Snapshot, error) {
	c.reads++
	if c.offline {
		return nil, errors.New("simulated offline controller")
	}
	snap, err := c.nativeStub.Snapshot(ctx, offset)
	if snap != nil {
		snap.QueriedAt = c.clock.Now()
	}
	return snap, err
}
func (c *calendarController) Control(ctx context.Context, in trimlight.ControlRequest, record func(*trimlight.Snapshot) error) (*trimlight.ControlResult, error) {
	if c.waitForDeadline {
		<-ctx.Done()
	}
	if c.beforeControl != nil {
		c.beforeControl()
	}
	result, err := c.nativeStub.Control(ctx, in, func(s *trimlight.Snapshot) error { s.QueriedAt = c.clock.Now(); return record(s) })
	if result != nil && result.Snapshot != nil {
		result.Snapshot.QueriedAt = c.clock.Now()
	}
	if err == nil && c.uncertain {
		result.Outcome = "uncertain"
		result.TransportError = "simulated unresolved readback"
		return result, errors.New(result.TransportError)
	}
	return result, err
}
func runnerFixture(t *testing.T) (*calendarRunner, *calendarController, *calendarClock, schedule.Plan) {
	t.Helper()
	clock := &calendarClock{}
	clock.Set(time.Date(2026, 10, 28, 20, 0, 0, 0, time.UTC))
	c := &calendarController{nativeStub: newNativeStub(t), clock: clock}
	c.raw[3] = 0
	c.dataDir = t.TempDir()
	s, w := setupIn(t, c, c.dataDir)
	w.offset = 3306
	snap, err := c.Snapshot(context.Background(), 3306)
	if err != nil {
		t.Fatal(err)
	}
	now := clock.Now()
	if err = s.Record(context.Background(), storage.Observation{Snapshot: snap, Source: "fixture"}, storage.Operation{ID: "baseline", Started: now, Finished: now}); err != nil {
		t.Fatal(err)
	}
	imported, err := s.ImportControllerPatterns(context.Background(), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	plan := schedule.Default()
	plan.Timezone = "UTC"
	plan.Rules = []schedule.Rule{{ID: "evening", Name: "Evening", Enabled: true, Kind: "weekly", Weekdays: []int{0, 1, 2, 3, 4, 5, 6}, Start: schedule.Boundary{Kind: "clock", Time: "18:00"}, End: schedule.Boundary{Kind: "clock", Time: "23:00"}, Action: schedule.Action{Kind: "pattern", PatternID: imported.Imported[0].ID, Revision: 1}}}
	if _, err = s.SaveSchedule(context.Background(), 1, plan, "fixture"); err != nil {
		t.Fatal(err)
	}
	if err = s.ClaimCalendar(context.Background(), 1, snap.StableSHA256, "fixture", now); err != nil {
		t.Fatal(err)
	}
	runner := newCalendarRunner(s, w)
	runner.now = clock.Now
	return runner, c, clock, plan
}
func step(t *testing.T, r *calendarRunner) time.Duration {
	t.Helper()
	delay, err := r.Step(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return delay
}
func runtimeState(t *testing.T, r *calendarRunner) storage.CalendarRuntime {
	t.Helper()
	s, e := r.store.CalendarRuntime(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	return s
}
func TestCalendarRunnerCurrentIntentNoDuplicatesAndRestart(t *testing.T) {
	r, c, clock, plan := runnerFixture(t)
	step(t, r)
	if c.writes != 1 || c.raw[3] != 1 || runtimeState(t, r).Status != "active" {
		t.Fatal(c.writes, runtimeState(t, r))
	}
	ops, _ := r.store.Operations(context.Background())
	found := false
	for _, op := range ops {
		if op.Control != nil && op.Control.Calendar != nil {
			found = true
			if op.Control.Calendar.Revision != 2 || op.Control.Calendar.Action != plan.Rules[0].Action || op.Control.Outcome != "verified" {
				t.Fatal(op)
			}
		}
	}
	if !found {
		t.Fatal("delivery provenance missing")
	}
	step(t, r)
	if c.writes != 1 {
		t.Fatal("duplicate write")
	}
	// A fresh runner reads persisted ownership and current intent, not old events.
	restarted := newCalendarRunner(r.store, r.worker)
	restarted.now = clock.Now
	clock.Set(clock.Now().Add(28 * time.Hour)) // midnight two dates later: OFF now
	step(t, restarted)
	if c.writes != 2 || c.raw[3] != 0 {
		t.Fatal("restart replayed missed transitions", c.writes, c.raw[3])
	}
	step(t, restarted)
	if c.writes != 2 {
		t.Fatal("duplicate OFF")
	}
}
func TestCalendarRunnerReconnectSkipsMissedTransitions(t *testing.T) {
	r, c, clock, _ := runnerFixture(t)
	clock.Set(clock.Now().Add(-3 * time.Hour))
	step(t, r) // 17:00, default off
	if c.writes != 0 {
		t.Fatal("unexpected pre-evening write")
	}
	c.offline = true
	clock.Set(clock.Now().Add(2 * time.Hour))
	for i := 0; i < 9; i++ {
		delay := step(t, r)
		if delay > 5*time.Minute || delay < 5*time.Second {
			t.Fatal(delay)
		}
	}
	if runtimeState(t, r).Status != "active" || c.writes != 0 {
		t.Fatal("offline delivery")
	}
	c.offline = false
	clock.Set(clock.Now().Add(5 * time.Hour)) // next midnight
	step(t, r)
	if c.writes != 0 {
		t.Fatal("replayed the missed evening")
	}
}
func TestCalendarRunnerOverrideExpiryResumesCalendar(t *testing.T) {
	r, c, clock, plan := runnerFixture(t)
	plan.Override = &schedule.Override{ID: "override", Action: schedule.Action{Kind: "off"}, StartsAt: clock.Now().Add(-time.Hour), ExpiresAt: clock.Now().Add(time.Hour)}
	if _, e := r.store.SaveSchedule(context.Background(), 2, plan, "owner"); e != nil {
		t.Fatal(e)
	}
	step(t, r)
	if c.writes != 0 {
		t.Fatal("ignored override")
	}
	clock.Set(clock.Now().Add(time.Hour))
	step(t, r)
	if c.writes != 1 || c.raw[3] != 1 {
		t.Fatal("did not resume at absolute expiry")
	}
}
func TestCalendarRunnerYieldsToExternalAndHouseholdControls(t *testing.T) {
	for _, external := range []bool{true, false} {
		t.Run(map[bool]string{true: "external", false: "household"}[external], func(t *testing.T) {
			r, c, _, _ := runnerFixture(t)
			step(t, r)
			if external {
				c.raw[52] ^= 1
				step(t, r)
			} else {
				obs, _ := r.store.Observation(context.Background())
				_, done, err := r.worker.EnqueueControl(context.Background(), "owner", trimlight.ControlRequest{Action: "timer", BaselineSHA256: obs.Snapshot.StableSHA256})
				if err != nil {
					t.Fatal(err)
				}
				if err = waitResult(t, done); err != nil {
					t.Fatal(err)
				}
			}
			if runtimeState(t, r).Status != "paused" {
				t.Fatal(runtimeState(t, r))
			}
			writes, reads := c.writes, c.reads
			step(t, r)
			if c.writes != writes || c.reads != reads {
				t.Fatal("paused runner still touched controller")
			}
		})
	}
}
func TestCalendarRunnerRejectsQueuedExpiryAndNewRevision(t *testing.T) {
	for _, expiry := range []bool{true, false} {
		t.Run(map[bool]string{true: "expired", false: "new-calendar"}[expiry], func(t *testing.T) {
			r, c, clock, plan := runnerFixture(t)
			c.beforeControl = func() {
				if expiry {
					clock.Set(clock.Now().Add(4 * time.Hour))
				} else {
					plan.Rules = nil
					if _, err := r.store.SaveSchedule(context.Background(), 2, plan, "other-phone"); err != nil {
						t.Error(err)
					}
				}
			}
			step(t, r)
			if c.writes != 0 || runtimeState(t, r).PendingID != "" {
				t.Fatal("stale request reached hardware")
			}
			c.beforeControl = nil
			step(t, r)
			if c.writes != 0 || runtimeState(t, r).Status != "active" {
				t.Fatal("did not select the new OFF intent", runtimeState(t, r))
			}
		})
	}
}
func TestCalendarRunnerAmbiguousDeliveryPausesWithoutRetry(t *testing.T) {
	r, c, _, _ := runnerFixture(t)
	c.uncertain = true
	step(t, r)
	if c.writes != 1 || runtimeState(t, r).Status != "paused" || runtimeState(t, r).PendingID != "" {
		t.Fatal(c.writes, runtimeState(t, r))
	}
	step(t, r)
	if c.writes != 1 {
		t.Fatal("retried ambiguous delivery")
	}
}
func TestCalendarRunnerPersistenceFailuresPreventReplay(t *testing.T) {
	for _, completion := range []bool{false, true} {
		t.Run(map[bool]string{false: "pending", true: "completion"}[completion], func(t *testing.T) {
			r, c, _, _ := runnerFixture(t)
			// Inject a SQLite failure in this isolated fixture, never household data.
			db, err := sql.Open("sqlite", filepath.Join(c.dataDir, "trimlights.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			condition := ""
			if completion {
				condition = " WHEN COALESCE(json_extract(NEW.body,'$.pending_operation'),'')=''"
			}
			if _, err = db.Exec("CREATE TRIGGER fail_calendar BEFORE UPDATE ON calendar_runtime" + condition + " BEGIN SELECT RAISE(ABORT,'simulated calendar storage failure'); END"); err != nil {
				t.Fatal(err)
			}
			_, err = r.Step(context.Background())
			if !completion && err == nil {
				t.Fatal("persistence failure was not returned")
			}
			expected := 0
			if completion {
				expected = 1
			}
			if c.writes != expected {
				t.Fatal(c.writes, expected)
			}
			if _, err = db.Exec("DROP TRIGGER fail_calendar"); err != nil {
				t.Fatal(err)
			}
			if err = r.store.RecoverControlIntents(context.Background()); err != nil {
				t.Fatal(err)
			}
			step(t, r)
			if c.writes != expected || runtimeState(t, r).Status != "paused" {
				t.Fatal("replayed uncertain intent", runtimeState(t, r))
			}
		})
	}
}

func TestCalendarRunnerFallBackIntervalAndConflict(t *testing.T) {
	r, c, clock, plan := runnerFixture(t)
	plan.Timezone = "America/New_York"
	plan.Rules[0].Weekdays = []int{0}
	plan.Rules[0].Start.Time = "01:30"
	plan.Rules[0].End.Time = "01:45"
	if _, err := r.store.SaveSchedule(context.Background(), 2, plan, "owner"); err != nil {
		t.Fatal(err)
	}
	clock.Set(time.Date(2026, 11, 1, 5, 35, 0, 0, time.UTC))
	step(t, r)
	if c.writes != 1 || c.raw[3] != 1 {
		t.Fatal("did not enter first repeated start")
	}
	clock.Set(time.Date(2026, 11, 1, 6, 0, 0, 0, time.UTC))
	step(t, r)
	if c.writes != 1 {
		t.Fatal("ended before second repeated end")
	}
	clock.Set(time.Date(2026, 11, 1, 6, 45, 0, 0, time.UTC))
	step(t, r)
	if c.writes != 2 || c.raw[3] != 0 {
		t.Fatal("did not end at second repeated end")
	}
	// Conflicting intent pauses rather than silently assigning ownership to a rule.
	duplicate := plan.Rules[0]
	duplicate.ID = "conflict"
	duplicate.Action = schedule.Action{Kind: "off"}
	plan.Rules = append(plan.Rules, duplicate)
	if _, err := r.store.SaveSchedule(context.Background(), 3, plan, "owner"); err != nil {
		t.Fatal(err)
	}
	clock.Set(time.Date(2026, 11, 8, 6, 35, 0, 0, time.UTC))
	step(t, r)
	if c.writes != 2 || runtimeState(t, r).Status != "paused" {
		t.Fatal("conflict was delivered")
	}
}
func TestCalendarRunnerCancellationAndPendingOwnership(t *testing.T) {
	r, c, _, _ := runnerFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := r.Step(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if c.writes != 0 {
		t.Fatal("canceled work sent a command")
	}
	if err := r.store.SuspendCalendar(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := r.Run(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestCalendarRunnerBoundsDeliveryByIntentLifetime(t *testing.T) {
	r, c, clock, plan := runnerFixture(t)
	plan.Override = &schedule.Override{ID: "short", Action: plan.Rules[0].Action, StartsAt: clock.Now(), ExpiresAt: clock.Now().Add(100 * time.Millisecond)}
	if _, err := r.store.SaveSchedule(context.Background(), 2, plan, "owner"); err != nil {
		t.Fatal(err)
	}
	c.waitForDeadline = true
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := r.Step(ctx); err != nil {
		t.Fatal(err)
	}
	// No pending intent was journaled, so no write was possible: the stale
	// intent is re-evaluated instead of pausing household ownership.
	if state := runtimeState(t, r); c.writes != 0 || state.Status != "active" || state.PendingID != "" {
		t.Fatal("expired transport sent a command or paused ownership", c.writes, state)
	}
	c.waitForDeadline = false
	clock.Set(plan.Override.ExpiresAt)
	step(t, r)
	if c.writes != 1 || c.raw[3] != 1 || runtimeState(t, r).Status != "active" {
		t.Fatal("did not re-evaluate the current intent", c.writes, runtimeState(t, r))
	}
}

func TestCalendarRunnerGuardSaveRaceDoesNotPause(t *testing.T) {
	r, c, _, _ := runnerFixture(t)
	calls := 0
	r.intent = func(ctx context.Context, s *storage.Store, now time.Time, snap *trimlight.Snapshot) (storage.SavedSchedule, schedule.Segment, trimlight.ControlRequest, error) {
		calls++
		if calls == 2 {
			// The pre-write guard sees a save between its schedule and input reads.
			return storage.SavedSchedule{}, schedule.Segment{}, trimlight.ControlRequest{}, storage.ErrScheduleConflict
		}
		return calendarIntent(ctx, s, now, snap)
	}
	step(t, r)
	if state := runtimeState(t, r); calls != 2 || c.writes != 0 || state.Status != "active" || state.PendingID != "" {
		t.Fatal("save race paused ownership or reached hardware", calls, c.writes, state)
	}
	step(t, r)
	if c.writes != 1 || runtimeState(t, r).Status != "active" {
		t.Fatal("did not deliver on the next pass", c.writes, runtimeState(t, r))
	}
}

func TestProductionStartupCannotActivatePersistedCalendar(t *testing.T) {
	r, c, _, _ := runnerFixture(t)
	// Use a copy of the fixture DB so the normal fixture worker remains isolated.
	// A consistent backup is not needed here: no operations are running, and this
	// SQLite VACUUM captures just the synthetic DB for a separate service instance.
	other := t.TempDir()
	db, err := sql.Open("sqlite", filepath.Join(c.dataDir, "trimlights.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("VACUUM INTO ?", filepath.Join(other, "trimlights.db")); err != nil {
		t.Fatal(err)
	}
	db.Close()
	state := runtimeState(t, r)
	if state.Status != "active" {
		t.Fatal(state)
	}
	var accesses atomic.Int32
	controller := controllerFunc(func(context.Context, int) (*trimlight.Snapshot, error) {
		accesses.Add(1)
		return nil, errors.New("unexpected hardware access")
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, Config{Listen: "127.0.0.1:0", DataDir: other, APIToken: "01234567890123456789012345678901", Controller: controller, ScheduleOffset: 3306})
	}()
	db, err = sql.Open("sqlite", "file:"+filepath.Join(other, "trimlights.db")+"?mode=ro&_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	deadline := time.After(2 * time.Second)
	for {
		var status string
		if err = db.QueryRow("SELECT json_extract(body,'$.status') FROM calendar_runtime WHERE id=1").Scan(&status); err != nil {
			t.Fatal(err)
		}
		if status == "paused" {
			break
		}
		select {
		case e := <-done:
			t.Fatal("service exited before applying gate", e)
		case <-deadline:
			t.Fatal("startup gate was not applied")
		case <-time.After(5 * time.Millisecond):
		}
	}
	cancel()
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	if accesses.Load() != 0 {
		t.Fatal("production startup reached hardware")
	}
}
