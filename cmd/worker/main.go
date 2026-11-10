// Command worker runs background jobs: imports, through the jobs port on
// River (docs/adr/0009-jobs-port.md).
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/rokumate/ronote/internal/config"
	"github.com/rokumate/ronote/internal/db"
	"github.com/rokumate/ronote/internal/fetch"
	"github.com/rokumate/ronote/internal/importer"
	"github.com/rokumate/ronote/internal/jobs/riverjobs"
	"github.com/rokumate/ronote/internal/store"
)

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

	fetcher := fetch.New(fetch.Options{AllowPrivateNetworks: cfg.FetchAllowPrivateNetworks})
	worker := importer.NewWorker(store.New(pool), fetcher, cfg.SourceTTL, logger)
	queue, err := riverjobs.New(pool, riverjobs.Options{Logger: logger})
	if err != nil {
		return err
	}
	queue.Handle(importer.Kind, worker.Handle, importer.JobOptions)
	logger.Info("worker started", "kind", importer.Kind, "env", cfg.Env, "source_ttl", cfg.SourceTTL.String())

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
