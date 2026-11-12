// Command worker runs background jobs: imports, through the jobs port on
// River (docs/adr/0009-jobs-port.md).
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rokumate/ronote/internal/config"
	"github.com/rokumate/ronote/internal/db"
	"github.com/rokumate/ronote/internal/embed"
	"github.com/rokumate/ronote/internal/fetch"
	"github.com/rokumate/ronote/internal/importer"
	"github.com/rokumate/ronote/internal/jobs/riverjobs"
	"github.com/rokumate/ronote/internal/search"
	"github.com/rokumate/ronote/internal/store"
)

// backfillEvery spaces the backfill's index jobs.
const backfillEvery = 200 * time.Millisecond

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "worker:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return err
	}
	logger := cfg.Logger(os.Stdout)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := db.Open(ctx, cfg.DatabaseURL, logger)
	if err != nil {
		return err
	}
	defer pool.Close()

	queue, err := riverjobs.New(pool, riverjobs.Options{Logger: logger})
	if err != nil {
		return err
	}
	// A finished import's new document is indexed in the import's transaction.
	st := store.New(pool).OnChange(search.EnqueueOnChange(queue, cfg.Embed.Model))
	fetcher := fetch.New(fetch.Options{AllowPrivateNetworks: cfg.FetchAllowPrivateNetworks})
	worker := importer.NewWorker(st, fetcher, cfg.SourceTTL, logger)
	queue.Handle(importer.Kind, worker.Handle, importer.JobOptions)
	indexer := search.NewIndexer(st, embed.NewClient(cfg.Embed.URL, cfg.Embed.Model), logger)
	queue.Handle(search.JobKind, indexer.Handle, search.JobOptions)
	logger.Info("worker started", "kinds", []string{importer.Kind, search.JobKind}, "env", cfg.Env,
		"source_ttl", cfg.SourceTTL.String(), "embed_model", cfg.Embed.Model)

	// Documents from before indexing, behind it, or of another model, five
	// a second.
	go func() {
		n, err := search.Backfill(ctx, st, queue, cfg.Embed.Model, backfillEvery)
		if err != nil && !errors.Is(err, context.Canceled) {
			logger.Error("index backfill", "error", err.Error(), "enqueued", n)
			return
		}
		logger.Info("index backfill enqueued", "documents", n)
	}()

	// A second signal while running jobs finish ends the process at once.
	go func() {
		<-ctx.Done()
		stop()
	}()
	// Run returns once ctx ends and the running jobs have finished.
	if err := queue.Run(ctx); err != nil {
		return fmt.Errorf("jobs: %w", err)
	}
	logger.Info("worker stopped")
	return nil
}
