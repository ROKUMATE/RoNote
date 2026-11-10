package importer

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/rokumate/ronote/internal/fetch"
	"github.com/rokumate/ronote/internal/httpapi/gen"
	"github.com/rokumate/ronote/internal/jobs"
	"github.com/rokumate/ronote/internal/jobs/memjobs"
)

func TestClassify(t *testing.T) {
	t.Parallel()
	wrap := func(err error) error { return fmt.Errorf("fetch https://docs.example.com/: %w", err) }
	tests := []struct {
		name string
		err  error
		want failure
	}{
		{"invalid URL", wrap(fetch.ErrInvalidURL), failure{code: "invalid_url", permanent: true}},
		{"blocked address", wrap(fetch.ErrBlockedAddress), failure{code: "blocked_address", permanent: true}},
		{"robots", wrap(fetch.ErrDisallowedByRobots), failure{code: "disallowed_by_robots", permanent: true}},
		{"robots.txt unavailable", wrap(fetch.ErrRobotsUnavailable), failure{code: "robots_unavailable"}},
		{"too large", wrap(fetch.ErrTooLarge), failure{code: "too_large", permanent: true}},
		{"unsupported type", wrap(fetch.ErrUnsupportedType), failure{code: "unsupported_type", permanent: true}},
		{"redirects", wrap(fetch.ErrTooManyRedirects), failure{code: "too_many_redirects", permanent: true}},
		{"extraction", fmt.Errorf("%w: bad", errExtract), failure{code: "extract_failed", permanent: true}},
		{"404", wrap(&fetch.StatusError{Code: http.StatusNotFound}), failure{code: "not_found", permanent: true}},
		{"410", wrap(&fetch.StatusError{Code: http.StatusGone}), failure{code: "not_found", permanent: true}},
		{"403", wrap(&fetch.StatusError{Code: http.StatusForbidden}), failure{code: "http_error", permanent: true}},
		{"timeout", wrap(fetch.ErrTimeout), failure{code: "timeout"}},
		{"deadline", context.DeadlineExceeded, failure{code: "timeout"}},
		{"503", wrap(&fetch.StatusError{Code: http.StatusServiceUnavailable, RetryAfter: time.Minute}), failure{code: "upstream_error", retryAfter: time.Minute}},
		{"500", wrap(&fetch.StatusError{Code: http.StatusInternalServerError}), failure{code: "upstream_error"}},
		{"429", wrap(&fetch.StatusError{Code: http.StatusTooManyRequests, RetryAfter: 2 * time.Second}), failure{code: "rate_limited", retryAfter: 2 * time.Second}},
		{"connection refused", errors.New("dial tcp: connection refused"), failure{code: "network_error"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := classify(tt.err); got != tt.want {
				t.Errorf("classify(%v) = %+v, want %+v", tt.err, got, tt.want)
			}
			if !gen.ImportErrorCode(tt.want.code).Valid() {
				t.Errorf("code %q is not in the spec's ImportErrorCode enum", tt.want.code)
			}
		})
	}
	if !gen.ImportErrorCode(codeInternal).Valid() {
		t.Errorf("code %q is not in the spec's ImportErrorCode enum", codeInternal)
	}
}

func TestValidate(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in, want string
		wantErr  error
	}{
		{in: "https://go.dev/doc/effective_go#intro", want: "https://go.dev/doc/effective_go"},
		{in: "http://localhost:3000/docs", want: "http://localhost:3000/docs"},
		{in: "ftp://go.dev/", wantErr: ErrUnsupportedScheme},
		{in: "go.dev/doc", wantErr: ErrInvalidURL},
		{in: "", wantErr: ErrInvalidURL},
		{in: "https://go.dev/" + string(make([]byte, 2048)), wantErr: ErrURLTooLong},
		{in: "http://127.0.0.1:8080/", wantErr: ErrIPLiteralHost},
		{in: "http://169.254.169.254/latest/meta-data/", wantErr: ErrIPLiteralHost},
		{in: "http://[::1]/", wantErr: ErrIPLiteralHost},
	}
	for _, tt := range tests {
		got, err := Validate(tt.in)
		if !errors.Is(err, tt.wantErr) || got != tt.want {
			t.Errorf("Validate(%.40q) = %q, %v; want %q, %v", tt.in, got, err, tt.want, tt.wantErr)
		}
	}
}

func TestOutcome(t *testing.T) {
	t.Parallel()
	busy := &fetch.StatusError{Code: http.StatusTooManyRequests, RetryAfter: 2 * time.Second}
	tests := []struct {
		name      string
		cause     error
		attempt   int
		code      string
		permanent bool
		delay     time.Duration
	}{
		{name: "transient, retried by backoff", cause: &fetch.StatusError{Code: http.StatusBadGateway}, attempt: 1},
		{name: "Retry-After, honoured", cause: busy, attempt: 2, delay: 2 * time.Second},
		{name: "Retry-After, capped", cause: &fetch.StatusError{Code: http.StatusServiceUnavailable, RetryAfter: time.Hour}, attempt: 1, delay: maxRetryAfter},
		{name: "transient on the last attempt", cause: busy, attempt: maxAttempts, code: "rate_limited"},
		{name: "permanent", cause: &fetch.StatusError{Code: http.StatusNotFound}, attempt: 1, code: "not_found", permanent: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			code, err := outcome(tt.cause, tt.attempt)
			delay, _ := jobs.RetryDelay(err)
			if code != tt.code || jobs.IsPermanent(err) != tt.permanent || delay != tt.delay || !errors.Is(err, tt.cause) {
				t.Errorf("outcome() = %q, %v (permanent %v, delay %v); want %q, permanent %v, delay %v",
					code, err, jobs.IsPermanent(err), delay, tt.code, tt.permanent, tt.delay)
			}
		})
	}
}

// A site's Retry-After of an hour, through the jobs port: the next attempt
// comes at the 10-minute cap, and no sooner.
func TestRetryAfterIsCappedOnTheQueue(t *testing.T) {
	t.Parallel()
	clock := memjobs.NewFakeClock(time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC))
	q := memjobs.New(memjobs.Options{Clock: clock})
	attempts := make(chan time.Time, 4)
	q.Handle(Kind, func(_ context.Context, d jobs.Delivery) error {
		attempts <- clock.Now()
		_, err := outcome(&fetch.StatusError{Code: http.StatusTooManyRequests, RetryAfter: time.Hour}, d.Attempt)
		return err
	}, JobOptions)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go func() { _ = q.Run(ctx) }()

	j, err := job(Args{ImportID: uuid.New(), OwnerID: uuid.New()})
	if err != nil {
		t.Fatal(err)
	}
	tx := q.Begin()
	if err := q.EnqueueTx(ctx, tx, j); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	first := <-attempts
	q.Idle()
	clock.Advance(maxRetryAfter - time.Second)
	select {
	case at := <-attempts:
		t.Fatalf("retried %v after the first attempt, before the cap", at.Sub(first))
	case <-time.After(100 * time.Millisecond):
	}
	clock.Advance(time.Second)
	select {
	case at := <-attempts:
		if gap := at.Sub(first); gap != maxRetryAfter {
			t.Errorf("retried %v after the first attempt, want %v", gap, maxRetryAfter)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no retry at the cap")
	}
}
