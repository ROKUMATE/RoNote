// Package riverjobs runs the jobs port on River, on Postgres, the default
// adapter (docs/adr/0009-jobs-port.md). It is the one package that imports
// River. Each kind is a River queue of its own. River keeps a job's attempt
// limit on its row, so a process that only enqueues declares its kinds in
// Options.Kinds.
package riverjobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivertype"

	"github.com/rokumate/ronote/internal/jobs"
)

// Options tune a Queue. Zero values mean River's defaults, or the ones noted.
type Options struct {
	Logger *slog.Logger
	// Kinds declares the options of kinds this process enqueues but doesn't
	// handle. Handle declares the ones it handles.
	Kinds map[string]jobs.Options
	// RetryDelay replaces River's backoff, by the attempt that failed; tests
	// shorten it.
	RetryDelay        func(attempt int) time.Duration
	FetchCooldown     time.Duration
	FetchPollInterval time.Duration
	// StopTimeout is how long Run waits for running handlers once its context
	// ends, 30 seconds by default; CancelTimeout is how long it then waits
	// for the ones it cancelled, 5 seconds by default.
	StopTimeout   time.Duration
	CancelTimeout time.Duration
}

// Queue is an Enqueuer and a Runner on River.
type Queue struct {
	pool     *pgxpool.Pool
	opts     Options
	inserter *river.Client[pgx.Tx]

	mu       sync.Mutex
	kinds    map[string]jobs.Options
	handlers map[string]jobs.Handler

	// retryAfter holds, by job ID, the delay a failed attempt asked for, from
	// the attempt to River's NextRetry.
	retryMu    sync.Mutex
	retryAfter map[int64]time.Duration
}

var (
	_ jobs.Enqueuer = (*Queue)(nil)
	_ jobs.Runner   = (*Queue)(nil)
)

// New returns a Queue on pool.
func New(pool *pgxpool.Pool, opts Options) (*Queue, error) {
	if opts.Logger == nil {
		opts.Logger = slog.New(slog.DiscardHandler)
	}
	if opts.StopTimeout == 0 {
		opts.StopTimeout = 30 * time.Second
	}
	if opts.CancelTimeout == 0 {
		opts.CancelTimeout = 5 * time.Second
	}
	for kind, o := range opts.Kinds {
		if err := o.Validate(); err != nil {
			return nil, fmt.Errorf("riverjobs: kind %q: %w", kind, err)
		}
	}
	inserter, err := river.NewClient(riverpgxv5.New(pool), &river.Config{Logger: opts.Logger})
	if err != nil {
		return nil, err
	}
	kinds := map[string]jobs.Options{}
	maps.Copy(kinds, opts.Kinds)
	return &Queue{
		pool:       pool,
		opts:       opts,
		inserter:   inserter,
		kinds:      kinds,
		handlers:   map[string]jobs.Handler{},
		retryAfter: map[int64]time.Duration{},
	}, nil
}

// args is a job as River stores it. Key is the job's own, made unique among
// the kind's jobs when it isn't empty.
type args struct {
	kind    string
	Key     string          `json:"key,omitempty" river:"unique"`
	Payload json.RawMessage `json:"payload"`
}

// Kind is the job's kind, which is also its queue.
func (a args) Kind() string { return a.kind }

// EnqueueTx inserts job in tx. Its kind must be declared.
func (q *Queue) EnqueueTx(ctx context.Context, tx pgx.Tx, job jobs.Job) error {
	q.mu.Lock()
	o, ok := q.kinds[job.Kind]
	q.mu.Unlock()
	if !ok {
		return fmt.Errorf("riverjobs: kind %q is not declared", job.Kind)
	}
	payload := json.RawMessage(job.Payload)
	if len(payload) == 0 {
		payload = json.RawMessage("null")
	}
	insert := &river.InsertOpts{Queue: job.Kind, MaxAttempts: o.MaxAttempts}
	if job.Key != "" {
		insert.UniqueOpts = river.UniqueOpts{ByArgs: true}
	}
	_, err := q.inserter.InsertTx(ctx, tx, args{kind: job.Kind, Key: job.Key, Payload: payload}, insert)
	return err
}

// Handle runs kind's jobs with h. It panics on options a kind can't run with.
func (q *Queue) Handle(kind string, h jobs.Handler, opts jobs.Options) {
	if err := opts.Validate(); err != nil {
		panic(err)
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	q.kinds[kind] = opts
	q.handlers[kind] = h
}

// Run works every handled kind's queue until ctx ends, then stops River:
// running handlers get StopTimeout to finish, then are cancelled.
func (q *Queue) Run(ctx context.Context) error {
	workers := river.NewWorkers()
	queues := map[string]river.QueueConfig{}
	q.mu.Lock()
	for kind, h := range q.handlers {
		o := q.kinds[kind]
		river.AddWorkerArgs(workers, args{kind: kind}, &worker{queue: q, handler: h, opts: o})
		queues[kind] = river.QueueConfig{MaxWorkers: o.Concurrency}
	}
	q.mu.Unlock()
	if len(queues) == 0 {
		return errors.New("riverjobs: Run needs a handler")
	}
	cfg := &river.Config{
		Logger:            q.opts.Logger,
		Workers:           workers,
		Queues:            queues,
		FetchCooldown:     q.opts.FetchCooldown,
		FetchPollInterval: q.opts.FetchPollInterval,
	}
	cfg.RetryPolicy = retryPolicy{delay: q.opts.RetryDelay}
	client, err := river.NewClient(riverpgxv5.New(q.pool), cfg)
	if err != nil {
		return err
	}
	// River gets its own context: stopping is done below, so that running
	// jobs can finish first.
	if err := client.Start(context.WithoutCancel(ctx)); err != nil {
		return fmt.Errorf("start: %w", err)
	}
	<-ctx.Done()

	q.opts.Logger.Info("jobs stopping", "timeout_ms", q.opts.StopTimeout.Milliseconds())
	stopCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), q.opts.StopTimeout)
	defer cancel()
	if err := client.Stop(stopCtx); err != nil {
		if !errors.Is(err, context.DeadlineExceeded) {
			return fmt.Errorf("stop: %w", err)
		}
		cancelCtx, cancelDone := context.WithTimeout(context.WithoutCancel(ctx), q.opts.CancelTimeout)
		defer cancelDone()
		if err := client.StopAndCancel(cancelCtx); err != nil {
			return fmt.Errorf("stop and cancel: %w", err)
		}
	}
	return nil
}

// worker runs one kind's jobs with its handler.
type worker struct {
	river.WorkerDefaults[args]
	queue   *Queue
	handler jobs.Handler
	opts    jobs.Options
}

// Timeout bounds one attempt.
func (w *worker) Timeout(*river.Job[args]) time.Duration { return w.opts.Timeout }

// Work runs the handler and says what River does next: Permanent cancels
// the job, and a RetryAfter waits for NextRetry.
func (w *worker) Work(ctx context.Context, job *river.Job[args]) error {
	err := w.handler(ctx, jobs.Delivery{
		Job:     jobs.Job{Kind: job.Kind, Key: job.Args.Key, Payload: job.Args.Payload},
		Attempt: job.Attempt,
	})
	switch {
	case err == nil:
		return nil
	case jobs.IsPermanent(err):
		return river.JobCancel(err)
	}
	if delay, ok := jobs.RetryDelay(err); ok && job.Attempt < job.MaxAttempts {
		w.queue.retryMu.Lock()
		w.queue.retryAfter[job.ID] = delay
		w.queue.retryMu.Unlock()
	}
	return err
}

// NextRetry honours a RetryAfter from the attempt that just failed; without
// one, the retry policy decides. On River's retry path, unlike a snooze,
// migration 00008's trigger makes the delay the database's clock plus d.
func (w *worker) NextRetry(job *river.Job[args]) time.Time {
	w.queue.retryMu.Lock()
	delay, ok := w.queue.retryAfter[job.ID]
	delete(w.queue.retryAfter, job.ID)
	w.queue.retryMu.Unlock()
	if !ok {
		return time.Time{}
	}
	return afterDelay(delay)
}

// retryPolicy retries after delay, by the attempt that failed, or after
// River's own backoff when delay is nil.
type retryPolicy struct{ delay func(attempt int) time.Duration }

func (p retryPolicy) NextRetry(job *rivertype.JobRow) time.Time {
	if p.delay != nil {
		return afterDelay(p.delay(job.Attempt))
	}
	return afterDelay(time.Until((&river.DefaultClientRetryPolicy{}).NextRetry(job)))
}

// delayOrigin is the time River stores for a retry delay of nothing. River
// only takes a retry time in the future, so a delay can't be sent as a
// distance from the epoch.
var delayOrigin = time.Date(3000, 1, 1, 0, 0, 0, 0, time.UTC)

// afterDelay is the retry time River stores for a retry delay from the
// failure: d past delayOrigin. A delay is not a point on the app's clock, so
// migration 00008's trigger turns it into the database's clock plus d,
// whichever clock the app has and however long the attempt waited or ran.
func afterDelay(d time.Duration) time.Time {
	return delayOrigin.Add(max(d, 0))
}
