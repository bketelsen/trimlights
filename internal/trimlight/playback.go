package trimlight

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"
)

type PlaybackReport struct {
	Before              *Snapshot `json:"before,omitempty"`
	Off                 *Snapshot `json:"off,omitempty"`
	Played              *Snapshot `json:"played,omitempty"`
	Restored            *Snapshot `json:"restored,omitempty"`
	Independent         *Snapshot `json:"independent,omitempty"`
	OriginalPatternID   byte      `json:"original_pattern_id"`
	TargetPatternID     byte      `json:"target_pattern_id"`
	WriteAttempted      bool      `json:"write_attempted"`
	OffVerified         bool      `json:"off_verified"`
	PlayedVerified      bool      `json:"played_verified"`
	RestoredVerified    bool      `json:"restored_verified"`
	IndependentVerified bool      `json:"independent_reconnect_verified"`
}

// VerifyPlayback performs only the owner-authorized bounded experiment. record
// must persist evidence and must not call this Client (the gate is held throughout).
// Cancellation interrupts observation holds but not bounded restoration cleanup.
// Evidence recording after the first write is bounded: see playbackTiming.
func (c *Client) VerifyPlayback(ctx context.Context, fingerprint string, target byte, record func(string, *Snapshot) error) (*PlaybackReport, error) {
	return c.verifyPlayback(ctx, fingerprint, target, record, defaultPlaybackTiming(10*time.Second))
}

// OFF and saved playback each send edit + write, then read back.
const playbackOperationExchanges = 6

// playbackTiming holds the budgets of one verification. Cleanup and independent
// budgets bound restoration (all attempts, reconnect and pacing) and the final
// read, including their evidence records. Margin covers the "off" and "played"
// evidence records and scheduling delays in the active phase; guard (part of the
// margin) is reserved for the hand-over from the active phase to cleanup.
type playbackTiming struct {
	hold, cleanup, independent, margin, guard time.Duration
}

func defaultPlaybackTiming(hold time.Duration) playbackTiming {
	return playbackTiming{hold: hold, cleanup: 40 * time.Second, independent: 10 * time.Second,
		margin: time.Minute, guard: 5 * time.Second}
}

// worstCase bounds the time from the first write until cleanup and independent
// verification finish: every paced exchange at its I/O timeout, both holds, the
// margin, and the cleanup and independent budgets.
func (t playbackTiming) worstCase(pause, timeout time.Duration) time.Duration {
	return playbackOperationExchanges*(pause+timeout) + 2*t.hold + t.margin + t.cleanup + t.independent
}

// activeBudget is the time from the first write until cleanup must start, so
// that cleanup and the independent read still end inside worstCase.
func (t playbackTiming) activeBudget(pause, timeout time.Duration) time.Duration {
	return t.worstCase(pause, timeout) - t.cleanup - t.independent - t.guard
}

// playbackWindowReachesSunday reports whether any instant from start to
// start+worst falls on Sunday in start's location. Every snapshot sends a clock
// query, and the Sunday clock encoding is unverified, so cleanup cannot read or
// restore state on Sunday. A day is at least 23 hours, so a window shorter than
// 12 hours spans at most two local dates; longer windows are refused outright.
func playbackWindowReachesSunday(start time.Time, worst time.Duration) bool {
	end := start.Add(worst).In(start.Location())
	return worst >= 12*time.Hour || start.Weekday() == time.Sunday || end.Weekday() == time.Sunday
}

func (s *session) playbackWrite(ctx context.Context, payload []byte) error {
	if err := s.enterEdit(ctx); err != nil {
		return err
	}
	return s.exchange(ctx, 4, payload, nil)
}

func (c *Client) verifyPlayback(ctx context.Context, fingerprint string, target byte, record func(string, *Snapshot) error, timing playbackTiming) (*PlaybackReport, error) {
	if !validFingerprint(fingerprint) || record == nil || target < 1 || target > 18 {
		return nil, errors.New("playback verification requires a reviewed fingerprint, recorder and target slot 1–18")
	}
	r := &PlaybackReport{TargetPatternID: target}
	var evidenceErr error
	// pending is an overrun recorder call that may still run. Calls stay
	// serialized: no call starts while an earlier one still runs.
	var pending chan error
	// recordStage returns by deadline (zero: no bound) even if record blocks,
	// so evidence recording cannot delay cleanup past the Sunday-safe window.
	recordStage := func(deadline time.Time, stage string, state *Snapshot) error {
		var err error
		if pending != nil {
			select {
			case <-pending:
				pending = nil
			default:
				err = errors.New("skipped: an earlier evidence recorder call is still running")
			}
		}
		if err == nil {
			done := make(chan error, 1)
			go func() { done <- record(stage, state) }()
			var expired <-chan time.Time
			if !deadline.IsZero() {
				timer := time.NewTimer(time.Until(deadline))
				defer timer.Stop()
				expired = timer.C
			}
			select {
			case err = <-done:
			case <-expired:
				select {
				case err = <-done:
				default:
					pending = done
					err = errors.New("evidence recorder did not return before its deadline")
				}
			}
		}
		if err != nil {
			evidenceErr = errors.Join(evidenceErr, fmt.Errorf("record %s: %w", stage, err))
		}
		return err
	}
	deadlineOf := func(ctx context.Context) time.Time { d, _ := ctx.Deadline(); return d }
	err := c.withSession(ctx, func(s *session) (operationErr error) {
		var err error
		r.Before, err = s.snapshot(ctx, 3306)
		if err != nil {
			return err
		}
		r.OriginalPatternID, err = validatePlaybackBaseline(r.Before, fingerprint, target)
		if err != nil {
			return err
		}
		if err = recordStage(time.Time{}, "before", r.Before); err != nil {
			return err
		}
		originalParams := r.Before.raw[3320:3476]
		targetStart := 52 + (int(target)-1)*182
		targetParams := r.Before.raw[targetStart : targetStart+156]
		off, _ := playbackModePayload(0)
		timer, _ := playbackModePayload(2)
		play, _ := savedPlaybackPayload(target)
		replay, _ := savedPlaybackPayload(r.OriginalPatternID)

		restore := func(clean context.Context, connection *session) error {
			// Reconcile before sending any restoration writes, including on reconnect.
			state, err := connection.snapshot(clean, 3306)
			if err != nil {
				return err
			}
			if !playbackInvariants(r.Before, state) {
				return errors.New("restoration refused: non-playback controller fields changed")
			}
			if !bytes.Equal(stable(state), stable(r.Before)) {
				if err = connection.playbackWrite(clean, replay); err != nil {
					return err
				}
				state, err = connection.snapshot(clean, 3306)
				if err != nil {
					return err
				}
				if err = verifyPlaybackState(r.Before, state, 1, originalParams); err != nil {
					return err
				}
				if err = connection.playbackWrite(clean, timer); err != nil {
					return err
				}
				state, err = connection.snapshot(clean, 3306)
				if err != nil {
					return err
				}
			}
			r.Restored = state
			if !bytes.Equal(stable(state), stable(r.Before)) {
				return errors.New("restoration differs from original non-clock baseline")
			}
			r.RestoredVerified = true
			return recordStage(deadlineOf(clean), "restored", state)
		}
		defer func() {
			if !r.WriteAttempted {
				return
			}
			clean, cancel := context.WithTimeout(context.Background(), timing.cleanup)
			defer cancel()
			restoreErr := restore(clean, s)
			_ = s.conn.Close()
			if !r.RestoredVerified {
				// Wait across connections too. Do not blindly resend: restore first
				// queries current state, and skips writes if it is already restored.
				if err := wait(clean, c.pause); err != nil {
					restoreErr = errors.Join(restoreErr, err)
				} else {
					retryErr := c.openSession(clean, func(fresh *session) error { return restore(clean, fresh) })
					restoreErr = errors.Join(restoreErr, retryErr)
				}
			}
			if !r.RestoredVerified {
				operationErr = errors.Join(operationErr, errors.New("RESTORATION NOT VERIFIED"), restoreErr)
				return
			}
			// Preserve any failed recovery attempt even if a later retry succeeds.
			operationErr = errors.Join(operationErr, restoreErr)
			independent, stop := context.WithTimeout(context.Background(), timing.independent)
			defer stop()
			independentErr := wait(independent, c.pause)
			if independentErr == nil {
				independentErr = c.openSession(independent, func(fresh *session) error {
					var err error
					r.Independent, err = fresh.snapshot(independent, 3306)
					if err != nil {
						return err
					}
					if !bytes.Equal(stable(r.Independent), stable(r.Before)) {
						return errors.New("independent read differs from original baseline")
					}
					r.IndependentVerified = true
					return recordStage(deadlineOf(independent), "independent", r.Independent)
				})
			}
			if !r.IndependentVerified {
				independentErr = errors.Join(errors.New("INDEPENDENT RESTORATION NOT VERIFIED"), independentErr)
			}
			operationErr = errors.Join(operationErr, independentErr)
		}()

		// Refuse before any write if cleanup could still run on Sunday, when the
		// clock query (and therefore every restoration readback) is refused.
		start := c.now().In(c.location)
		worst := timing.worstCase(c.pause, c.timeout)
		if playbackWindowReachesSunday(start, worst) {
			return fmt.Errorf("playback verification refused: cleanup could run until %s, which reaches Sunday in %s; Sunday clock encoding is unverified, so no write was sent",
				start.Add(worst).Format(time.RFC3339), c.location)
		}
		// Enforce the window: writes, readbacks, holds and evidence records of
		// the active phase end by this absolute deadline, so cleanup starts in
		// time to finish (with the independent read) before start+worst.
		activeDeadline := time.Now().Add(timing.activeBudget(c.pause, c.timeout))
		active, stopActive := context.WithDeadline(ctx, activeDeadline)
		defer stopActive()
		defer func() {
			if errors.Is(operationErr, context.DeadlineExceeded) && ctx.Err() == nil {
				operationErr = fmt.Errorf("active phase reached its Sunday-safe deadline; no further control write sent: %w", operationErr)
			}
		}()
		// Mark before edit/send so even a partially sent or canceled exchange
		// triggers reconciliation. An unchanged baseline needs no cleanup write.
		r.WriteAttempted = true
		if err = s.playbackWrite(active, off); err != nil {
			return err
		}
		r.Off, err = s.snapshot(active, 3306)
		if err != nil {
			return err
		}
		if err = verifyPlaybackState(r.Before, r.Off, 0, originalParams); err != nil {
			return err
		}
		r.OffVerified = true
		if err = recordStage(activeDeadline, "off", r.Off); err != nil {
			return err
		}
		if err = wait(active, timing.hold); err != nil {
			return err
		}
		if err = s.playbackWrite(active, play); err != nil {
			return err
		}
		r.Played, err = s.snapshot(active, 3306)
		if err != nil {
			return err
		}
		if err = verifyPlaybackState(r.Before, r.Played, 1, targetParams); err != nil {
			return err
		}
		r.PlayedVerified = true
		if err = recordStage(activeDeadline, "played", r.Played); err != nil {
			return err
		}
		return wait(active, timing.hold)
	})
	return r, errors.Join(err, evidenceErr)
}
