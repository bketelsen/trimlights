package service

import (
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"time"
)

// loggerOrDefault keeps every logging site safe when a caller passes no logger.
func loggerOrDefault(logger *slog.Logger) *slog.Logger {
	if logger == nil {
		return slog.Default()
	}
	return logger
}

// serverError records the cause of a 5xx response once and answers the
// unchanged generic message. The route is the matched ServeMux pattern.
func serverError(w http.ResponseWriter, r *http.Request, logger *slog.Logger, code int, message string, err error) {
	logFailure(r, logger, code, err)
	jsonError(w, code, message)
}

func logFailure(r *http.Request, logger *slog.Logger, code int, err error, attrs ...any) {
	attrs = append([]any{"route", r.Pattern, "status", code, "error", safeError(err)}, attrs...)
	loggerOrDefault(logger).ErrorContext(r.Context(), "request failed", attrs...)
}

// safeError describes an error without network addresses. Controller transport
// errors carry the controller address, which must never reach the log.
func safeError(err error) string {
	if err == nil {
		return ""
	}
	var dns *net.DNSError
	if errors.As(err, &dns) {
		return "controller name lookup failed"
	}
	var op *net.OpError
	if errors.As(err, &op) {
		message := "controller network " + op.Op + " failed"
		var syscall *os.SyscallError
		switch {
		case op.Timeout():
			message += ": timeout"
		case errors.As(op.Err, &syscall):
			message += ": " + syscall.Err.Error()
		}
		return message
	}
	return err.Error()
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	if s.status == 0 {
		s.status = code
	}
	s.ResponseWriter.WriteHeader(code)
}
func (s *statusRecorder) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	return s.ResponseWriter.Write(b)
}
func (s *statusRecorder) Unwrap() http.ResponseWriter { return s.ResponseWriter }

// requestLog records method, path without query, status and duration at debug
// level, so the default info level stays free of request and probe noise.
func requestLog(logger *slog.Logger, next http.Handler) http.Handler {
	logger = loggerOrDefault(logger)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !logger.Enabled(r.Context(), slog.LevelDebug) {
			next.ServeHTTP(w, r)
			return
		}
		started := time.Now()
		recorder := &statusRecorder{ResponseWriter: w}
		next.ServeHTTP(recorder, r)
		status := recorder.status
		if status == 0 {
			status = http.StatusOK
		}
		logger.DebugContext(r.Context(), "request", "method", r.Method, "path", r.URL.Path, "status", status, "duration_ms", time.Since(started).Milliseconds())
	})
}
