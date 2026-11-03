// Package importer runs the import pipeline: fetch a page, or reuse the
// stored copy, extract it, and keep it as the owner's document. The API
// enqueues imports through Service and the worker process runs them with
// Worker, on the River queue "imports". docs/adr/0005-import-pipeline.md
// records the rules.
package importer

import (
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
)

const (
	// Queue is the River queue imports run on.
	Queue = "imports"
	// maxAttempts counts every try of a job, retries included.
	maxAttempts = 3
	jobTimeout  = 60 * time.Second
	maxWorkers  = 4
	// maxRetryAfter caps how long a server's Retry-After may delay a retry.
	maxRetryAfter = 10 * time.Minute
)

// Args are an import job's arguments. The owner travels with the import so
// every query the job runs is scoped to them.
type Args struct {
	ImportID uuid.UUID `json:"import_id"`
	OwnerID  uuid.UUID `json:"owner_id"`
}

// Kind names the job type.
func (Args) Kind() string { return "import" }

// InsertOpts puts imports on their queue with a cap of three tries.
func (Args) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: Queue, MaxAttempts: maxAttempts}
}

// ClientOptions tune a River client. Zero values mean River's defaults;
// tests shorten the retry delay and polling.
type ClientOptions struct {
	Logger            *slog.Logger
	RetryPolicy       river.ClientRetryPolicy
	FetchCooldown     time.Duration
	FetchPollInterval time.Duration
}

// NewClient returns a River client on pool. With a nil worker it only
// inserts jobs, as the API process does; with a worker it also works the
// imports queue, up to four jobs at a time.
func NewClient(pool *pgxpool.Pool, worker *Worker, opts ClientOptions) (*river.Client[pgx.Tx], error) {
	cfg := &river.Config{
		Logger:            opts.Logger,
		RetryPolicy:       opts.RetryPolicy,
		FetchCooldown:     opts.FetchCooldown,
		FetchPollInterval: opts.FetchPollInterval,
	}
	if worker != nil {
		workers := river.NewWorkers()
		river.AddWorker(workers, worker)
		cfg.Workers = workers
		cfg.Queues = map[string]river.QueueConfig{Queue: {MaxWorkers: maxWorkers}}
	}
	return river.NewClient(riverpgxv5.New(pool), cfg)
}
