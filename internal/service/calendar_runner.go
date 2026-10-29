package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"trimlights/internal/schedule"
	"trimlights/internal/storage"
	"trimlights/internal/trimlight"
)

// calendarRunner is deliberately not started by service.Run. Activation and
// native timer handoff require protocol evidence; there is no enabling flag/API.
// Tests exercise this engine through the real worker, with synthetic hardware.
type calendarRunner struct {
	store    *storage.Store
	worker   *Worker
	now      func() time.Time
	mu       sync.Mutex
	failures int
}

func newCalendarRunner(s *storage.Store, w *Worker) *calendarRunner {
	return &calendarRunner{store: s, worker: w, now: time.Now}
}
func (r *calendarRunner) Run(ctx context.Context) error {
	for {
		delay, err := r.Step(ctx)
		if err != nil {
			return err
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}
func awaitCalendar(ctx context.Context, done <-chan error) error {
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (r *calendarRunner) pause(ctx context.Context, state storage.CalendarRuntime, reason string) (time.Duration, error) {
	err := r.store.PauseCalendar(ctx, state.Version, reason)
	if errors.Is(err, storage.ErrCalendarOwnership) {
		return time.Second, nil
	}
	return 30 * time.Second, err
}
func (r *calendarRunner) backoff() time.Duration {
	r.failures++
	return time.Duration(1<<min(r.failures-1, 6)) * 5 * time.Second
}

func calendarIntent(ctx context.Context, s *storage.Store, now time.Time, snap *trimlight.Snapshot) (storage.SavedSchedule, schedule.Segment, trimlight.ControlRequest, error) {
	saved, err := s.Schedule(ctx)
	if err != nil {
		return saved, schedule.Segment{}, trimlight.ControlRequest{}, err
	}
	input, err := s.CalendarInputs(ctx, saved.Revision, saved.Plan)
	if err != nil {
		return saved, schedule.Segment{}, trimlight.ControlRequest{}, err
	}
	// Only the caller's fresh canonical snapshot is suitable for physical resolution.
	input.Observation.Snapshot = snap
	preview, err := schedule.Evaluate(saved.Plan, now, now.Add(48*time.Hour))
	if err != nil {
		return saved, schedule.Segment{}, trimlight.ControlRequest{}, err
	}
	intent := preview.Segments[0]
	if intent.Conflict {
		return saved, intent, trimlight.ControlRequest{}, errors.New("current calendar rules conflict")
	}
	for _, issue := range preview.Issues {
		if strings.HasPrefix(issue.Message, "Skipped:") {
			return saved, intent, trimlight.ControlRequest{}, errors.New("upcoming calendar occurrences are skipped; review the timeline")
		}
	}
	check := func(a schedule.Action) error {
		if a.Kind == "off" {
			return nil
		}
		p, ok := input.Pinned[a]
		if !ok || !nativeDesignMatches(p.Design, snap) {
			return errors.New("an enabled calendar reference no longer matches its original native slot")
		}
		return nil
	}
	for _, rule := range saved.Plan.Rules {
		if rule.Enabled {
			if err = check(rule.Action); err != nil {
				return saved, intent, trimlight.ControlRequest{}, err
			}
		}
	}
	if o := saved.Plan.Override; o != nil && o.ExpiresAt.After(now) {
		if err = check(o.Action); err != nil {
			return saved, intent, trimlight.ControlRequest{}, err
		}
	}
	command := trimlight.ControlRequest{Action: "off", BaselineSHA256: snap.StableSHA256}
	if intent.Action.Kind == "pattern" {
		command.Action = "play"
		command.PatternID = input.Pinned[intent.Action].Design.Native.ID
	}
	return saved, intent, command, nil
}
func calendarAlreadyApplied(snap *trimlight.Snapshot, command trimlight.ControlRequest) bool {
	if command.Action == "off" {
		return snap.Playback.Mode == "off"
	}
	if snap.Playback.Mode != "manual" {
		return false
	}
	for _, p := range snap.Patterns {
		if p.ID == command.PatternID {
			return sameParameters(p.ParametersHex, snap.Playback.ParametersHex)
		}
	}
	return false
}

// Step processes only current intent. Caller-controlled time permits deterministic
// restart, DST, expiry and outage tests without sleeps or household services.
func (r *calendarRunner) Step(ctx context.Context) (time.Duration, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	state, err := r.store.CalendarRuntime(ctx)
	if err != nil {
		return 0, err
	}
	if state.Status != "active" {
		return 30 * time.Second, nil
	}
	if state.PendingID != "" {
		return r.pause(ctx, state, "A previous Calendar delivery is still pending; review its outcome.")
	}
	if r.worker.Status().Error != "" {
		return r.pause(ctx, state, "Controller audit persistence failed; repair storage before resuming.")
	}
	_, done, err := r.worker.Enqueue(ctx, "calendar")
	if err == nil {
		err = awaitCalendar(ctx, done)
	}
	if err != nil {
		if ctx.Err() != nil {
			return 0, ctx.Err()
		}
		return min(r.backoff(), 5*time.Minute), nil
	}
	r.failures = 0
	observed, err := r.store.Observation(ctx)
	if err != nil {
		return 0, err
	}
	snap, err := trimlight.ValidatedNativeSnapshot(observed.Snapshot)
	if err != nil {
		return r.pause(ctx, state, "Unsupported controller state; Calendar stopped.")
	}
	// Re-read ownership after the queued refresh: a household action may have won.
	latest, err := r.store.CalendarRuntime(ctx)
	if err != nil {
		return 0, err
	}
	if latest.Version != state.Version || latest.Status != "active" {
		return time.Second, nil
	}
	if snap.StableSHA256 != state.OwnedSHA256 || snap.Playback.Mode == "timer" {
		return r.pause(ctx, state, "Controller state changed outside Calendar; review before resuming.")
	}
	saved, intent, command, err := calendarIntent(ctx, r.store, r.now().UTC(), snap)
	if errors.Is(err, storage.ErrScheduleConflict) {
		return time.Second, nil
	}
	if err != nil {
		return r.pause(ctx, state, err.Error())
	}
	delay := max(time.Second, min(30*time.Second, intent.Until.Sub(r.now())))
	if calendarAlreadyApplied(snap, command) {
		return delay, nil
	}
	delivery := &storage.CalendarDelivery{Version: state.Version, Revision: saved.Revision, Action: intent.Action, From: intent.From, Until: intent.Until}
	guard := func(ctx context.Context, before *trimlight.Snapshot) error {
		now := r.now().UTC()
		if now.Before(delivery.From) || !now.Before(delivery.Until) {
			return storage.ErrCalendarOwnership
		}
		fresh, current, wanted, err := calendarIntent(ctx, r.store, now, before)
		if err != nil {
			return err
		}
		if fresh.Revision != delivery.Revision || current.Action != delivery.Action || wanted != command {
			return storage.ErrCalendarOwnership
		}
		return nil
	}
	// Bound pacing and transport by this intent's lifetime as well as the
	// worker deadline. A queue/edit-session delay must not send an expired scene.
	remaining := delivery.Until.Sub(r.now())
	if remaining <= 0 {
		return time.Second, nil
	}
	deliveryCtx, cancelDelivery := context.WithTimeout(ctx, remaining)
	defer cancelDelivery()
	_, done, err = r.worker.enqueue(deliveryCtx, "calendar", &command, delivery, nil, guard)
	if err == nil {
		err = awaitCalendar(ctx, done)
	}
	if ctx.Err() != nil {
		return 0, ctx.Err()
	}
	if errors.Is(err, storage.ErrCalendarOwnership) || errors.Is(err, ErrQueueFull) {
		return time.Second, nil
	}
	if err != nil {
		latest, e := r.store.CalendarRuntime(ctx)
		if e != nil {
			return 0, e
		}
		if latest.Status == "active" {
			return r.pause(ctx, latest, fmt.Sprintf("Calendar command stopped: %v", err))
		}
	}
	return delay, nil
}
