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
func (c *Client) VerifyPlayback(ctx context.Context, fingerprint string, target byte, record func(string, *Snapshot) error) (*PlaybackReport, error) {
	return c.verifyPlayback(ctx, fingerprint, target, record, 10*time.Second)
}

func (s *session) playbackWrite(ctx context.Context, payload []byte) error {
	if err := s.enterEdit(ctx); err != nil {
		return err
	}
	return s.exchange(ctx, 4, payload, nil)
}

func (c *Client) verifyPlayback(ctx context.Context, fingerprint string, target byte, record func(string, *Snapshot) error, hold time.Duration) (*PlaybackReport, error) {
	if !validFingerprint(fingerprint) || record == nil || target < 1 || target > 18 {
		return nil, errors.New("playback verification requires a reviewed fingerprint, recorder and target slot 1–18")
	}
	r := &PlaybackReport{TargetPatternID: target}
	var evidenceErr error
	recordStage := func(stage string, state *Snapshot) error {
		err := record(stage, state)
		if err != nil {
			evidenceErr = errors.Join(evidenceErr, fmt.Errorf("record %s: %w", stage, err))
		}
		return err
	}
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
		if err = recordStage("before", r.Before); err != nil {
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
			return recordStage("restored", state)
		}
		defer func() {
			if !r.WriteAttempted {
				return
			}
			clean, cancel := context.WithTimeout(context.Background(), 40*time.Second)
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
			independent, stop := context.WithTimeout(context.Background(), 10*time.Second)
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
					return recordStage("independent", r.Independent)
				})
			}
			if !r.IndependentVerified {
				independentErr = errors.Join(errors.New("INDEPENDENT RESTORATION NOT VERIFIED"), independentErr)
			}
			operationErr = errors.Join(operationErr, independentErr)
		}()

		// Mark before edit/send so even a partially sent or canceled exchange
		// triggers reconciliation. An unchanged baseline needs no cleanup write.
		r.WriteAttempted = true
		if err = s.playbackWrite(ctx, off); err != nil {
			return err
		}
		r.Off, err = s.snapshot(ctx, 3306)
		if err != nil {
			return err
		}
		if err = verifyPlaybackState(r.Before, r.Off, 0, originalParams); err != nil {
			return err
		}
		r.OffVerified = true
		if err = recordStage("off", r.Off); err != nil {
			return err
		}
		if err = wait(ctx, hold); err != nil {
			return err
		}
		if err = s.playbackWrite(ctx, play); err != nil {
			return err
		}
		r.Played, err = s.snapshot(ctx, 3306)
		if err != nil {
			return err
		}
		if err = verifyPlaybackState(r.Before, r.Played, 1, targetParams); err != nil {
			return err
		}
		r.PlayedVerified = true
		if err = recordStage("played", r.Played); err != nil {
			return err
		}
		return wait(ctx, hold)
	})
	return r, errors.Join(err, evidenceErr)
}
