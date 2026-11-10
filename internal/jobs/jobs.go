// Package jobs is the port between code that has work done later and what
// runs it (docs/adr/0009-jobs-port.md). A job is enqueued in the caller's
// Postgres transaction and delivered at least once to its kind's handler.
// riverjobs runs jobs on River, the default; memjobs runs them in memory,
// for unit tests; jobstest checks that both keep this contract.
package jobs

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// Job is work to run later. Payload is its handler's input, as JSON. Jobs
// with the same non-empty Key run once.
type Job struct {
	Kind    string
	Key     string
	Payload []byte
}

// Delivery is one attempt at a job. Attempt counts from 1.
type Delivery struct {
	Job     Job
	Attempt int
}

// Handler runs one attempt at a job. Nil completes the job; Permanent stops
// it; RetryAfter, or any other error, tries again until the kind's
// MaxAttempts. Since a job may be delivered more than once, a handler must
// do nothing the second time.
type Handler func(ctx context.Context, d Delivery) error

// Options say how a kind's jobs run.
type Options struct {
	// MaxAttempts counts every attempt, retries included.
	MaxAttempts int
	// Concurrency is how many of the kind's jobs may run at once.
	Concurrency int
	// Timeout ends each attempt's context.
	Timeout time.Duration
}

// Validate reports options a kind can't run with.
func (o Options) Validate() error {
	if o.MaxAttempts < 1 || o.Concurrency < 1 || o.Timeout <= 0 {
		return fmt.Errorf("jobs: options need MaxAttempts and Concurrency of at least 1 and a Timeout, got %+v", o)
	}
	return nil
}

// Enqueuer enqueues jobs in the caller's transaction: a job exists exactly
// when tx commits.
type Enqueuer interface {
	EnqueueTx(ctx context.Context, tx pgx.Tx, job Job) error
}

// Runner delivers jobs to handlers.
type Runner interface {
	// Handle runs kind's jobs with h. Call it before Run.
	Handle(kind string, h Handler, opts Options)
	// Run delivers jobs until ctx ends, then waits for the handlers still
	// running, cancelling them after a grace period.
	Run(ctx context.Context) error
}

type permanentError struct{ err error }

func (e *permanentError) Error() string { return e.err.Error() }
func (e *permanentError) Unwrap() error { return e.err }

// Permanent marks err as one that retrying won't help: its job stops.
func Permanent(err error) error {
	return &permanentError{err: err}
}

// IsPermanent reports whether err, or an error it wraps, came from Permanent.
func IsPermanent(err error) bool {
	var p *permanentError
	return errors.As(err, &p)
}

type retryAfterError struct {
	err   error
	delay time.Duration
}

func (e *retryAfterError) Error() string { return e.err.Error() }
func (e *retryAfterError) Unwrap() error { return e.err }

// RetryAfter asks for the next attempt no sooner than d from now, as a
// server's Retry-After does.
func RetryAfter(err error, d time.Duration) error {
	return &retryAfterError{err: err, delay: d}
}

// RetryDelay returns the delay a RetryAfter in err asked for.
func RetryDelay(err error) (time.Duration, bool) {
	var r *retryAfterError
	if errors.As(err, &r) {
		return r.delay, true
	}
	return 0, false
}
