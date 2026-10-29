package trimlight

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"sync"
	"testing"
	"time"
)

type emulator struct {
	mu             sync.Mutex
	raw            []byte
	writes         []byte
	disconnectOnce bool
	cancelOnWrite  context.CancelFunc
	controlWrites  [][]byte
	onControl      func(int, []byte) bool // called under mu; true disconnects
	timerPatternID byte
	connections    int
	dialFailure    bool
	editCount      int
}

func (e *emulator) dial(ctx context.Context, _, _ string) (net.Conn, error) {
	e.mu.Lock()
	e.connections++
	fail := e.dialFailure
	e.mu.Unlock()
	if fail {
		return nil, errors.New("simulated controller offline")
	}
	client, server := net.Pipe()
	go e.serve(server)
	return client, nil
}

func fragmented(w io.Writer, p []byte) error {
	for len(p) > 0 {
		n := min(7, len(p))
		if err := writeAll(w, p[:n]); err != nil {
			return err
		}
		p = p[n:]
	}
	return nil
}

func (e *emulator) serve(conn net.Conn) {
	defer conn.Close()
	b := make([]byte, len(hello))
	if _, err := io.ReadFull(conn, b); err != nil || !bytes.Equal(b, hello) {
		return
	}
	if fragmented(conn, []byte{0x53, 1, 0, 0, 3, 3, 'a', 'b', 'c'}) != nil {
		return
	}
	for {
		header := make([]byte, 10)
		if _, err := io.ReadFull(conn, header); err != nil {
			return
		}
		if !bytes.Equal(header[:6], []byte("SPTECH")) {
			return
		}
		body := make([]byte, 17+int(binary.BigEndian.Uint16(header[8:10])))
		if _, err := io.ReadFull(conn, body); err != nil {
			return
		}
		payload := make([]byte, len(body)-17)
		for i := range payload {
			payload[i] = body[17+i] ^ body[16]
		}
		if header[6] == 11 {
			e.mu.Lock()
			e.editCount++
			e.mu.Unlock()
			if fragmented(conn, editReply) != nil {
				return
			}
			continue
		}
		if len(payload) == 0 {
			return
		}
		switch payload[0] {
		case 2, 4:
			if len(payload) != 4 || payload[1] != 0 || payload[2] != 1 {
				return
			}
			e.mu.Lock()
			e.controlWrites = append(e.controlWrites, bytes.Clone(payload))
			id := payload[3]
			if payload[0] == 2 {
				if id != 0 && id != 2 {
					e.mu.Unlock()
					return
				}
				e.raw[3] = id
				if id == 2 {
					id = e.timerPatternID
				} else {
					id = 0
				}
			} else {
				e.raw[3] = 1
			}
			if id >= 1 && id <= 18 {
				start := 52 + (int(id)-1)*182
				copy(e.raw[3320:3476], e.raw[start:start+156])
			}
			closeNow := false
			if e.onControl != nil {
				closeNow = e.onControl(len(e.controlWrites), e.raw)
			}
			e.mu.Unlock()
			if closeNow {
				return
			}
		case 0x15:
			e.mu.Lock()
			raw := bytes.Clone(e.raw)
			e.mu.Unlock()
			frame, _ := encodeFrame(4, raw, 0x90)
			if fragmented(conn, frame) != nil {
				return
			}
		case 0x13:
			if len(payload) != 13 || payload[3] != 1 || payload[4] != 0 {
				return
			}
			e.mu.Lock()
			copy(e.raw[3313:3320], payload[4:11])
			e.writes = append(e.writes, payload[10])
			closeNow := e.disconnectOnce && payload[10] == 4
			e.disconnectOnce = false
			cancel := e.cancelOnWrite
			e.cancelOnWrite = nil
			e.mu.Unlock()
			if cancel != nil {
				cancel()
			}
			if closeNow {
				return
			}
		default:
			return
		}
	}
}

func testClient(t *testing.T) (*Client, *emulator) {
	t.Helper()
	e := &emulator{raw: fixture(t)}
	c := NewClient("example.invalid:8586", time.UTC)
	c.dial = e.dial
	c.pause = time.Millisecond
	c.timeout = 100 * time.Millisecond
	c.now = func() time.Time { return time.Date(2026, 10, 28, 10, 0, 0, 0, time.UTC) }
	return c, e
}

func TestFragmentedLiveSnapshot(t *testing.T) {
	c, _ := testClient(t)
	s, err := c.Snapshot(context.Background(), 3306)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Patterns) != 18 || s.Schedules[1].Off != "00:03" {
		t.Fatal("bad snapshot")
	}
}

func TestProbeAndRecovery(t *testing.T) {
	for _, name := range []string{"success", "disconnect-after-write", "cancel-after-write", "record-error", "restore-record-error"} {
		t.Run(name, func(t *testing.T) {
			c, e := testClient(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if name == "disconnect-after-write" {
				e.disconnectOnce = true
			}
			if name == "cancel-after-write" {
				e.cancelOnWrite = cancel
			}
			recorded := map[string]bool{}
			r, err := c.ProbeSchedule2(ctx, fixtureFingerprint(t), func(stage string, s *Snapshot) error {
				recorded[stage] = true
				if stage == "changed" && name == "record-error" {
					return errors.New("disk full")
				}
				if stage == "restored" && name == "restore-record-error" {
					return errors.New("disk full during restoration recording")
				}
				return nil
			})
			if name == "success" && err != nil {
				t.Fatal(err)
			}
			if name != "success" && err == nil {
				t.Fatal("failure was hidden")
			}
			if !r.RestoredVerified || !r.IndependentVerified {
				t.Fatalf("restoration not verified: %+v %v", r, err)
			}
			if name == "success" && !r.ChangedVerified {
				t.Fatal("change not verified")
			}
			e.mu.Lock()
			defer e.mu.Unlock()
			if !bytes.Equal(e.writes, []byte{4, 3}) {
				t.Fatalf("writes %v", e.writes)
			}
			if !bytes.Equal(e.raw, fixture(t)) {
				t.Fatal("did not restore exact fixture")
			}
			if !recorded["before"] || !recorded["restored"] || !recorded["independent"] {
				t.Fatal(recorded)
			}
		})
	}
}

func TestRecorderFailurePreventsWrite(t *testing.T) {
	c, e := testClient(t)
	_, err := c.ProbeSchedule2(context.Background(), fixtureFingerprint(t), func(string, *Snapshot) error { return errors.New("disk full") })
	if err == nil {
		t.Fatal("error lost")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.writes) != 0 {
		t.Fatal("wrote without baseline backup")
	}
}

func TestCancellationWhileQueued(t *testing.T) {
	c, _ := testClient(t)
	c.gate <- struct{}{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Snapshot(ctx, 3306); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestCommandPacing(t *testing.T) {
	c, _ := testClient(t)
	c.pause = 20 * time.Millisecond
	start := time.Now()
	err := c.withSession(context.Background(), func(s *session) error {
		if _, err := s.snapshot(context.Background(), 3306); err != nil {
			return err
		}
		return s.enterEdit(context.Background())
	})
	if err != nil {
		t.Fatal(err)
	}
	if time.Since(start) < c.pause {
		t.Fatal("command pacing skipped")
	}
}

func TestProbeRejectsUnreviewedFreshState(t *testing.T) {
	e := &emulator{raw: fixture(t)}
	c := NewClient("example.invalid:8586", time.UTC)
	c.dial = e.dial
	c.pause = 0
	c.now = func() time.Time { return time.Date(2000, 1, 5, 12, 0, 0, 0, time.UTC) }
	fingerprint := fixtureFingerprint(t)
	e.raw[80] ^= 1
	report, err := c.ProbeSchedule2(context.Background(), fingerprint, func(string, *Snapshot) error { return nil })
	if err == nil || report.ChangedVerified {
		t.Fatal(report, err)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.writes) != 0 {
		t.Fatal("changed controller baseline was written")
	}
}
