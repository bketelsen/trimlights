package trimlight

import (
	"context"
	"errors"
	"time"
)

var ErrStaleBaseline = errors.New("controller state changed; refresh and review before trying again")
var ErrUnsupportedControl = errors.New("native controls require the verified controller layout and known mode")

type ControlRequest struct {
	Action         string `json:"action"`
	PatternID      byte   `json:"pattern_id,omitempty"`
	BaselineSHA256 string `json:"baseline_sha256"`
}

// SupportsNativeControl also handles snapshots reloaded from JSON storage, whose
// private raw field is absent. Never trust derived JSON fields as wire evidence.
func SupportsNativeControl(s *Snapshot) bool {
	_, err := ValidatedNativeSnapshot(s)
	return err == nil
}

type ControlResult struct {
	Snapshot       *Snapshot `json:"snapshot,omitempty"`
	Outcome        string    `json:"outcome"`
	WriteAttempted bool      `json:"write_attempted"`
	TransportError string    `json:"transport_error,omitempty"`
}

// Control applies one lasting user intent, never a diagnostic restore sequence.
// beforeWrite must durably record pending intent and the fresh baseline. The gate
// stays held during the sole read-only reconciliation after an ambiguous send.
func (c *Client) Control(ctx context.Context, input ControlRequest, beforeWrite func(*Snapshot) error) (*ControlResult, error) {
	if err := input.Validate(); err != nil {
		return nil, err
	}
	if beforeWrite == nil {
		return nil, errors.New("a pending-intent recorder is required")
	}
	r := &ControlResult{Outcome: "rejected"}
	err := c.withSession(ctx, func(s *session) error {
		before, err := s.snapshot(ctx, 3306)
		if err != nil {
			return err
		}
		r.Snapshot = before
		if !nativeControlLayout(before) {
			return ErrUnsupportedControl
		}
		if before.StableSHA256 != input.BaselineSHA256 {
			return ErrStaleBaseline
		}
		payload, err := nativeControlPayload(input)
		if err != nil {
			return err
		}
		if err = beforeWrite(before); err != nil {
			return err
		}
		if nativeControlMatches(before, before, input) {
			r.Outcome = "verified"
			return nil
		}
		if err = s.enterEdit(ctx); err != nil {
			return err
		}
		r.WriteAttempted = true // The send can be partial even when it returns an error.
		r.Outcome = "uncertain"
		err = s.exchange(ctx, 4, payload, nil)
		if err == nil {
			var after *Snapshot
			after, err = s.snapshot(ctx, 3306)
			if after != nil {
				r.Snapshot = after
			}
			if err == nil && nativeControlMatches(before, after, input) {
				r.Outcome = "verified"
				return nil
			}
			if err == nil {
				err = errors.New("control readback did not match the requested state and unchanged library/schedules")
			}
		}
		r.TransportError = err.Error()
		_ = s.conn.Close()
		clean, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		readErr := wait(clean, c.pause)
		if readErr == nil {
			readErr = c.openSession(clean, func(fresh *session) error {
				state, e := fresh.snapshot(clean, 3306)
				if e != nil {
					return e
				}
				r.Snapshot = state
				if !nativeControlMatches(before, state, input) {
					return errors.New("independent read does not confirm requested state; refresh before another command")
				}
				r.Outcome = "reconciled"
				return nil
			})
		}
		if r.Outcome == "reconciled" {
			return nil
		}
		return errors.Join(errors.New("control outcome uncertain; no write was retried"), err, readErr)
	})
	return r, err
}
