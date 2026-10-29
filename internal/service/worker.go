package service

import (
	"context"
	"crypto/rand"
	"errors"
	"sync"
	"time"

	"trimlights/internal/storage"
	"trimlights/internal/trimlight"
)

var ErrQueueFull = errors.New("controller queue is full")
var ErrDisabled = errors.New("controller access is disabled")

type Controller interface {
	Snapshot(context.Context, int) (*trimlight.Snapshot, error)
}
type NativeController interface {
	Controller
	Control(context.Context, trimlight.ControlRequest, func(*trimlight.Snapshot) error) (*trimlight.ControlResult, error)
}
type request struct {
	id, actor string
	ctx       context.Context
	done      chan error
	control   *trimlight.ControlRequest
}
type Worker struct {
	controller      Controller
	store           *storage.Store
	offset          int
	queue           chan request
	mu              sync.RWMutex
	active          string
	lastError       string
	pace            time.Duration
	controlsBlocked bool
}

func NewWorker(store *storage.Store, controller Controller, offset int) *Worker {
	return &Worker{store: store, controller: controller, offset: offset, queue: make(chan request, 8), pace: time.Second}
}
func (w *Worker) Enqueue(ctx context.Context, actor string) (string, <-chan error, error) {
	return w.enqueue(ctx, actor, nil)
}

func (w *Worker) ControlAvailability(state storage.Observation) (bool, string) {
	w.mu.RLock()
	blocked := w.controlsBlocked
	w.mu.RUnlock()
	if blocked {
		return false, "Audit persistence failed; restart the service after fixing storage."
	}
	if _, ok := w.controller.(NativeController); !ok || w.offset != 3306 {
		return false, "Native controls are not configured for the verified layout."
	}
	if state.LastError != "" {
		return false, "Refresh the controller after the last failed operation."
	}
	if !trimlight.SupportsNativeControl(state.Snapshot) {
		return false, "Refresh to load a supported controller snapshot."
	}
	return true, "Native saved-slot controls are available; Calendar execution remains disabled."
}

func (w *Worker) EnqueueControl(ctx context.Context, actor string, input trimlight.ControlRequest) (string, <-chan error, error) {
	if err := input.Validate(); err != nil {
		return "", nil, err
	}
	state, err := w.store.Observation(ctx)
	if err != nil {
		return "", nil, err
	}
	if available, reason := w.ControlAvailability(state); !available {
		return "", nil, errors.New(reason)
	}
	if state.Snapshot.StableSHA256 != input.BaselineSHA256 {
		return "", nil, trimlight.ErrStaleBaseline
	}
	return w.enqueue(ctx, actor, &input)
}

func (w *Worker) enqueue(ctx context.Context, actor string, control *trimlight.ControlRequest) (string, <-chan error, error) {
	if w.controller == nil {
		return "", nil, ErrDisabled
	}
	if err := ctx.Err(); err != nil {
		return "", nil, err
	}
	r := request{id: rand.Text(), actor: actor, ctx: ctx, done: make(chan error, 1), control: control}
	select {
	case w.queue <- r:
		return r.id, r.done, nil
	default:
		return "", nil, ErrQueueFull
	}
}

type WorkerStatus struct {
	Active  string `json:"active_operation,omitempty"`
	Queued  int    `json:"queued"`
	Enabled bool   `json:"enabled"`
	Error   string `json:"persistence_error,omitempty"`
}

func (w *Worker) Status() WorkerStatus {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return WorkerStatus{w.active, len(w.queue), w.controller != nil, w.lastError}
}
func (w *Worker) Run(ctx context.Context) {
	defer func() {
		for {
			select {
			case r := <-w.queue:
				r.done <- context.Canceled
			default:
				return
			}
		}
	}()
	var next time.Time
	for {
		select {
		case <-ctx.Done():
			return
		case r := <-w.queue:
			if err := r.ctx.Err(); err != nil {
				r.done <- err
				continue
			}
			timer := time.NewTimer(time.Until(next))
			select {
			case <-ctx.Done():
				timer.Stop()
				r.done <- ctx.Err()
				return
			case <-r.ctx.Done():
				timer.Stop()
				r.done <- r.ctx.Err()
				continue
			case <-timer.C:
			}
			w.mu.Lock()
			w.active = r.id
			w.mu.Unlock()
			op := storage.Operation{ID: r.id, Actor: r.actor, Started: time.Now().UTC()}
			opCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
			stop := context.AfterFunc(r.ctx, cancel)
			var snap *trimlight.Snapshot
			var readErr, pendingErr error
			pending := false
			if r.control == nil {
				snap, readErr = w.controller.Snapshot(opCtx, w.offset)
			} else {
				op.Control = &storage.ControlAudit{Request: *r.control, Outcome: "rejected"}
				state, err := w.store.Observation(opCtx)
				readErr = err
				if readErr == nil {
					if available, reason := w.ControlAvailability(state); !available {
						readErr = errors.New(reason)
					}
				}
				if readErr == nil && state.Snapshot.StableSHA256 != r.control.BaselineSHA256 {
					readErr = trimlight.ErrStaleBaseline
				}
				if readErr == nil {
					var controlResult *trimlight.ControlResult
					controlResult, readErr = w.controller.(NativeController).Control(opCtx, *r.control, func(before *trimlight.Snapshot) error {
						state.Snapshot, state.Source = before, "controller"
						state.LastAttempt, state.LastError = op.Started, ""
						op.Control.Outcome, op.Finished = "pending", op.Started
						pendingErr = w.store.Record(opCtx, state, op)
						pending = pendingErr == nil
						return pendingErr
					})
					if controlResult != nil {
						snap = controlResult.Snapshot
						op.Control.Outcome = controlResult.Outcome
						op.Control.WriteAttempted = controlResult.WriteAttempted
						op.Control.TransportError = controlResult.TransportError
					} else {
						op.Control.Outcome = "rejected"
					}
					if readErr == nil && (controlResult == nil || (controlResult.Outcome != "verified" && controlResult.Outcome != "reconciled")) {
						readErr = errors.New("controller did not provide a verified control result")
					}
				}
			}
			stop()
			cancel()
			next = time.Now().Add(w.pace)
			op.Finished = time.Now().UTC()
			if readErr != nil {
				op.Error = readErr.Error()
			}
			// Finish audit with a short independent context even when the caller leaves.
			persistCtx, persistCancel := context.WithTimeout(context.Background(), 3*time.Second)
			state, persistErr := w.store.Observation(persistCtx)
			if persistErr == nil {
				state.LastAttempt = op.Started
				state.LastError = op.Error
				if snap != nil && (readErr == nil || r.control != nil) {
					state.Snapshot = snap
					state.Source = "controller"
				}
				if pending {
					persistErr = w.store.CompleteControl(persistCtx, state, op)
				} else {
					persistErr = w.store.Record(persistCtx, state, op)
				}
			}
			persistCancel()
			persistErr = errors.Join(pendingErr, persistErr)
			w.mu.Lock()
			w.active = ""
			w.lastError = ""
			if persistErr != nil {
				w.lastError = persistErr.Error()
				w.controlsBlocked = true
			}
			w.mu.Unlock()
			r.done <- errors.Join(readErr, persistErr)
		}
	}
}
