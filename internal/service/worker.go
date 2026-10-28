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
type request struct {
	id, actor string
	ctx       context.Context
	done      chan error
}
type Worker struct {
	controller Controller
	store      *storage.Store
	offset     int
	queue      chan request
	mu         sync.RWMutex
	active     string
	lastError  string
	pace       time.Duration
}

func NewWorker(store *storage.Store, controller Controller, offset int) *Worker {
	return &Worker{store: store, controller: controller, offset: offset, queue: make(chan request, 8), pace: time.Second}
}
func (w *Worker) Enqueue(ctx context.Context, actor string) (string, <-chan error, error) {
	if w.controller == nil {
		return "", nil, ErrDisabled
	}
	if err := ctx.Err(); err != nil {
		return "", nil, err
	}
	r := request{rand.Text(), actor, ctx, make(chan error, 1)}
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
			snap, readErr := w.controller.Snapshot(opCtx, w.offset)
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
				if readErr == nil {
					state.Snapshot = snap
					state.Source = "controller"
				}
				persistErr = w.store.Record(persistCtx, state, op)
			}
			persistCancel()
			w.mu.Lock()
			w.active = ""
			w.lastError = ""
			if persistErr != nil {
				w.lastError = persistErr.Error()
			}
			w.mu.Unlock()
			r.done <- errors.Join(readErr, persistErr)
		}
	}
}
