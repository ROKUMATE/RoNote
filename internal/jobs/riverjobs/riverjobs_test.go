package riverjobs

import (
	"testing"
	"time"

	"github.com/riverqueue/river/rivertype"
)

func TestRetryPolicy(t *testing.T) {
	t.Parallel()
	now := time.Now()
	job := &rivertype.JobRow{Attempt: 2, AttemptedAt: &now, Errors: make([]rivertype.AttemptError, 1)}

	if got, want := (retryPolicy{delay: func(attempt int) time.Duration { return time.Duration(attempt) * time.Minute }}).NextRetry(job), afterDelay(2*time.Minute); !got.Equal(want) {
		t.Errorf("fixed delay: NextRetry() = %v, want %v", got, want)
	}

	// River's backoff is seconds after the first failure, not a time on any clock.
	got := (retryPolicy{}).NextRetry(job).Sub(delayOrigin)
	if got <= 0 || got > time.Minute {
		t.Errorf("default backoff after one failure = %v, want a delay of at most a minute", got)
	}
}

func TestAfterDelayIsNeverNegative(t *testing.T) {
	t.Parallel()
	if got := afterDelay(-time.Hour); !got.Equal(delayOrigin) {
		t.Errorf("afterDelay(-1h) = %v, want a delay of nothing", got)
	}
}
