// Command worker runs background jobs: the import pipeline on the River
// queue "imports".
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
	"github.com/rokumate/ronote/internal/fetch"
	"github.com/rokumate/ronote/internal/importer"
	"github.com/rokumate/ronote/internal/store"
)

// Running jobs get stopTimeout to finish when the worker stops, then
// cancelTimeout after their context is cancelled.
const (
	stopTimeout   = 30 * time.Second
	cancelTimeout = 5 * time.Second
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
	client, err := importer.NewClient(pool, worker, importer.ClientOptions{Logger: logger})
	if err != nil {
		return err
	}
	// The client gets its own context: stopping is done below, so that
	// running jobs can finish first.
	if err := client.Start(context.WithoutCancel(ctx)); err != nil {
		return fmt.Errorf("start: %w", err)
	}
	logger.Info("worker started", "queue", importer.Queue, "env", cfg.Env, "source_ttl", cfg.SourceTTL.String())

	<-ctx.Done()
	stop()
	logger.Info("worker stopping", "timeout_ms", stopTimeout.Milliseconds())
	stopCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), stopTimeout)
	defer cancel()
	if err := client.Stop(stopCtx); err != nil {
		if !errors.Is(err, context.DeadlineExceeded) {
			return fmt.Errorf("stop: %w", err)
		}
		cancelCtx, cancelDone := context.WithTimeout(context.WithoutCancel(ctx), cancelTimeout)
		defer cancelDone()
		if err := client.StopAndCancel(cancelCtx); err != nil {
			return fmt.Errorf("stop and cancel: %w", err)
		}
	}
	logger.Info("worker stopped")
	return nil
}
