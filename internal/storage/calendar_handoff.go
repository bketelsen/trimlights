package storage

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"trimlights/internal/schedule"
	"trimlights/internal/trimlight"
)

// CalendarHandoff records a reviewed ownership transfer, not a recurring delivery.
// The service keeps this path dormant until protocol/handoff verification.
type CalendarHandoff struct {
	Direction string          `json:"direction"`
	Version   int             `json:"ownership_version"`
	Revision  int             `json:"calendar_revision,omitempty"`
	Action    schedule.Action `json:"action"`
	From      time.Time       `json:"from"`
	Until     time.Time       `json:"until"`
}

func calendarHandoffControl(ctx context.Context, tx *sql.Tx, o Observation, op Operation, complete bool) error {
	h := op.Control.Handoff
	if op.Control.Calendar != nil || (h.Direction != "calendar" && h.Direction != "timer") || h.Version < 1 || !h.Until.After(h.From) || h.Until.Sub(h.From) > time.Minute {
		return errors.New("invalid calendar handoff")
	}
	if err := op.Control.Request.Validate(); err != nil {
		return err
	}
	if h.Direction == "calendar" {
		if err := h.Action.Validate(); err != nil {
			return err
		}
		if h.Revision < 1 || (h.Action.Kind == "off" && op.Control.Request.Action != "off") || (h.Action.Kind == "pattern" && op.Control.Request.Action != "play") {
			return errors.New("handoff action does not match its control")
		}
	} else if op.Control.Request.Action != "timer" {
		return errors.New("native handoff requires TIMER")
	}
	state, err := runtimeIn(ctx, tx)
	if err != nil {
		return err
	}
	if !complete && op.Control.Outcome == "pending" {
		if state.Version != h.Version || state.PendingID != "" || (h.Direction == "calendar" && state.Status == "active") {
			return ErrCalendarOwnership
		}
		if h.Direction == "calendar" {
			var revision int
			if err = tx.QueryRowContext(ctx, "SELECT MAX(revision) FROM schedule_revisions").Scan(&revision); err != nil {
				return err
			}
			if revision != h.Revision {
				return ErrCalendarOwnership
			}
		}
		snap, err := trimlight.ValidatedNativeSnapshot(o.Snapshot)
		if err != nil {
			return err
		}
		if snap.StableSHA256 != op.Control.Request.BaselineSHA256 {
			return trimlight.ErrStaleBaseline
		}
		state.Status, state.Actor = "paused", op.Actor
		state.PendingID, state.LastOperation = op.ID, op.ID
		state.Reason = "An ownership transfer is pending verification."
		return writeRuntime(ctx, tx, state)
	}
	if complete {
		if state.PendingID != op.ID {
			return ErrCalendarOwnership
		}
		state.PendingID, state.LastOperation = "", op.ID
		if op.Error == "" && (op.Control.Outcome == "verified" || op.Control.Outcome == "reconciled") {
			snap, err := trimlight.ValidatedNativeSnapshot(o.Snapshot)
			if err != nil {
				return err
			}
			state.OwnedSHA256 = snap.StableSHA256
			// A pause during delivery changes the version and must win even if the
			// controller completed successfully. Never reactivate that ownership.
			if state.Version == h.Version+1 && state.Status == "paused" {
				state.Status, state.Reason = "active", ""
				if h.Direction == "timer" {
					// Native TIMER owns the lights: a disabled runtime keeps no owner.
					state.Status, state.Actor, state.OwnedSHA256 = "disabled", "", ""
				}
			}
		} else {
			state.Status, state.Reason = "paused", "Ownership transfer was not verified: "+op.Error
		}
		return writeRuntime(ctx, tx, state)
	}
	return nil
}
