package memjobs

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/rokumate/ronote/internal/jobs"
	"github.com/rokumate/ronote/internal/jobs/jobstest"
)

func TestConformance(t *testing.T) {
	jobstest.Run(t, func(*testing.T) jobstest.Adapter {
		clock := NewFakeClock(time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC))
		q := New(Options{Clock: clock, RetryDelay: func(int) time.Duration { return time.Second }})
		return jobstest.Adapter{
			Enqueuer: q,
			Runner:   q,
			Begin:    func(context.Context) (pgx.Tx, error) { return q.Begin(), nil },
			Wait: func(d time.Duration) {
				idle(q)
				clock.Advance(d)
			},
			Now:        clock.Now,
			RetryDelay: time.Second,
			Settle:     100 * time.Millisecond,
		}
	})
}

// idle waits until no handler runs, so the retry a finished attempt asked
// for is scheduled before the clock moves past it.
func idle(q *Queue) {
	for {
		q.mu.Lock()
		busy := false
		for _, k := range q.kinds {
			busy = busy || k.running > 0
		}
		q.mu.Unlock()
		if !busy {
			return
		}
		time.Sleep(time.Millisecond)
	}
}

func TestTransactions(t *testing.T) {
	t.Parallel()
	q, other := New(Options{}), New(Options{})
	ctx := t.Context()
	job := jobs.Job{Kind: "k", Payload: []byte("{}")}

	if err := q.EnqueueTx(ctx, other.Begin(), job); err == nil {
		t.Error("EnqueueTx() took another queue's transaction")
	}
	tx := q.Begin()
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := q.EnqueueTx(ctx, tx, job); !errors.Is(err, pgx.ErrTxClosed) {
		t.Errorf("EnqueueTx() on a committed transaction = %v, want ErrTxClosed", err)
	}
	if err := tx.Rollback(ctx); !errors.Is(err, pgx.ErrTxClosed) {
		t.Errorf("Rollback() after Commit() = %v, want ErrTxClosed", err)
	}
}

func TestFakeClock(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	c := NewFakeClock(start)
	soon, later, now := c.After(time.Second), c.After(time.Minute), c.After(0)
	select {
	case <-now:
	default:
		t.Error("After(0) did not fire at once")
	}
	c.Advance(time.Second)
	select {
	case at := <-soon:
		if !at.Equal(start.Add(time.Second)) {
			t.Errorf("fired at %v", at)
		}
	default:
		t.Error("After(1s) did not fire after Advance(1s)")
	}
	select {
	case <-later:
		t.Error("After(1m) fired after Advance(1s)")
	default:
	}
}
