// Package memjobs runs the jobs port in memory, for unit tests: no
// Postgres, and retries timed by a clock the test controls
// (docs/adr/0009-jobs-port.md). Jobs are delivered first in, first out.
package memjobs

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/rokumate/ronote/internal/jobs"
)

// Options tune a Queue. Zero values mean the defaults.
type Options struct {
	// Clock times retries; the wall clock by default. Attempts' timeouts run
	// on the wall clock whatever it is.
	Clock Clock
	// RetryDelay is how long a plain error waits before the next attempt, by
	// the attempt that failed; a second by default.
	RetryDelay func(attempt int) time.Duration
	// Grace is how long Run waits for running handlers once its context
	// ends before it cancels theirs; five seconds by default.
	Grace time.Duration
}

// Queue is an Enqueuer and a Runner in memory.
type Queue struct {
	clock Clock
	retry func(int) time.Duration
	grace time.Duration

	mu      sync.Mutex
	kinds   map[string]*kind
	waiting []*pending
	keys    map[string]bool
	wake    chan struct{}
	running sync.WaitGroup
}

type kind struct {
	handler jobs.Handler
	opts    jobs.Options
	running int
}

// pending is a job waiting for its next attempt, due at at.
type pending struct {
	job     jobs.Job
	attempt int
	at      time.Time
}

var (
	_ jobs.Enqueuer = (*Queue)(nil)
	_ jobs.Runner   = (*Queue)(nil)
)

// New returns an empty Queue.
func New(opts Options) *Queue {
	q := &Queue{
		clock: opts.Clock,
		retry: opts.RetryDelay,
		grace: opts.Grace,
		kinds: map[string]*kind{},
		keys:  map[string]bool{},
		wake:  make(chan struct{}, 1),
	}
	if q.clock == nil {
		q.clock = wallClock{}
	}
	if q.retry == nil {
		q.retry = func(int) time.Duration { return time.Second }
	}
	if q.grace == 0 {
		q.grace = 5 * time.Second
	}
	return q
}

// Tx is the transaction memjobs' EnqueueTx takes: jobs enqueued on it appear
// when it commits and vanish when it rolls back. It is a pgx.Tx in name
// only: any method but Commit and Rollback panics.
type Tx struct {
	pgx.Tx
	q    *Queue
	jobs []jobs.Job
	done bool
}

// Begin starts a transaction to enqueue jobs on.
func (q *Queue) Begin() *Tx {
	return &Tx{q: q}
}

// Commit enqueues the transaction's jobs.
func (t *Tx) Commit(context.Context) error {
	if t.done {
		return pgx.ErrTxClosed
	}
	t.done = true
	t.q.add(t.jobs)
	return nil
}

// Rollback drops the transaction's jobs.
func (t *Tx) Rollback(context.Context) error {
	if t.done {
		return pgx.ErrTxClosed
	}
	t.done = true
	t.jobs = nil
	return nil
}

// EnqueueTx enqueues job when tx, from this queue's Begin, commits.
func (q *Queue) EnqueueTx(_ context.Context, tx pgx.Tx, job jobs.Job) error {
	t, ok := tx.(*Tx)
	if !ok || t.q != q {
		return errors.New("memjobs: EnqueueTx takes a transaction from this queue's Begin")
	}
	if t.done {
		return pgx.ErrTxClosed
	}
	t.jobs = append(t.jobs, job)
	return nil
}

func (q *Queue) add(js []jobs.Job) {
	q.mu.Lock()
	for _, job := range js {
		if job.Key != "" {
			key := job.Kind + "\x00" + job.Key
			if q.keys[key] {
				continue
			}
			q.keys[key] = true
		}
		q.waiting = append(q.waiting, &pending{job: job, attempt: 1, at: q.clock.Now()})
	}
	q.mu.Unlock()
	q.poke()
}

// Handle runs kind's jobs with h. It panics on options a kind can't run with.
func (q *Queue) Handle(name string, h jobs.Handler, opts jobs.Options) {
	if err := opts.Validate(); err != nil {
		panic(err)
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	q.kinds[name] = &kind{handler: h, opts: opts}
}

// Run delivers jobs until ctx ends, then waits for running handlers,
// cancelling them after the grace period.
func (q *Queue) Run(ctx context.Context) error {
	base, cancel := context.WithCancel(context.WithoutCancel(ctx))
	defer cancel()
	for {
		next, later := q.dispatch(base)
		var due <-chan time.Time
		if later {
			due = q.clock.After(next.Sub(q.clock.Now()))
		}
		select {
		case <-ctx.Done():
			return q.drain(cancel)
		case <-q.wake:
		case <-due:
		}
	}
}

// drain waits for the running handlers, cancelling them after the grace period.
func (q *Queue) drain(cancel context.CancelFunc) error {
	done := make(chan struct{})
	go func() {
		q.running.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(q.grace):
		cancel()
		<-done
	}
	return nil
}

// dispatch starts every job that is due and has room in its kind, and
// returns when the next one not yet due will be.
func (q *Queue) dispatch(ctx context.Context) (next time.Time, later bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	now := q.clock.Now()
	kept := q.waiting[:0]
	for _, p := range q.waiting {
		k := q.kinds[p.job.Kind]
		switch {
		case k == nil || k.running >= k.opts.Concurrency:
			kept = append(kept, p)
		case p.at.After(now):
			kept = append(kept, p)
			if !later || p.at.Before(next) {
				next, later = p.at, true
			}
		default:
			k.running++
			q.running.Add(1)
			go q.deliver(ctx, k, p)
		}
	}
	q.waiting = kept
	return next, later
}

// deliver runs one attempt, then completes the job or schedules its next.
func (q *Queue) deliver(ctx context.Context, k *kind, p *pending) {
	defer q.running.Done()
	attempt, cancel := context.WithTimeout(ctx, k.opts.Timeout)
	err := k.handler(attempt, jobs.Delivery{Job: p.job, Attempt: p.attempt})
	cancel()

	q.mu.Lock()
	k.running--
	if err != nil && !jobs.IsPermanent(err) && p.attempt < k.opts.MaxAttempts {
		delay, ok := jobs.RetryDelay(err)
		if !ok {
			delay = q.retry(p.attempt)
		}
		q.waiting = append(q.waiting, &pending{job: p.job, attempt: p.attempt + 1, at: q.clock.Now().Add(delay)})
	}
	q.mu.Unlock()
	q.poke()
}

// poke wakes Run to look at the queue again.
func (q *Queue) poke() {
	select {
	case q.wake <- struct{}{}:
	default:
	}
}
