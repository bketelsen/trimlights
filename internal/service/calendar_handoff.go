package service

import (
	"context"
	"errors"
	"slices"
	"time"

	"trimlights/internal/storage"
	"trimlights/internal/trimlight"
)

// These internal values are deliberately not accepted by HTTP or CLI. A future
// activation UI must bind a server-created review to its authenticated actor.
type handoffReview struct {
	transfer  storage.CalendarHandoff
	command   trimlight.ControlRequest
	schedules []trimlight.Schedule
}

type calendarHandoff struct {
	store  *storage.Store
	worker *Worker
	now    func() time.Time
}

func newCalendarHandoff(s *storage.Store, w *Worker) *calendarHandoff {
	return &calendarHandoff{store: s, worker: w, now: time.Now}
}

// review has no network or mutation side effects. Any inconsistency between these
// reads is rejected again before delivery and atomically at the pending commit.
func (h *calendarHandoff) review(ctx context.Context, direction string) (handoffReview, error) {
	var review handoffReview
	if direction != "calendar" && direction != "timer" {
		return review, errors.New("unknown ownership transfer direction")
	}
	state, err := h.store.CalendarRuntime(ctx)
	if err != nil {
		return review, err
	}
	if state.PendingID != "" || (direction == "calendar" && state.Status == "active") {
		return review, storage.ErrCalendarOwnership
	}
	obs, err := h.store.Observation(ctx)
	if err != nil {
		return review, err
	}
	if available, reason := h.worker.ControlAvailability(obs); !available {
		return review, errors.New(reason)
	}
	snap, err := trimlight.ValidatedNativeSnapshot(obs.Snapshot)
	if err != nil {
		return review, err
	}
	now := h.now().UTC()
	if snap.QueriedAt.IsZero() || snap.QueriedAt.After(now) || now.Sub(snap.QueriedAt) > 5*time.Minute {
		return review, errors.New("refresh the controller before reviewing ownership")
	}
	review.transfer = storage.CalendarHandoff{Direction: direction, Version: state.Version, From: now, Until: now.Add(time.Minute)}
	review.command = trimlight.ControlRequest{Action: "timer", BaselineSHA256: snap.StableSHA256}
	review.schedules = append([]trimlight.Schedule(nil), snap.Schedules...)
	if direction == "calendar" {
		saved, intent, command, err := calendarIntent(ctx, h.store, now, snap)
		if err != nil {
			return handoffReview{}, err
		}
		review.command = command
		review.transfer.Revision, review.transfer.Action = saved.Revision, intent.Action
		if intent.Until.Before(review.transfer.Until) {
			review.transfer.Until = intent.Until
		}
	}
	return review, nil
}

func (h *calendarHandoff) guard(ctx context.Context, review handoffReview, snap *trimlight.Snapshot) error {
	now := h.now().UTC()
	t := review.transfer
	if t.From.IsZero() || now.Before(t.From) || !now.Before(t.Until) || t.Until.Sub(t.From) > time.Minute {
		return errors.New("ownership review expired; review again")
	}
	state, err := h.store.CalendarRuntime(ctx)
	if err != nil {
		return err
	}
	if state.Version != t.Version || state.PendingID != "" || (t.Direction == "calendar" && state.Status == "active") {
		return storage.ErrCalendarOwnership
	}
	// The review binds the native schedules it showed, in addition to the fingerprint.
	if snap == nil || snap.StableSHA256 != review.command.BaselineSHA256 || !slices.Equal(snap.Schedules, review.schedules) {
		return trimlight.ErrStaleBaseline
	}
	if t.Direction == "calendar" {
		saved, intent, command, err := calendarIntent(ctx, h.store, now, snap)
		if err != nil {
			return err
		}
		if saved.Revision != t.Revision || intent.Action != t.Action || command != review.command {
			return storage.ErrCalendarOwnership
		}
	} else if t.Direction != "timer" || review.command.Action != "timer" {
		return errors.New("invalid ownership review")
	}
	return nil
}

// apply performs only the reviewed action. Completion owns the durable state
// transition; returning early on caller cancellation never claims success.
func (h *calendarHandoff) apply(ctx context.Context, actor string, review handoffReview) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	obs, err := h.store.Observation(ctx)
	if err != nil {
		return err
	}
	if available, reason := h.worker.ControlAvailability(obs); !available {
		return errors.New(reason)
	}
	snap, err := trimlight.ValidatedNativeSnapshot(obs.Snapshot)
	if err != nil {
		return err
	}
	if err = h.guard(ctx, review, snap); err != nil {
		return err
	}
	remaining := review.transfer.Until.Sub(h.now())
	if remaining <= 0 {
		return errors.New("ownership review expired; review again")
	}
	deliveryCtx, cancel := context.WithTimeout(ctx, remaining)
	defer cancel()
	guard := func(ctx context.Context, before *trimlight.Snapshot) error { return h.guard(ctx, review, before) }
	id, done, err := h.worker.enqueue(deliveryCtx, actor, &review.command, nil, &review.transfer, guard)
	if err != nil {
		return err
	}
	if err = awaitCalendar(ctx, done); err != nil {
		return err
	}
	state, err := h.store.CalendarRuntime(ctx)
	if err != nil {
		return err
	}
	wanted := "active"
	if review.transfer.Direction == "timer" {
		wanted = "disabled"
	}
	if state.Status != wanted || state.Version != review.transfer.Version+2 || state.LastOperation != id || state.PendingID != "" {
		return storage.ErrCalendarOwnership
	}
	return nil
}
