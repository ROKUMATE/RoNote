// Package jobstest checks that an adapter keeps the jobs contract
// (docs/adr/0009-jobs-port.md): one suite, run against every adapter, each
// case on a fresh, empty adapter.
package jobstest

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/rokumate/ronote/internal/jobs"
)

// Adapter is an adapter under test, with what the suite needs to drive it.
type Adapter struct {
	Enqueuer jobs.Enqueuer
	Runner   jobs.Runner
	// Begin starts a transaction the adapter's EnqueueTx takes.
	Begin func(ctx context.Context) (pgx.Tx, error)
	// Wait lets d pass by the adapter's clock.
	Wait func(d time.Duration)
	// Now is the time by the adapter's clock.
	Now func() time.Time
	// RetryDelay is how long the adapter waits after a plain error.
	RetryDelay time.Duration
	// Settle is how long, on the wall clock, the suite waits before it
	// concludes that nothing more will be delivered.
	Settle time.Duration
}

// retryAfter is what the suite's RetryAfter asks for: longer than an
// adapter's RetryDelay and Settle together, so an adapter that ignored it
// would deliver early.
const retryAfter = 3 * time.Second

// patience bounds every wait for something that must happen.
const patience = 15 * time.Second

// Run runs the suite, each case on an adapter from newAdapter.
func Run(t *testing.T, newAdapter func(t *testing.T) Adapter) {
	cases := []struct {
		name string
		run  func(t *testing.T, a Adapter)
	}{
		{"a handler error redelivers", redelivers},
		{"Permanent stops retries", permanentStops},
		{"RetryAfter is honoured", retryAfterHonoured},
		{"retries stop at the attempt limit", attemptsLimited},
		{"a rolled-back transaction leaves no job", rollbackLeavesNone},
		{"the same Key enqueued twice runs once", keyRunsOnce},
		{"the concurrency limit holds", concurrencyLimited},
		{"an attempt's context ends at its timeout", timeoutEnds},
		{"shutdown waits for a running handler", shutdownWaits},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { c.run(t, newAdapter(t)) })
	}
}

var errFails = errors.New("this attempt fails")

func options(maxAttempts, concurrency int) jobs.Options {
	return jobs.Options{MaxAttempts: maxAttempts, Concurrency: concurrency, Timeout: 10 * time.Second}
}

func job(kind, key, payload string) jobs.Job {
	return jobs.Job{Kind: kind, Key: key, Payload: []byte(`"` + payload + `"`)}
}

// start runs a's Runner until the test ends.
func start(t *testing.T, a Adapter) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.WithoutCancel(t.Context()))
	done := make(chan error, 1)
	go func() { done <- a.Runner.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("Run() = %v", err)
			}
		case <-time.After(patience):
			t.Errorf("Run() did not return within %v of its context ending", patience)
		}
	})
}

// enqueue enqueues js in one transaction, and commits it.
func enqueue(t *testing.T, a Adapter, js ...jobs.Job) {
	t.Helper()
	tx, err := a.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, j := range js {
		if err := a.Enqueuer.EnqueueTx(t.Context(), tx, j); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(t.Context()); err != nil {
		t.Fatal(err)
	}
}

// delivered is one delivery as a handler saw it.
type delivered struct {
	payload string
	attempt int
	at      time.Time
}

// recorder hands every delivery to a channel, and returns what fail says.
func recorder(a Adapter, fail func(jobs.Delivery) error) (jobs.Handler, chan delivered) {
	got := make(chan delivered, 32)
	return func(_ context.Context, d jobs.Delivery) error {
		got <- delivered{payload: string(d.Job.Payload), attempt: d.Attempt, at: a.Now()}
		return fail(d)
	}, got
}

func next(t *testing.T, got <-chan delivered) delivered {
	t.Helper()
	select {
	case d := <-got:
		return d
	case <-time.After(patience):
		t.Fatalf("no delivery within %v", patience)
		return delivered{}
	}
}

func none(t *testing.T, a Adapter, got <-chan delivered) {
	t.Helper()
	select {
	case d := <-got:
		t.Fatalf("unexpected delivery: %+v", d)
	case <-time.After(a.Settle):
	}
}

func redelivers(t *testing.T, a Adapter) {
	h, got := recorder(a, func(d jobs.Delivery) error {
		if d.Attempt == 1 {
			return errFails
		}
		return nil
	})
	a.Runner.Handle("redelivers", h, options(3, 1))
	start(t, a)
	enqueue(t, a, job("redelivers", "", "x"))
	if d := next(t, got); d.attempt != 1 {
		t.Fatalf("first delivery = %+v, want attempt 1", d)
	}
	a.Wait(a.RetryDelay)
	if d := next(t, got); d.attempt != 2 {
		t.Fatalf("second delivery = %+v, want attempt 2", d)
	}
	a.Wait(2 * a.RetryDelay)
	none(t, a, got)
}

func permanentStops(t *testing.T, a Adapter) {
	h, got := recorder(a, func(jobs.Delivery) error { return jobs.Permanent(errFails) })
	a.Runner.Handle("permanent", h, options(3, 1))
	start(t, a)
	enqueue(t, a, job("permanent", "", "x"))
	next(t, got)
	a.Wait(2 * a.RetryDelay)
	none(t, a, got)
}

func retryAfterHonoured(t *testing.T, a Adapter) {
	if a.RetryDelay+a.Settle >= retryAfter {
		t.Fatalf("RetryDelay %v and Settle %v must add up to less than %v", a.RetryDelay, a.Settle, retryAfter)
	}
	h, got := recorder(a, func(d jobs.Delivery) error {
		if d.Attempt == 1 {
			return jobs.RetryAfter(errFails, retryAfter)
		}
		return nil
	})
	a.Runner.Handle("retry-after", h, options(3, 1))
	start(t, a)
	enqueue(t, a, job("retry-after", "", "x"))
	first := next(t, got)
	// Past the plain retry delay, not past the one asked for.
	a.Wait(a.RetryDelay)
	none(t, a, got)
	a.Wait(retryAfter)
	second := next(t, got)
	if gap := second.at.Sub(first.at); second.attempt != 2 || gap < retryAfter {
		t.Errorf("second delivery = attempt %d, %v after the first; want attempt 2 at least %v after", second.attempt, gap, retryAfter)
	}
}

func attemptsLimited(t *testing.T, a Adapter) {
	h, got := recorder(a, func(jobs.Delivery) error { return errFails })
	a.Runner.Handle("limited", h, options(2, 1))
	start(t, a)
	enqueue(t, a, job("limited", "", "x"))
	for want := 1; want <= 2; want++ {
		if d := next(t, got); d.attempt != want {
			t.Fatalf("delivery = %+v, want attempt %d", d, want)
		}
		a.Wait(a.RetryDelay)
	}
	a.Wait(2 * a.RetryDelay)
	none(t, a, got)
}

func rollbackLeavesNone(t *testing.T, a Adapter) {
	h, got := recorder(a, func(jobs.Delivery) error { return nil })
	a.Runner.Handle("rollback", h, options(1, 1))
	start(t, a)
	tx, err := a.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Enqueuer.EnqueueTx(t.Context(), tx, job("rollback", "", "rolled back")); err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(t.Context()); err != nil {
		t.Fatal(err)
	}
	// One at a time, first in first out: had the job survived, it would run first.
	enqueue(t, a, job("rollback", "", "committed"))
	if d := next(t, got); d.payload != `"committed"` {
		t.Fatalf("delivery = %+v, want the committed job", d)
	}
	none(t, a, got)
}

func keyRunsOnce(t *testing.T, a Adapter) {
	h, got := recorder(a, func(jobs.Delivery) error { return nil })
	a.Runner.Handle("keyed", h, options(1, 1))
	enqueue(t, a, job("keyed", "k", "first"))
	enqueue(t, a, job("keyed", "k", "second"))
	enqueue(t, a, job("keyed", "", "unkeyed"))
	start(t, a)
	for _, want := range []string{`"first"`, `"unkeyed"`} {
		if d := next(t, got); d.payload != want {
			t.Fatalf("delivery = %+v, want %s", d, want)
		}
	}
	// A key that has run stays taken.
	enqueue(t, a, job("keyed", "k", "third"))
	none(t, a, got)
}

func concurrencyLimited(t *testing.T, a Adapter) {
	var running, most atomic.Int32
	started := make(chan struct{}, 8)
	release := make(chan struct{})
	a.Runner.Handle("busy", func(context.Context, jobs.Delivery) error {
		now := running.Add(1)
		for {
			m := most.Load()
			if now <= m || most.CompareAndSwap(m, now) {
				break
			}
		}
		started <- struct{}{}
		<-release
		running.Add(-1)
		return nil
	}, options(1, 2))
	start(t, a)
	enqueue(t, a, job("busy", "", "1"), job("busy", "", "2"), job("busy", "", "3"), job("busy", "", "4"), job("busy", "", "5"))
	wait := func(n int) {
		t.Helper()
		for range n {
			select {
			case <-started:
			case <-time.After(patience):
				t.Fatalf("a handler did not start within %v", patience)
			}
		}
	}
	wait(2)
	select {
	case <-started:
		t.Fatal("a third handler started while two ran")
	case <-time.After(a.Settle):
	}
	close(release)
	wait(3)
	if m := most.Load(); m != 2 {
		t.Errorf("at most %d handlers ran at once, want 2", m)
	}
}

func timeoutEnds(t *testing.T, a Adapter) {
	ended := make(chan error, 1)
	a.Runner.Handle("slow", func(ctx context.Context, _ jobs.Delivery) error {
		<-ctx.Done()
		ended <- ctx.Err()
		return ctx.Err()
	}, jobs.Options{MaxAttempts: 1, Concurrency: 1, Timeout: 200 * time.Millisecond})
	start(t, a)
	enqueue(t, a, job("slow", "", "x"))
	select {
	case err := <-ended:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("the attempt's context ended with %v, want its deadline", err)
		}
	case <-time.After(patience):
		t.Fatalf("the attempt's context did not end within %v", patience)
	}
}

func shutdownWaits(t *testing.T, a Adapter) {
	started := make(chan struct{})
	release := make(chan struct{})
	var finished atomic.Bool
	var cancelled atomic.Bool
	a.Runner.Handle("finishing", func(ctx context.Context, _ jobs.Delivery) error {
		close(started)
		<-release
		cancelled.Store(ctx.Err() != nil)
		finished.Store(true)
		return nil
	}, options(1, 1))
	ctx, cancel := context.WithCancel(context.WithoutCancel(t.Context()))
	defer cancel()
	stopped := make(chan error, 1)
	go func() { stopped <- a.Runner.Run(ctx) }()
	enqueue(t, a, job("finishing", "", "x"))
	select {
	case <-started:
	case <-time.After(patience):
		t.Fatalf("the handler did not start within %v", patience)
	}

	cancel()
	select {
	case err := <-stopped:
		t.Fatalf("Run() = %v while its handler was still running", err)
	case <-time.After(a.Settle):
	}
	close(release)
	select {
	case err := <-stopped:
		if err != nil {
			t.Errorf("Run() = %v", err)
		}
	case <-time.After(patience):
		t.Fatalf("Run() did not return within %v of its handler finishing", patience)
	}
	if !finished.Load() || cancelled.Load() {
		t.Errorf("handler finished = %v, cancelled = %v; want it to finish, uncancelled", finished.Load(), cancelled.Load())
	}
}
