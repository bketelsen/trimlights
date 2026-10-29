package trimlight

import (
	"context"
	"errors"
	"testing"
)

func TestNativeControlReadbackAndNoAutomaticRetry(t *testing.T) {
	for _, name := range []string{"off", "play", "timer", "already-timer", "disconnect", "cancel", "unavailable", "foreign-change"} {
		t.Run(name, func(t *testing.T) {
			c, e, before := playbackClient(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			action, target := "play", byte(9)
			if name == "off" {
				action, target = "off", 0
			}
			if name == "timer" || name == "already-timer" {
				action, target = "timer", 0
				if name == "timer" {
					e.raw[3] = 1
					copy(e.raw[3320:3476], e.raw[52+8*182:52+8*182+156])
					before, _ = ParseSnapshot(e.raw, 3306)
				}
			}
			e.onControl = func(n int, raw []byte) bool {
				if name == "cancel" {
					cancel()
				}
				if name == "unavailable" {
					e.dialFailure = true
					return true
				}
				if name == "foreign-change" {
					raw[3476] ^= 1
				}
				return name == "disconnect"
			}
			recorded := false
			r, err := c.Control(ctx, ControlRequest{Action: action, PatternID: target, BaselineSHA256: before.StableSHA256}, func(s *Snapshot) error {
				e.mu.Lock()
				defer e.mu.Unlock()
				if len(e.controlWrites) != 0 || e.editCount != 0 {
					t.Fatal("write before audit")
				}
				recorded = true
				return nil
			})
			if !recorded || r == nil {
				t.Fatal(r, err)
			}
			if name == "unavailable" || name == "foreign-change" {
				if err == nil || r.Outcome != "uncertain" {
					t.Fatal(r, err)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				want := "verified"
				if name == "disconnect" || name == "cancel" {
					want = "reconciled"
				}
				if r.Outcome != want {
					t.Fatalf("outcome %s, want %s", r.Outcome, want)
				}
			}
			e.mu.Lock()
			defer e.mu.Unlock()
			wantWrites := 1
			if name == "already-timer" {
				wantWrites = 0
			}
			if len(e.controlWrites) != wantWrites || len(e.writes) != 0 {
				t.Fatal("repeated or unexpected write", e.controlWrites, e.writes)
			}
		})
	}
}

func TestNativeControlRejectsBeforeEditing(t *testing.T) {
	for _, name := range []string{"stale", "recorder", "cancel-before-send", "unknown-mode", "unsupported-layout"} {
		t.Run(name, func(t *testing.T) {
			c, e, before := playbackClient(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if name == "stale" {
				e.raw[80] ^= 1
			}
			if name == "unknown-mode" {
				e.raw[3] = 255
				before, _ = ParseSnapshot(e.raw, 3306)
			}
			if name == "unsupported-layout" {
				e.raw[25] = 17
				before, _ = ParseSnapshot(e.raw, 3306)
			}
			r, err := c.Control(ctx, ControlRequest{Action: "off", BaselineSHA256: before.StableSHA256}, func(*Snapshot) error {
				if name == "cancel-before-send" {
					cancel()
					return nil
				}
				return errors.New("disk full")
			})
			if err == nil || r.WriteAttempted {
				t.Fatal(r, err)
			}
			if name == "stale" && !errors.Is(err, ErrStaleBaseline) {
				t.Fatal(err)
			}
			e.mu.Lock()
			defer e.mu.Unlock()
			if len(e.controlWrites) != 0 || e.editCount != 0 {
				t.Fatal("invalid operation edited controller")
			}
		})
	}
}
