//go:build integration

package pgtest

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"
)

// riverHeader is the first line scripts/river-migration.sh writes.
var riverHeader = regexp.MustCompile(`(?m)^-- River main-line migrations (\d+) to (\d+), from River (v\S+)\.$`)

// TestRiverMigrationsAreCurrent fails when the River version go.mod pins
// knows a migration that no goose migration holds yet.
func TestRiverMigrationsAreCurrent(t *testing.T) {
	t.Parallel()
	migrations := os.DirFS(filepath.Join(repoRoot(), "migrations"))
	files, err := fs.Glob(migrations, "*.sql")
	if err != nil {
		t.Fatal(err)
	}
	covered := 1 // River's version 1 is its own migration table, which goose replaces
	for _, f := range files {
		data, err := fs.ReadFile(migrations, f)
		if err != nil {
			t.Fatal(err)
		}
		if m := riverHeader.FindSubmatch(data); m != nil {
			last, _ := strconv.Atoi(string(m[2]))
			covered = max(covered, last)
		}
	}
	migrator, err := rivermigrate.New(riverpgxv5.New(nil), nil)
	if err != nil {
		t.Fatal(err)
	}
	all := migrator.AllVersions()
	if known := all[len(all)-1].Version; known != covered {
		t.Errorf("River knows migrations up to %d, the goose migrations hold up to %d; run make river-migration first=%d last=%d",
			known, covered, covered+1, known)
	}
}

type pingArgs struct{}

func (pingArgs) Kind() string { return "ping" }

type pingWorker struct {
	river.WorkerDefaults[pingArgs]
}

func (pingWorker) Work(context.Context, *river.Job[pingArgs]) error { return nil }

// TestRiverRunsOnGooseSchema works one job through a River client on a
// database migrated by goose alone.
func TestRiverRunsOnGooseSchema(t *testing.T) {
	db := New(t)
	workers := river.NewWorkers()
	river.AddWorker(workers, pingWorker{})
	client, err := river.NewClient(riverpgxv5.New(db.Pool), &river.Config{
		Queues:  map[string]river.QueueConfig{river.QueueDefault: {MaxWorkers: 1}},
		Workers: workers,
	})
	if err != nil {
		t.Fatal(err)
	}
	completed, cancel := client.Subscribe(river.EventKindJobCompleted)
	defer cancel()
	ctx, stop := context.WithTimeout(t.Context(), 30*time.Second)
	defer stop()
	if err := client.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := client.Stop(context.WithoutCancel(ctx)); err != nil {
			t.Errorf("stop: %v", err)
		}
	}()

	if _, err := client.Insert(ctx, pingArgs{}, nil); err != nil {
		t.Fatal(err)
	}
	select {
	case ev := <-completed:
		if ev.Job.Kind != "ping" {
			t.Errorf("completed job kind = %q, want ping", ev.Job.Kind)
		}
	case <-ctx.Done():
		t.Fatal("the job did not complete")
	}
}

type flakyArgs struct{}

func (flakyArgs) Kind() string { return "flaky" }

// flakyWorker fails a job's first attempt, asking for a retry after
// retryAfter by the app's clock, and reports the start of every attempt.
type flakyWorker struct {
	river.WorkerDefaults[flakyArgs]
	retryAfter time.Duration
	attempts   chan time.Time
}

func (w flakyWorker) Work(_ context.Context, job *river.Job[flakyArgs]) error {
	w.attempts <- time.Now()
	if job.Attempt == 1 {
		return errors.New("first attempt fails")
	}
	return nil
}

func (w flakyWorker) NextRetry(*river.Job[flakyArgs]) time.Time {
	return time.Now().Add(w.retryAfter)
}

// TestJobsRunWhateverTheAppClock runs jobs while the database's clock is
// off from the app's by half an hour either way. The app writes River's
// schedule times and the database claims jobs, so without one clock for
// both a new job waits out the offset (P11: the podman VM's clock lagged
// the Mac's after sleep, and imports stayed queued).
func TestJobsRunWhateverTheAppClock(t *testing.T) {
	db := New(t)
	// Every unqualified now() on a connection that puts this schema before
	// pg_catalog reads the skewed clock: River's queries, and triggers.
	if _, err := db.Pool.Exec(t.Context(), "CREATE SCHEMA clockskew"); err != nil {
		t.Fatal(err)
	}
	const retryAfter = 300 * time.Millisecond
	for _, offset := range []time.Duration{-30 * time.Minute, 30 * time.Minute} {
		t.Run(fmt.Sprintf("database clock %v", offset), func(t *testing.T) {
			_, err := db.Pool.Exec(t.Context(), fmt.Sprintf(
				"CREATE OR REPLACE FUNCTION clockskew.now() RETURNS timestamptz LANGUAGE sql STABLE AS $$ SELECT pg_catalog.now() + interval '%d seconds' $$",
				int(offset.Seconds())))
			if err != nil {
				t.Fatal(err)
			}
			cfg, err := pgxpool.ParseConfig(db.URL)
			if err != nil {
				t.Fatal(err)
			}
			cfg.ConnConfig.RuntimeParams["search_path"] = "clockskew, pg_catalog, public"
			pool, err := pgxpool.NewWithConfig(t.Context(), cfg)
			if err != nil {
				t.Fatal(err)
			}
			defer pool.Close()

			worker := flakyWorker{retryAfter: retryAfter, attempts: make(chan time.Time, 4)}
			workers := river.NewWorkers()
			river.AddWorker(workers, pingWorker{})
			river.AddWorker(workers, worker)
			client, err := river.NewClient(riverpgxv5.New(pool), &river.Config{
				Queues:            map[string]river.QueueConfig{river.QueueDefault: {MaxWorkers: 2}},
				Workers:           workers,
				FetchCooldown:     20 * time.Millisecond,
				FetchPollInterval: 50 * time.Millisecond,
			})
			if err != nil {
				t.Fatal(err)
			}
			completed, cancel := client.Subscribe(river.EventKindJobCompleted)
			defer cancel()
			ctx, stop := context.WithTimeout(t.Context(), 20*time.Second)
			defer stop()
			if err := client.Start(ctx); err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := client.Stop(context.WithoutCancel(ctx)); err != nil {
					t.Errorf("stop: %v", err)
				}
			}()

			inserted := time.Now()
			if _, err := client.Insert(ctx, pingArgs{}, nil); err != nil {
				t.Fatal(err)
			}
			select {
			case <-completed:
				if waited := time.Since(inserted); waited > 5*time.Second {
					t.Errorf("a new job ran after %v, want at once", waited)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("a new job did not run within 5s")
			}

			if _, err := client.Insert(ctx, flakyArgs{}, nil); err != nil {
				t.Fatal(err)
			}
			var first, second time.Time
			for _, at := range []*time.Time{&first, &second} {
				select {
				case *at = <-worker.attempts:
				case <-time.After(10 * time.Second):
					t.Fatal("a failed job was not retried within 10s")
				}
			}
			if gap := second.Sub(first); gap < retryAfter {
				t.Errorf("retried %v after the first attempt, before the %v it asked for", gap, retryAfter)
			}
		})
	}
}
