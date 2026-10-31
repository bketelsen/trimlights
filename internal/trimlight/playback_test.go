package trimlight

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func playbackClient(t *testing.T) (*Client, *emulator, *Snapshot) {
	t.Helper()
	c, e := testClient(t)
	e.raw[3] = 2
	e.timerPatternID = 7
	copy(e.raw[3320:3476], e.raw[52+6*182:52+6*182+156])
	s, err := ParseSnapshot(e.raw, 3306)
	if err != nil {
		t.Fatal(err)
	}
	return c, e, s
}

func TestPlaybackVerificationAndRecovery(t *testing.T) {
	for _, name := range []string{
		"success", "off-disconnect", "play-disconnect", "cancel-off", "cancel-play",
		"cancel-hold", "off-record-error", "played-record-error", "restored-record-error",
		"independent-record-error", "ignored-play", "timer-disconnect",
	} {
		t.Run(name, func(t *testing.T) {
			c, e, before := playbackClient(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			e.onControl = func(n int, raw []byte) bool {
				if name == "cancel-off" && n == 1 || name == "cancel-play" && n == 2 {
					cancel()
				}
				if name == "ignored-play" && n == 2 {
					raw[3] = 0
					copy(raw[3320:3476], before.raw[3320:3476])
				}
				return name == "off-disconnect" && n == 1 || name == "play-disconnect" && n == 2 || name == "timer-disconnect" && n == 4
			}
			stages := map[string]bool{}
			r, err := c.verifyPlayback(ctx, before.StableSHA256, 9, func(stage string, state *Snapshot) error {
				if stages[stage] {
					t.Fatalf("stage recorded twice: %s", stage)
				}
				stages[stage] = true
				if stage == "off" && name == "cancel-hold" {
					cancel()
				}
				if name == stage+"-record-error" {
					return errors.New("disk full")
				}
				return nil
			}, time.Millisecond)
			if name == "success" && err != nil || name != "success" && err == nil {
				t.Fatalf("incorrect error: %v", err)
			}
			if r == nil || !r.WriteAttempted || !r.RestoredVerified || !r.IndependentVerified {
				t.Fatalf("restoration not verified: %+v %v", r, err)
			}
			if !stages["before"] || !stages["restored"] || !stages["independent"] {
				t.Fatal(stages)
			}
			e.mu.Lock()
			defer e.mu.Unlock()
			if !bytes.Equal(e.raw, before.raw) || len(e.writes) != 0 {
				t.Fatal("baseline not restored or schedule write sent")
			}
			if name == "success" || name == "timer-disconnect" {
				want := [][]byte{{2, 0, 1, 0}, {4, 0, 1, 9}, {4, 0, 1, 7}, {2, 0, 1, 2}}
				if !reflect.DeepEqual(e.controlWrites, want) || e.editCount != 4 {
					t.Fatalf("wrong exchanges or replay after uncertain TIMER: %v edits=%d", e.controlWrites, e.editCount)
				}
			}
			if name == "success" && (!r.OffVerified || !r.PlayedVerified || e.connections != 2) {
				t.Fatalf("success incomplete: %+v connections=%d", r, e.connections)
			}
		})
	}
}

func TestPlaybackRefusesUnreviewedOrUnrestorableBaseline(t *testing.T) {
	for _, name := range []string{"fingerprint", "mode", "unknown-mode", "unmatched", "ambiguous", "slot-order", "same-target", "baseline-record-error"} {
		t.Run(name, func(t *testing.T) {
			c, e, baseline := playbackClient(t)
			target := byte(9)
			switch name {
			case "fingerprint":
				e.raw[100] ^= 1
			case "mode":
				e.raw[3] = 0
			case "unknown-mode":
				e.raw[3] = 255
			case "unmatched":
				clear(e.raw[3320:3476])
			case "ambiguous":
				copy(e.raw[52:208], e.raw[3320:3476])
			case "slot-order":
				e.raw[26], e.raw[26+182] = e.raw[26+182], e.raw[26]
			case "same-target":
				target = 7
			}
			fingerprint := baseline.StableSHA256
			if name != "fingerprint" {
				changed, err := ParseSnapshot(e.raw, 3306)
				if err != nil {
					t.Fatal(err)
				}
				fingerprint = changed.StableSHA256
			}
			r, err := c.verifyPlayback(context.Background(), fingerprint, target, func(string, *Snapshot) error {
				if name == "baseline-record-error" {
					return errors.New("disk full")
				}
				return nil
			}, time.Millisecond)
			if err == nil || r.WriteAttempted {
				t.Fatalf("invalid baseline accepted: %+v %v", r, err)
			}
			e.mu.Lock()
			defer e.mu.Unlock()
			if len(e.controlWrites) != 0 || e.editCount != 0 {
				t.Fatal("sent edit/control without valid persisted baseline")
			}
		})
	}
}

func TestPlaybackDoesNotOverwriteExternalChanges(t *testing.T) {
	for _, offset := range []int{4, 80, 3306, 3476} {
		c, e, before := playbackClient(t)
		e.onControl = func(n int, raw []byte) bool {
			if n == 1 {
				raw[offset] ^= 1
			}
			return false
		}
		r, err := c.verifyPlayback(context.Background(), before.StableSHA256, 9, func(string, *Snapshot) error { return nil }, time.Millisecond)
		if err == nil || !strings.Contains(err.Error(), "RESTORATION NOT VERIFIED") || r.RestoredVerified || r.IndependentVerified {
			t.Fatalf("external change at %d hidden: %+v %v", offset, r, err)
		}
		e.mu.Lock()
		writes := len(e.controlWrites)
		e.mu.Unlock()
		if writes != 1 {
			t.Fatalf("wrote after external change at %d: %d", offset, writes)
		}
	}
}

func TestPlaybackReportsPersistentRestorationFailure(t *testing.T) {
	c, e, before := playbackClient(t)
	e.onControl = func(n int, raw []byte) bool { e.dialFailure = true; return true }
	r, err := c.verifyPlayback(context.Background(), before.StableSHA256, 9, func(string, *Snapshot) error { return nil }, time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "RESTORATION NOT VERIFIED") || r.RestoredVerified {
		t.Fatalf("restoration failure hidden: %+v %v", r, err)
	}
}

func TestPlaybackKeepsGateDuringCleanupAndIndependentRead(t *testing.T) {
	c, _, before := playbackClient(t)
	r, err := c.verifyPlayback(context.Background(), before.StableSHA256, 9, func(stage string, _ *Snapshot) error {
		if stage == "restored" || stage == "independent" {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
			defer cancel()
			if _, err := c.Snapshot(ctx, 3306); !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("competing snapshot was not gated at %s: %v", stage, err)
			}
		}
		return nil
	}, time.Millisecond)
	if err != nil || !r.IndependentVerified {
		t.Fatalf("verification failed: %+v %v", r, err)
	}
}

func TestPlaybackWireCommandsAreBounded(t *testing.T) {
	for mode := 0; mode < 256; mode++ {
		p, err := playbackModePayload(byte(mode))
		if mode == 0 || mode == 2 {
			if err != nil || !bytes.Equal(p, []byte{2, 0, 1, byte(mode)}) {
				t.Fatal(p, err)
			}
		} else if err == nil {
			t.Fatalf("unobserved mode accepted: %d", mode)
		}
		p, err = savedPlaybackPayload(byte(mode))
		if mode >= 1 && mode <= 18 {
			if err != nil || !bytes.Equal(p, []byte{4, 0, 1, byte(mode)}) {
				t.Fatal(p, err)
			}
		} else if err == nil {
			t.Fatalf("unsupported slot accepted: %d", mode)
		}
	}
}

func TestPlaybackWorstCaseWindow(t *testing.T) {
	// Production values: 1 s pacing, 5 s I/O timeout and 10 s holds.
	worst := playbackWorstCase(time.Second, 5*time.Second, 10*time.Second)
	if worst != 166*time.Second {
		t.Fatalf("worst case %v", worst)
	}
	zone := time.FixedZone("controller", -5*3600)
	last := time.Date(2026, 10, 31, 23, 57, 13, 999999999, zone) // Saturday
	for _, tc := range []struct {
		start time.Time
		want  bool
	}{
		{time.Date(2026, 10, 31, 6, 0, 0, 0, zone), false},
		{last, false},
		{last.Add(time.Nanosecond), true}, // Ends exactly at Sunday 00:00:00.
		{time.Date(2026, 10, 31, 23, 59, 0, 0, zone), true},
		{time.Date(2026, 11, 1, 12, 0, 0, 0, zone), true},              // Sunday.
		{time.Date(2026, 11, 2, 0, 0, 0, 0, zone), false},              // Monday.
		{time.Date(2026, 11, 1, 3, 0, 0, 0, time.UTC).In(zone), false}, // UTC Sunday, local Saturday.
	} {
		if got := playbackWindowReachesSunday(tc.start, worst); got != tc.want {
			t.Errorf("%s: reaches Sunday = %v, want %v", tc.start, got, tc.want)
		}
	}
	if !playbackWindowReachesSunday(time.Date(2026, 10, 28, 10, 0, 0, 0, zone), 12*time.Hour) {
		t.Fatal("long window accepted")
	}
}

func TestPlaybackRefusesWindowReachingSunday(t *testing.T) {
	zone := time.FixedZone("controller", -5*3600)
	for _, tc := range []struct {
		name    string
		now     time.Time
		hold    time.Duration
		refused bool
	}{
		{"saturday-23:59", time.Date(2026, 10, 31, 23, 59, 0, 0, zone), time.Millisecond, true},
		// Test budgets total about 111 s: 23:58:00 ends on Saturday only
		// without the two 10 s holds, so the holds carry the window past midnight.
		{"window-crosses-midnight", time.Date(2026, 10, 31, 23, 58, 0, 0, zone), 10 * time.Second, true},
		{"late-saturday-window-fits", time.Date(2026, 10, 31, 23, 57, 0, 0, zone), time.Millisecond, false},
		{"early-saturday", time.Date(2026, 10, 31, 6, 0, 0, 0, zone), time.Millisecond, false},
		{"utc-sunday-local-saturday", time.Date(2026, 11, 1, 3, 0, 0, 0, time.UTC), time.Millisecond, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, e, before := playbackClient(t)
			c.location = zone
			c.now = func() time.Time { return tc.now }
			r, err := c.verifyPlayback(context.Background(), before.StableSHA256, 9, func(string, *Snapshot) error { return nil }, tc.hold)
			e.mu.Lock()
			defer e.mu.Unlock()
			if !tc.refused {
				if err != nil || !r.IndependentVerified || len(e.controlWrites) != 4 {
					t.Fatalf("allowed start failed: %+v %v writes=%v", r, err, e.controlWrites)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), "Sunday") || r.WriteAttempted || r.Before == nil {
				t.Fatalf("Sunday window not refused: %+v %v", r, err)
			}
			if len(e.controlWrites) != 0 || e.editCount != 0 {
				t.Fatalf("wrote before refusing: %v edits=%d", e.controlWrites, e.editCount)
			}
		})
	}
}
