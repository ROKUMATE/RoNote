package jobs

import (
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestHandlerErrors(t *testing.T) {
	t.Parallel()
	cause := errors.New("site down")
	tests := []struct {
		name      string
		err       error
		permanent bool
		delay     time.Duration
		retries   bool
	}{
		{name: "plain", err: cause},
		{name: "permanent", err: Permanent(cause), permanent: true},
		{name: "permanent, wrapped", err: fmt.Errorf("import: %w", Permanent(cause)), permanent: true},
		{name: "retry after", err: RetryAfter(cause, time.Minute), delay: time.Minute, retries: true},
		{name: "retry after, wrapped", err: fmt.Errorf("import: %w", RetryAfter(cause, time.Second)), delay: time.Second, retries: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := IsPermanent(tt.err); got != tt.permanent {
				t.Errorf("IsPermanent() = %v, want %v", got, tt.permanent)
			}
			if delay, ok := RetryDelay(tt.err); delay != tt.delay || ok != tt.retries {
				t.Errorf("RetryDelay() = %v, %v; want %v, %v", delay, ok, tt.delay, tt.retries)
			}
			if !errors.Is(tt.err, cause) || tt.err.Error() == "" {
				t.Errorf("%v does not wrap its cause", tt.err)
			}
		})
	}
}

func TestOptionsValidate(t *testing.T) {
	t.Parallel()
	good := Options{MaxAttempts: 3, Concurrency: 4, Timeout: time.Minute}
	if err := good.Validate(); err != nil {
		t.Errorf("Validate(%+v) = %v", good, err)
	}
	for _, bad := range []Options{
		{Concurrency: 1, Timeout: time.Second},
		{MaxAttempts: 1, Timeout: time.Second},
		{MaxAttempts: 1, Concurrency: 1},
	} {
		if bad.Validate() == nil {
			t.Errorf("Validate(%+v) = nil, want an error", bad)
		}
	}
}
