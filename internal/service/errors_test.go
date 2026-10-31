package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"trimlights/internal/storage"
	"trimlights/internal/trimlight"
)

const fixtureAddress = "192.0.2.44:8586"

// addressError is a controller transport error that quotes the controller
// address, as net.Dial errors do.
func addressError(t *testing.T) error {
	t.Helper()
	remote, err := net.ResolveTCPAddr("tcp", fixtureAddress)
	if err != nil {
		t.Fatal(err)
	}
	return fmt.Errorf("connect: %w", &net.OpError{Op: "dial", Net: "tcp", Addr: remote, Err: fmt.Errorf("refused")})
}

// failingNative fails every control with a fixed error after a good baseline.
type failingNative struct {
	*nativeStub
	err error
}

func (f failingNative) Control(context.Context, trimlight.ControlRequest, func(*trimlight.Snapshot) error) (*trimlight.ControlResult, error) {
	return nil, f.err
}

type apiAnswer struct {
	code      int
	body      string
	message   string
	operation string
}

func post(t *testing.T, h http.Handler, token, path, body string) apiAnswer {
	t.Helper()
	r := httptest.NewRequest("POST", path, strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, r)
	var out struct {
		Error       string `json:"error"`
		OperationID string `json:"operation_id"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatal(rr.Code, rr.Body.String(), err)
	}
	return apiAnswer{rr.Code, rr.Body.String(), out.Error, out.OperationID}
}

func operation(t *testing.T, s *storage.Store, id string) storage.Operation {
	t.Helper()
	ops, err := s.Operations(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, op := range ops {
		if op.ID == id {
			return op
		}
	}
	t.Fatal("operation not recorded", id, ops)
	return storage.Operation{}
}

// A failed refresh answers a stable message and the operation ID. The detail
// stays in the operation record; the address never reaches the client.
func TestRefreshFailureAnswersStableMessage(t *testing.T) {
	for _, tc := range []struct {
		name, want string
		err        error
	}{
		{"transport", msgRefreshFailed, addressError(t)},
		{"sunday", trimlight.ErrSundayClock.Error(), fmt.Errorf("snapshot: %w", trimlight.ErrSundayClock)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, w := setup(t, controllerFunc(func(context.Context, int) (*trimlight.Snapshot, error) { return nil, tc.err }))
			token := strings.Repeat("a", 32)
			got := post(t, Handler(s, w, token, nil), token, "/api/v1/controller/refresh", `{"acknowledge_clock_sync":true}`)
			if got.code != 502 || got.message != tc.want || got.operation == "" || strings.Contains(got.body, "192.0.2.44") {
				t.Fatal(got)
			}
			if op := operation(t, s, got.operation); op.Error != tc.err.Error() {
				t.Fatal("operation lost the detail", op.Error)
			}
		})
	}
}

// A failed control answers a stable message and the operation ID.
func TestControlFailureAnswersStableMessage(t *testing.T) {
	for _, tc := range []struct {
		name, want string
		err        error
	}{
		{"transport", msgControlFailed, addressError(t)},
		{"sunday", trimlight.ErrSundayClock.Error(), fmt.Errorf("snapshot: %w", trimlight.ErrSundayClock)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stub := newNativeStub(t)
			s, w := setup(t, failingNative{stub, tc.err})
			w.offset = 3306
			snap, err := stub.Snapshot(context.Background(), 3306)
			if err != nil {
				t.Fatal(err)
			}
			if err = s.Record(context.Background(), storage.Observation{Snapshot: snap, Source: "fixture"}, storage.Operation{ID: "initial", Started: snap.QueriedAt, Finished: snap.QueriedAt}); err != nil {
				t.Fatal(err)
			}
			token := strings.Repeat("a", 32)
			body := fmt.Sprintf(`{"action":"off","baseline_sha256":%q,"acknowledge_clock_sync":true}`, snap.StableSHA256)
			got := post(t, Handler(s, w, token, nil), token, "/api/v1/controller/control", body)
			if got.code != 502 || got.message != tc.want || got.operation == "" || strings.Contains(got.body, "192.0.2.44") {
				t.Fatal(got)
			}
			if op := operation(t, s, got.operation); op.Error != tc.err.Error() {
				t.Fatal("operation lost the detail", op.Error)
			}
		})
	}
}

// A refused enqueue answers a fixed reason; storage text stays in the log.
func TestEnqueueRefusalAnswersStableMessage(t *testing.T) {
	token := strings.Repeat("a", 32)
	t.Run("storage", func(t *testing.T) {
		s, err := storage.Open(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		logger, logs := testLogger(slog.LevelInfo)
		h := Handler(s, NewWorker(s, newNativeStub(t), 3306, logger), token, logger)
		if err = s.Close(); err != nil {
			t.Fatal(err)
		}
		body := fmt.Sprintf(`{"action":"off","baseline_sha256":%q,"acknowledge_clock_sync":true}`, strings.Repeat("0", 64))
		got := post(t, h, token, "/api/v1/controller/control", body)
		if got.code != 503 || got.message != msgControllerUnavailable {
			t.Fatal(got)
		}
		if !strings.Contains(logs.String(), "closed") {
			t.Fatal("log lost the detail", logs.String())
		}
	})
	t.Run("unavailable", func(t *testing.T) {
		stub := newNativeStub(t)
		s, w := setup(t, stub)
		snap, err := stub.Snapshot(context.Background(), 3306)
		if err != nil {
			t.Fatal(err)
		}
		if err = s.Record(context.Background(), storage.Observation{Snapshot: snap, Source: "fixture"}, storage.Operation{ID: "initial", Started: snap.QueriedAt, Finished: snap.QueriedAt}); err != nil {
			t.Fatal(err)
		}
		body := fmt.Sprintf(`{"action":"off","baseline_sha256":%q,"acknowledge_clock_sync":true}`, snap.StableSHA256)
		got := post(t, Handler(s, w, token, nil), token, "/api/v1/controller/control", body)
		if _, reason := w.ControlAvailability(storage.Observation{Snapshot: snap}); got.code != 503 || got.message != reason {
			t.Fatal(got, reason)
		}
	})
	t.Run("disabled", func(t *testing.T) {
		s, w := setup(t, nil)
		got := post(t, Handler(s, w, token, nil), token, "/api/v1/controller/refresh", `{"acknowledge_clock_sync":true}`)
		if got.code != 503 || got.message != ErrDisabled.Error() {
			t.Fatal(got)
		}
	})
}
