// Command api serves the RoNote HTTP API.
package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/google/uuid"

	"github.com/rokumate/ronote/internal/auth"
	"github.com/rokumate/ronote/internal/blob/s3blob"
	"github.com/rokumate/ronote/internal/chat"
	"github.com/rokumate/ronote/internal/chat/anthropic"
	"github.com/rokumate/ronote/internal/config"
	"github.com/rokumate/ronote/internal/db"
	"github.com/rokumate/ronote/internal/embed"
	"github.com/rokumate/ronote/internal/events"
	"github.com/rokumate/ronote/internal/httpapi"
	"github.com/rokumate/ronote/internal/importer"
	"github.com/rokumate/ronote/internal/jobs"
	"github.com/rokumate/ronote/internal/jobs/riverjobs"
	"github.com/rokumate/ronote/internal/search"
	"github.com/rokumate/ronote/internal/store"
)

const shutdownTimeout = 10 * time.Second

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "api:", err)
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

	// The API only enqueues jobs; the worker process runs them. River keeps
	// a job's attempt limit with it, so the kinds are declared here too.
	queue, err := riverjobs.New(pool, riverjobs.Options{
		Logger: logger,
		Kinds:  map[string]jobs.Options{importer.Kind: importer.JobOptions, search.JobKind: search.JobOptions},
	})
	if err != nil {
		return err
	}
	// Saves, reverts and notes enqueue their document's indexing in their
	// own transaction.
	st := store.New(pool).OnChange(search.EnqueueOnChange(queue, cfg.Embed.Model))
	// One connection listens for import status changes; event streams share it.
	broker := events.NewBroker()
	listening := make(chan struct{})
	go func() {
		defer close(listening)
		broker.Listen(ctx, pool, logger)
	}()
	searcher := search.NewSearcher(st, embed.NewClient(cfg.Embed.URL, cfg.Embed.Model), logger)
	chats, err := chat.NewService(chat.Options{
		Store: st, Searcher: searcher,
		Model:     anthropic.New(cfg.Chat.URL, cfg.Chat.APIKey, cfg.Chat.Model),
		ModelName: cfg.Chat.Model, Enabled: cfg.Chat.Enabled,
		Limits: chat.Limits{MessagesPerDay: cfg.Limits.ChatMessagesPerDay, TokensPerDay: cfg.Limits.ChatTokensPerDay},
		Logger: logger,
	})
	if err != nil {
		return err
	}

	streamsDone := make(chan struct{})

	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp", cfg.HTTPAddr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	imports := importer.NewService(st, queue, importer.Quota{
		ImportsPerDay: cfg.Limits.ImportsPerDay, Documents: cfg.Limits.Documents,
	}, s3blob.New(s3blob.Config(cfg.Blob)))
	srv := httpapi.NewServer(httpapi.New(httpapi.Options{
		Logger:    logger,
		DB:        pool,
		Backend:   backend{Store: st, Service: imports},
		Events:    broker,
		Done:      streamsDone,
		Accounts:  accounts{Store: st, imports: imports},
		Cookies:   auth.Cookies{Secure: cfg.Env.Deployed()},
		PublicURL: cfg.PublicURL,
		Limits:    httpapi.Limits{APIPerMinute: cfg.Limits.APIPerMinute, AuthPerMinute: cfg.Limits.AuthPerMinute},
		// Scores help tune search in dev and test; people don't need them.
		Searcher:   searcher,
		ShowScores: !cfg.Env.Deployed(),
		Chat:       chats,
		GitHub: auth.GitHub{
			ClientID:     cfg.GitHub.ClientID,
			ClientSecret: cfg.GitHub.ClientSecret,
			AuthorizeURL: cfg.GitHub.AuthorizeURL,
			TokenURL:     cfg.GitHub.TokenURL,
			APIURL:       cfg.GitHub.APIURL,
			RedirectURL:  cfg.PublicURL + "/api/v1/auth/github/callback",
		},
	}), logger)
	// Shutdown waits for open connections; ending the event streams lets it.
	srv.RegisterOnShutdown(func() { close(streamsDone) })

	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(ln) }()
	logger.Info("api listening", "addr", ln.Addr().String(), "env", cfg.Env)

	select {
	case err := <-serveErr:
		return fmt.Errorf("serve: %w", err)
	case <-ctx.Done():
	}
	stop()

	logger.Info("api shutting down", "timeout_ms", shutdownTimeout.Milliseconds())
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	// Answers outlive their streams: each is saved before the process ends.
	if err := chats.Shutdown(shutdownCtx); err != nil {
		logger.Warn("answers stopped at shutdown", "error", err)
	}
	if err := <-serveErr; !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serve: %w", err)
	}
	<-listening
	logger.Info("api stopped")
	return nil
}

// backend serves /api/v1 from the store and the import service.
type backend struct {
	*store.Store
	*importer.Service
}

// accounts are the store's, but deleting one also deletes its blobs.
type accounts struct {
	*store.Store
	imports *importer.Service
}

func (a accounts) DeleteUser(ctx context.Context, user uuid.UUID) error {
	return a.imports.DeleteUser(ctx, user)
}
