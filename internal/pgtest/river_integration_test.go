//go:build integration

package pgtest

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"
	"time"

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
