package trimlight

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net"
	"time"
)

// ErrSundayClock refuses a snapshot on Sunday. Every snapshot sends a clock
// query, and the Sunday clock encoding is not verified.
var ErrSundayClock = errors.New("controller clock sync is unavailable on Sunday")

// Client serializes sessions for a controller. Use one instance per controller.
type Client struct {
	address  string
	location *time.Location
	timeout  time.Duration
	pause    time.Duration
	gate     chan struct{}
	now      func() time.Time
	dial     func(context.Context, string, string) (net.Conn, error)
}

func NewClient(address string, location *time.Location) *Client {
	if location == nil {
		location = time.UTC
	}
	return &Client{address: address, location: location, timeout: 5 * time.Second, pause: time.Second,
		gate: make(chan struct{}, 1), now: time.Now, dial: (&net.Dialer{Timeout: 5 * time.Second}).DialContext}
}

type session struct {
	client *Client
	conn   net.Conn
	next   time.Time
}

func wait(ctx context.Context, d time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if d <= 0 {
		return nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (c *Client) withSession(ctx context.Context, fn func(*session) error) error {
	select {
	case c.gate <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-c.gate }()
	return c.openSession(ctx, fn)
}

// openSession permits recovery connections while an operation retains the gate.
// Call only from withSession or while its callback still owns the gate.
func (c *Client) openSession(ctx context.Context, fn func(*session) error) error {
	conn, err := c.dial(ctx, "tcp", c.address)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer conn.Close()
	s := &session{client: c, conn: conn}
	if err = s.io(ctx, func() error {
		if err := writeAll(conn, hello); err != nil {
			return err
		}
		header := make([]byte, 6)
		if _, err := io.ReadFull(conn, header); err != nil {
			return err
		}
		length := int(header[2])<<16 | int(header[3])<<8 | int(header[4])
		if !bytes.Equal(header[:2], []byte{0x53, 1}) || length > 4096 {
			return errors.New("invalid handshake")
		}
		_, err := io.CopyN(io.Discard, conn, int64(length))
		return err
	}); err != nil {
		return fmt.Errorf("handshake: %w", err)
	}
	return fn(s)
}

func writeAll(w io.Writer, p []byte) error {
	for len(p) > 0 {
		n, err := w.Write(p)
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		p = p[n:]
	}
	return nil
}

// Bound each whole exchange, and interrupt blocked I/O on cancellation. Wait for
// the cancellation callback before returning so it cannot affect cleanup I/O.
func (s *session) io(ctx context.Context, fn func() error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	deadline := time.Now().Add(s.client.timeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	if err := s.conn.SetDeadline(deadline); err != nil {
		return err
	}
	done := make(chan struct{})
	stop := context.AfterFunc(ctx, func() { _ = s.conn.SetDeadline(time.Now()); close(done) })
	err := fn()
	if !stop() {
		<-done
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}

func (s *session) exchange(ctx context.Context, cmd byte, payload []byte, read func() error) error {
	if err := wait(ctx, time.Until(s.next)); err != nil {
		return err
	}
	var key [1]byte
	if _, err := rand.Read(key[:]); err != nil {
		return err
	}
	frame, err := encodeFrame(cmd, payload, key[0])
	if err != nil {
		return err
	}
	err = s.io(ctx, func() error {
		if err := writeAll(s.conn, frame); err != nil {
			return err
		}
		if read != nil {
			return read()
		}
		return nil
	})
	// Space commands from exchange completion, including acknowledgments.
	s.next = time.Now().Add(s.client.pause)
	return err
}

func (s *session) snapshot(ctx context.Context, offset int) (*Snapshot, error) {
	// Get a fresh timestamp after pacing rather than before a potentially long wait.
	if err := wait(ctx, time.Until(s.next)); err != nil {
		return nil, err
	}
	now := s.client.now().In(s.client.location)
	if now.Weekday() == time.Sunday {
		return nil, ErrSundayClock
	}
	query, err := clockQuery(now)
	if err != nil {
		return nil, err
	}
	var raw []byte
	err = s.exchange(ctx, 4, query, func() error { var e error; raw, e = readSnapshotFrame(s.conn); return e })
	if err != nil {
		return nil, fmt.Errorf("snapshot: %w", err)
	}
	result, err := ParseSnapshot(raw, offset)
	if err != nil {
		return nil, err
	}
	result.QueriedAt = now
	result.ClockSyncPossible = true
	return result, nil
}

func (s *session) enterEdit(ctx context.Context) error {
	return s.exchange(ctx, 11, []byte{1}, func() error {
		response := make([]byte, len(editReply))
		if _, err := io.ReadFull(s.conn, response); err != nil {
			return err
		}
		if !bytes.Equal(response, editReply) {
			return errors.New("unexpected edit-session response")
		}
		return nil
	})
}

func (s *session) writeProbe(ctx context.Context, minute byte) error {
	payload, err := probePayload(minute, s.client.now().In(s.client.location))
	if err != nil {
		return err
	}
	return s.exchange(ctx, 4, payload, nil)
}

func (c *Client) Snapshot(ctx context.Context, offset int) (result *Snapshot, err error) {
	err = c.withSession(ctx, func(s *session) error { result, err = s.snapshot(ctx, offset); return err })
	return
}

type ProbeReport struct {
	Before              *Snapshot `json:"before,omitempty"`
	Changed             *Snapshot `json:"changed,omitempty"`
	Restored            *Snapshot `json:"restored,omitempty"`
	Independent         *Snapshot `json:"independent,omitempty"`
	Reconciled          *Snapshot `json:"reconciled,omitempty"` // latest read before a restoration write
	ChangedVerified     bool      `json:"changed_verified"`
	RestoredVerified    bool      `json:"restored_verified"`
	IndependentVerified bool      `json:"independent_reconnect_verified"`
}

// ProbeSchedule2 is deliberately restricted to the captured disabled schedule.
// record must persist the baseline before any write, and must not call this Client:
// one gate acquisition covers the probe, cleanup, reconnect recovery and the
// independent read. Cleanup has its own contexts so cancelling the experiment does
// not cancel the restoration attempt.
func (c *Client) ProbeSchedule2(ctx context.Context, expectedFingerprint string, record func(string, *Snapshot) error) (*ProbeReport, error) {
	if !validFingerprint(expectedFingerprint) {
		return nil, errors.New("a reviewed baseline SHA-256 fingerprint is required")
	}
	if record == nil {
		return nil, errors.New("a snapshot recorder is required")
	}
	r := &ProbeReport{}
	attempted, refused := false, false
	var restoreErr error
	// Reconcile before each restoration write. Only the probe's off-minute byte
	// may differ from the baseline; any other change stops cleanup unwritten.
	restore := func(s *session, clean context.Context, edit bool) error {
		state, err := s.snapshot(clean, 3306)
		if err != nil {
			return err
		}
		r.Reconciled = state
		if err = validateProbeRestoration(r.Before, state); err != nil {
			refused = true
			return err
		}
		if edit {
			if err = s.enterEdit(clean); err != nil {
				return err
			}
		}
		if err = s.writeProbe(clean, 3); err != nil {
			return err
		}
		state, err = s.snapshot(clean, 3306)
		if err != nil {
			return err
		}
		r.Restored = state
		if !bytes.Equal(stable(state), stable(r.Before)) {
			return errors.New("restoration differs from baseline")
		}
		r.RestoredVerified = true
		return record("restored", state)
	}
	err := c.withSession(ctx, func(s *session) (err error) {
		r.Before, err = s.snapshot(ctx, 3306)
		if err != nil {
			return err
		}
		if err = validateProbeBaseline(r.Before, expectedFingerprint); err != nil {
			return err
		}
		if err = record("before", r.Before); err != nil {
			return err
		}
		if err = s.enterEdit(ctx); err != nil {
			return err
		}
		defer func() {
			if attempted {
				restoreErr = c.probeCleanup(s, r, restore, &refused, record)
			}
		}()
		attempted = true // Also restore after a partial/ambiguous send failure.
		if err = s.writeProbe(ctx, 4); err != nil {
			return err
		}
		r.Changed, err = s.snapshot(ctx, 3306)
		if err != nil {
			return err
		}
		expected := bytes.Clone(stable(r.Before))
		expected[probeOffMinuteOffset] = 4
		if !bytes.Equal(stable(r.Changed), expected) {
			return errors.New("read-back differs from intended single-byte change")
		}
		r.ChangedVerified = true
		return record("changed", r.Changed)
	})
	if attempted && !r.RestoredVerified {
		restoreErr = errors.Join(errors.New("RESTORATION NOT VERIFIED"), restoreErr)
	}
	return r, errors.Join(err, restoreErr)
}

// probeCleanup runs inside the probe's withSession callback, so the gate stays
// held. Fresh connections therefore use openSession, never withSession.
func (c *Client) probeCleanup(s *session, r *ProbeReport, restore func(*session, context.Context, bool) error, refused *bool, record func(string, *Snapshot) error) error {
	clean, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	restoreErr := restore(s, clean, false)
	cancel()
	_ = s.conn.Close()
	// A broken socket must not prevent a bounded restoration on a fresh session.
	if !r.RestoredVerified && !*refused {
		clean, cancel := context.WithTimeout(context.Background(), 35*time.Second)
		for i := 0; i < 2 && !r.RestoredVerified && !*refused; i++ {
			// Pace across connections as within one session.
			if restoreErr = wait(clean, c.pause); restoreErr == nil {
				restoreErr = c.openSession(clean, func(fresh *session) error { return restore(fresh, clean, true) })
			}
		}
		cancel()
	}
	if !r.RestoredVerified {
		return restoreErr
	}
	independent, stop := context.WithTimeout(context.Background(), 10*time.Second)
	independentErr := wait(independent, c.pause)
	if independentErr == nil {
		independentErr = c.openSession(independent, func(fresh *session) (err error) {
			r.Independent, err = fresh.snapshot(independent, 3306)
			return err
		})
	}
	stop()
	if independentErr == nil {
		r.IndependentVerified = bytes.Equal(stable(r.Independent), stable(r.Before))
		if !r.IndependentVerified {
			independentErr = errors.New("independent read differs from baseline")
		} else {
			independentErr = record("independent", r.Independent)
		}
	}
	return errors.Join(restoreErr, independentErr)
}
