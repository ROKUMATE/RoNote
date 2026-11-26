// Command worker runs background jobs: imports of pages and PDFs, blob
// cleanup and indexing, through the jobs port on River
// (docs/adr/0009-jobs-port.md).
//
//	worker [run]                    run jobs
//	worker healthcheck              fail unless a running worker's database answered lately
//	worker check-config [--offline] check the configuration, and the blob store's
//	                                bucket unless --offline, starting nothing
//	worker convert-check            convert the PDF on stdin once, with the
//	                                import's docling client and no retries
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/rokumate/ronote/internal/blob/s3blob"
	"github.com/rokumate/ronote/internal/checkconfig"
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
		"check-config": func(args []string) error {
			return checkconfig.Run(context.Background(), args, os.Getenv, os.Stdout)
		},
		"healthcheck": cli.NoArgs(func() error {
			return health.Fresh(heartbeatFile, heartbeatStale, time.Now())
		}),
		"convert-check": cli.NoArgs(func() error {
			cfg, err := config.Load(os.Getenv)
			if err != nil {
				return err
			}
			return convertCheck(context.Background(), newConverter(cfg), os.Stdin, os.Stdout)
		}),
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "worker:", err)
		os.Exit(cli.ExitCode(err))
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
	if err := checkconfig.Bucket(ctx, cfg); err != nil {
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
	queue.Handle(importer.PDFKind, importer.NewPDFWorker(st, blobs, newConverter(cfg), docling.ServeVersion, logger).Handle, importer.PDFJobOptions)
	cleaner := importer.NewBlobs(st, blobs, queue, logger)
	queue.Handle(importer.DeleteBlobsKind, cleaner.HandleDelete, importer.BlobJobOptions)
	queue.Handle(importer.SweepBlobsKind, cleaner.HandleSweep, importer.BlobJobOptions)
	embedder := embed.NewClient(cfg.Embed.URL, cfg.Embed.Model)
	embedder.Threads = cfg.Embed.Threads
	indexer := search.NewIndexer(st, embedder, logger)
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

// newConverter is the docling and pdftext client PDF imports use; docling's
// own document timeout applies.
func newConverter(cfg config.Config) *docling.Client {
	return &docling.Client{
		Docling:   docling.Service{URL: cfg.PDF.DoclingURL, Key: cfg.PDF.DoclingKey},
		PDFText:   docling.Service{URL: cfg.PDF.TextURL, Key: cfg.PDF.TextKey},
		HTTP:      &http.Client{Timeout: 2 * time.Minute},
		PollEvery: time.Second,
	}
}

// convertCheck has docling convert the PDF read from in once, as an import
// does but with no job around it, so nothing retries: make
// prod-docling-curve times it by page count (scripts/real-images.sh). It
// prints "convert-check: ok"; a PDF docling refuses or gives up on is an
// error naming its code, such as pdf_conversion_timeout.
func convertCheck(ctx context.Context, c *docling.Client, in io.Reader, out io.Writer) error {
	pdf, err := io.ReadAll(io.LimitReader(in, importer.MaxPDFBytes+1))
	if err != nil {
		return err
	}
	if len(pdf) > importer.MaxPDFBytes {
		return fmt.Errorf("convert-check: the PDF is over %d bytes", importer.MaxPDFBytes)
	}
	if _, err := c.Convert(ctx, pdf, "convert-check.pdf"); err != nil {
		return fmt.Errorf("convert-check: %w", err)
	}
	_, err = fmt.Fprintln(out, "convert-check: ok")
	return err
}
