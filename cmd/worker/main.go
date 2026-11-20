// Command worker runs background jobs: imports of pages and PDFs, blob
// cleanup and indexing, through the jobs port on River
// (docs/adr/0009-jobs-port.md).
//
//	worker [run]         run jobs
//	worker healthcheck   fail unless a running worker's database answered lately
package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/rokumate/ronote/internal/blob/s3blob"
	"github.com/rokumate/ronote/internal/cli"
	"github.com/rokumate/ronote/internal/config"
	"github.com/rokumate/ronote/internal/db"
	"github.com/rokumate/ronote/internal/docling"
	"github.com/rokumate/ronote/internal/embed"
	"github.com/rokumate/ronote/internal/fetch"
	"github.com/rokumate/ronote/internal/health"
	"github.com/rokumate/ronote/internal/importer"
	"github.com/rokumate/ronote/internal/jobs/riverjobs"
	"github.com/rokumate/ronote/internal/schema"
	"github.com/rokumate/ronote/internal/search"
	"github.com/rokumate/ronote/internal/store"
	"github.com/rokumate/ronote/internal/version"
)

// backfillEvery spaces the backfill's index jobs.
const backfillEvery = 200 * time.Millisecond

// The worker serves no HTTP, so its health check reads a file it touches
// every heartbeatEvery while its database answers; three missed beats fail
// it (docs/adr/0014-production-topology.md).
const (
	heartbeatEvery = 30 * time.Second
	heartbeatStale = 3 * heartbeatEvery
)

var heartbeatFile = filepath.Join(os.TempDir(), "ronote-worker-alive")

func main() {
	err := cli.Run(os.Args[1:], "run", map[string]cli.Command{
		"run": cli.NoArgs(run),
		"healthcheck": cli.NoArgs(func() error {
			return health.Fresh(heartbeatFile, heartbeatStale, time.Now())
		}),
	})
	if err != nil {
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
	if err := schema.Check(ctx, pool); err != nil {
		return err
	}
	go health.Beat(ctx, heartbeatFile, heartbeatEvery, pool.Ping)

	queue, err := riverjobs.New(pool, riverjobs.Options{Logger: logger})
	if err != nil {
		return err
	}
	// A finished import's new document is indexed in the import's transaction.
	st := store.New(pool).OnChange(search.EnqueueOnChange(queue, cfg.Embed.Model))
	fetcher := fetch.New(fetch.Options{AllowPrivateNetworks: cfg.FetchAllowPrivateNetworks})
	worker := importer.NewWorker(st, fetcher, cfg.SourceTTL, logger)
	queue.Handle(importer.Kind, worker.Handle, importer.JobOptions)
	blobs := s3blob.New(s3blob.Config(cfg.Blob))
	converter := &docling.Client{
		Docling:   docling.Service{URL: cfg.PDF.DoclingURL, Key: cfg.PDF.DoclingKey},
		PDFText:   docling.Service{URL: cfg.PDF.TextURL, Key: cfg.PDF.TextKey},
		HTTP:      &http.Client{Timeout: 2 * time.Minute},
		PollEvery: time.Second,
	}
	queue.Handle(importer.PDFKind, importer.NewPDFWorker(st, blobs, converter, docling.ServeVersion, logger).Handle, importer.PDFJobOptions)
	cleaner := importer.NewBlobs(st, blobs, queue, logger)
	queue.Handle(importer.DeleteBlobsKind, cleaner.HandleDelete, importer.BlobJobOptions)
	queue.Handle(importer.SweepBlobsKind, cleaner.HandleSweep, importer.BlobJobOptions)
	indexer := search.NewIndexer(st, embed.NewClient(cfg.Embed.URL, cfg.Embed.Model), logger)
	queue.Handle(search.JobKind, indexer.Handle, search.JobOptions)
	logger.Info("worker started", "kinds", []string{importer.Kind, importer.PDFKind, importer.DeleteBlobsKind, importer.SweepBlobsKind, search.JobKind},
		"env", cfg.Env, "version", version.Version, "source_ttl", cfg.SourceTTL.String(), "embed_model", cfg.Embed.Model)

	// Leftover blobs are swept now, then daily.
	if err := cleaner.ScheduleSweep(ctx, 0); err != nil {
		return fmt.Errorf("schedule blob sweep: %w", err)
	}

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
