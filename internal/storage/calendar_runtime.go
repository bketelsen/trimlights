package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"trimlights/internal/schedule"
	"trimlights/internal/trimlight"
)

var ErrCalendarOwnership = errors.New("calendar ownership changed; delivery stopped")

type CalendarRuntime struct {
	Version       int    `json:"version"`
	Status        string `json:"status"`
	Actor         string `json:"actor,omitempty"`
	OwnedSHA256   string `json:"owned_sha256,omitempty"`
	PendingID     string `json:"pending_operation,omitempty"`
	LastOperation string `json:"last_operation,omitempty"`
	Reason        string `json:"reason,omitempty"`
}
type CalendarDelivery struct {
	Version  int             `json:"ownership_version"`
	Revision int             `json:"calendar_revision"`
	Action   schedule.Action `json:"action"`
	From     time.Time       `json:"from"`
	Until    time.Time       `json:"until"`
}

func scanRuntime(row scanner) (CalendarRuntime, error) {
	var raw []byte
	var state CalendarRuntime
	if err := row.Scan(&raw); err != nil {
		return state, err
	}
	err := json.Unmarshal(raw, &state)
	if err == nil && (state.Version < 1 || (state.Status != "disabled" && state.Status != "active" && state.Status != "paused")) {
		err = errors.New("invalid calendar ownership record")
	}
	return state, err
}
func (s *Store) CalendarRuntime(ctx context.Context) (CalendarRuntime, error) {
	return scanRuntime(s.db.QueryRowContext(ctx, "SELECT body FROM calendar_runtime WHERE id=1"))
}
func runtimeIn(ctx context.Context, tx *sql.Tx) (CalendarRuntime, error) {
	return scanRuntime(tx.QueryRowContext(ctx, "SELECT body FROM calendar_runtime WHERE id=1"))
}
func writeRuntime(ctx context.Context, tx *sql.Tx, state CalendarRuntime) error {
	state.Version++
	raw, err := json.Marshal(state)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, "UPDATE calendar_runtime SET body=? WHERE id=1", raw)
	return err
}

// ClaimCalendar only records ownership of an already reviewed MANUAL/OFF state.
// No production endpoint calls it; native TIMER handoff still needs verification.
func (s *Store) ClaimCalendar(ctx context.Context, version int, fingerprint, actor string, now time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	state, err := runtimeIn(ctx, tx)
	if err != nil {
		return err
	}
	if state.Version != version || state.PendingID != "" {
		return ErrCalendarOwnership
	}
	var raw []byte
	if err = tx.QueryRowContext(ctx, "SELECT body FROM observation WHERE id=1").Scan(&raw); err != nil {
		return err
	}
	var observation Observation
	if err = json.Unmarshal(raw, &observation); err != nil {
		return err
	}
	snap, err := trimlight.ValidatedNativeSnapshot(observation.Snapshot)
	if err != nil || observation.LastError != "" || snap.QueriedAt.IsZero() || snap.QueriedAt.After(now) || now.Sub(snap.QueriedAt) > 5*time.Minute || snap.StableSHA256 != fingerprint || snap.Playback.Mode == "timer" {
		return errors.New("calendar ownership requires a fresh reviewed MANUAL/OFF snapshot")
	}
	state.Status, state.Actor, state.OwnedSHA256, state.Reason = "active", actor, fingerprint, ""
	if err = writeRuntime(ctx, tx, state); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Store) PauseCalendar(ctx context.Context, version int, reason string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	state, err := runtimeIn(ctx, tx)
	if err != nil {
		return err
	}
	if state.Version != version {
		return ErrCalendarOwnership
	}
	state.Status, state.Reason = "paused", reason
	if err = writeRuntime(ctx, tx, state); err != nil {
		return err
	}
	return tx.Commit()
}

// SuspendCalendar is the hard production gate, independent of host configuration.
func (s *Store) SuspendCalendar(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	state, err := runtimeIn(ctx, tx)
	if err != nil {
		return err
	}
	if state.Status == "active" {
		state.Status, state.Reason = "paused", "Calendar activation is unavailable pending protocol and handoff verification."
		if err = writeRuntime(ctx, tx, state); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Called inside the same transaction as pending/final audit and observation.
func calendarControl(ctx context.Context, tx *sql.Tx, o Observation, op Operation, complete bool) error {
	if op.Control == nil {
		return nil
	}
	if op.Control.Handoff != nil {
		return calendarHandoffControl(ctx, tx, o, op, complete)
	}
	state, err := runtimeIn(ctx, tx)
	if err != nil {
		return err
	}
	d := op.Control.Calendar
	if !complete && op.Control.Outcome == "pending" {
		if d == nil {
			if state.Status == "active" {
				state.Status, state.Reason = "paused", "A household control took ownership of the lights."
				return writeRuntime(ctx, tx, state)
			}
			return nil
		}
		var revision int
		if err = tx.QueryRowContext(ctx, "SELECT MAX(revision) FROM schedule_revisions").Scan(&revision); err != nil {
			return err
		}
		if state.Status != "active" || state.Version != d.Version || state.PendingID != "" || state.OwnedSHA256 != op.Control.Request.BaselineSHA256 || revision != d.Revision {
			return ErrCalendarOwnership
		}
		if err = d.Action.Validate(); err != nil {
			return err
		}
		if !d.Until.After(d.From) {
			return errors.New("invalid calendar delivery interval")
		}
		state.PendingID = op.ID
		state.LastOperation = op.ID
		state.Reason = ""
		return writeRuntime(ctx, tx, state)
	}
	if complete && d != nil {
		if state.PendingID != op.ID {
			return ErrCalendarOwnership
		}
		state.PendingID = ""
		state.LastOperation = op.ID
		if op.Error == "" && (op.Control.Outcome == "verified" || op.Control.Outcome == "reconciled") {
			snap, err := trimlight.ValidatedNativeSnapshot(o.Snapshot)
			if err != nil {
				return err
			}
			state.OwnedSHA256 = snap.StableSHA256
		} else {
			state.Status, state.Reason = "paused", "Calendar delivery was not verified: "+op.Error
		}
		return writeRuntime(ctx, tx, state)
	}
	return nil
}
