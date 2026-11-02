package httpapi

import (
	"context"
	"crypto/rand"
	"errors"
	"log/slog"
	"math"
	"net/http"
	"regexp"
	"runtime/debug"
	"time"
)

const requestIDHeader = "X-Request-ID"

var validRequestID = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

type requestIDKey struct{}

// withMiddleware wraps h in request ID, access log and panic recovery,
// outermost first.
func withMiddleware(logger *slog.Logger, h http.Handler) http.Handler {
	return withRequestID(withAccessLog(logger, withRecovery(logger, h)))
}

// withRequestID keeps a valid incoming X-Request-ID or generates a new one,
// echoes it in the response and stores it in the request context.
func withRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(requestIDHeader)
		if !validRequestID.MatchString(id) {
			id = rand.Text()
		}
		w.Header().Set(requestIDHeader, id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestIDKey{}, id)))
	})
}

func requestID(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}

// withAccessLog logs one line per request once the response is done.
func withAccessLog(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w}
		next.ServeHTTP(sw, r)

		status := sw.status
		if status == 0 {
			status = http.StatusOK
		}
		logger.LogAttrs(r.Context(), slog.LevelInfo, "request",
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.Int("status", status),
			slog.Float64("duration_ms", durationMS(time.Since(start))),
			slog.String("request_id", requestID(r.Context())),
		)
	})
}

// durationMS returns d in milliseconds rounded to 3 decimals, so logs read
// the same in every handler instead of JSON printing raw nanoseconds.
func durationMS(d time.Duration) float64 {
	return math.Round(float64(d)/float64(time.Microsecond)) / 1000
}

// withRecovery logs a panic and answers with a 500 envelope if no response
// has started.
func withRecovery(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sw := &statusWriter{ResponseWriter: w}
		defer recoverPanic(logger, sw, r)
		next.ServeHTTP(sw, r)
	})
}

// recoverPanic must be deferred directly so that recover sees the panic.
// http.ErrAbortHandler is re-raised so net/http can abort the connection as
// intended.
func recoverPanic(logger *slog.Logger, w *statusWriter, r *http.Request) {
	p := recover()
	if p == nil {
		return
	}
	if err, ok := p.(error); ok && errors.Is(err, http.ErrAbortHandler) {
		panic(p)
	}
	logger.LogAttrs(r.Context(), slog.LevelError, "panic",
		slog.Any("panic", p),
		slog.String("stack", string(debug.Stack())),
		slog.String("request_id", requestID(r.Context())),
	)
	if w.status == 0 {
		writeError(w, r, http.StatusInternalServerError, "internal_error", "internal server error")
	}
}

// statusWriter records the final status code written through it.
type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(status int) {
	if w.status == 0 && status >= http.StatusOK {
		w.status = status
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(b)
}

// Unwrap lets http.ResponseController reach the underlying writer.
func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
