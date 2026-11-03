package importer

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"github.com/rokumate/ronote/internal/fetch"
	"github.com/rokumate/ronote/internal/store"
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
		})
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

func TestRetryAfterIsUsedOnce(t *testing.T) {
	t.Parallel()
	w := NewWorker(nil, nil, 0, nil)
	job := &river.Job[Args]{JobRow: &rivertype.JobRow{ID: 7, Attempt: 1, MaxAttempts: 3}}
	err := w.failed(t.Context(), job, importFixture(), &fetch.StatusError{Code: http.StatusTooManyRequests, RetryAfter: time.Hour})
	if err == nil {
		t.Fatal("failed() = nil, want the error back for a retry")
	}
	if next := w.NextRetry(job); time.Until(next) < maxRetryAfter-time.Minute || time.Until(next) > maxRetryAfter {
		t.Errorf("NextRetry() = %v from now, want the 10 minute cap", time.Until(next))
	}
	if next := w.NextRetry(job); !next.IsZero() {
		t.Errorf("second NextRetry() = %v, want zero so River's backoff applies", next)
	}
}

func importFixture() store.Import {
	return store.Import{ID: uuid.New(), OwnerID: uuid.New(), Status: store.ImportFetching}
}
