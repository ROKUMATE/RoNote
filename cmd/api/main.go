// Command api serves the RoNote HTTP API.
//
//	api [serve]                  serve the API
//	api migrate                  apply the migrations the database lacks
//	api migrate status           print the schema's version against this build's;
//	                             exit 3 if the database is behind, 4 if ahead
//	api check-config [--offline] check the configuration, and the blob store's
//	                             bucket unless --offline, starting nothing
//	api healthcheck URL          fail unless URL (its /healthz) answers 200
//	api search-check             search the database's newest chunk's library by a
//	                             word of it, by words alone: the restore drill's
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/rokumate/ronote/internal/auth"
	"github.com/rokumate/ronote/internal/blob/s3blob"
	"github.com/rokumate/ronote/internal/chat"
	"github.com/rokumate/ronote/internal/chat/anthropic"
	"github.com/rokumate/ronote/internal/checkconfig"
	"github.com/rokumate/ronote/internal/cli"
	"github.com/rokumate/ronote/internal/config"
	"github.com/rokumate/ronote/internal/db"
	"github.com/rokumate/ronote/internal/embed"
	"github.com/rokumate/ronote/internal/events"
	"github.com/rokumate/ronote/internal/health"
	"github.com/rokumate/ronote/internal/httpapi"
	"github.com/rokumate/ronote/internal/importer"
	"github.com/rokumate/ronote/internal/jobs"
	"github.com/rokumate/ronote/internal/jobs/riverjobs"
	"github.com/rokumate/ronote/internal/schema"
	"github.com/rokumate/ronote/internal/search"
	"github.com/rokumate/ronote/internal/store"
	"github.com/rokumate/ronote/internal/version"
)

const (
	shutdownTimeout    = 10 * time.Second
	healthcheckTimeout = 2 * time.Second
)

func main() {
	err := cli.Run(os.Args[1:], "serve", map[string]cli.Command{
		"serve":   cli.NoArgs(run),
		"migrate": migrate,
		"check-config": func(args []string) error {
			return checkconfig.Run(context.Background(), args, os.Getenv, os.Stdout)
		},
		"healthcheck": healthcheck,
		"search-check": cli.NoArgs(func() error {
			return withDatabase(func(ctx context.Context, pool *pgxpool.Pool, _ *slog.Logger) error {
				hits, searched, err := search.CheckSearch(ctx, store.New(pool))
				switch {
				case err != nil:
					return err
				case !searched:
					fmt.Println("search: nothing is indexed to search")
				default:
					fmt.Printf("search: %d hits in the newest chunk's library\n", hits)
				}
				return nil
			})
		}),
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "api:", err)
		os.Exit(cli.ExitCode(err))
	}
}

// migrate applies the embedded migrations (docs/adr/0014-production-topology.md):
// production runs it once, as its own unit, before the api and the worker.
// `migrate status` compares the schema with this build's, for ronote-deploy
// and the restore drill (docs/adr/0015-operations.md).
func migrate(args []string) error {
	switch {
	case len(args) == 0:
		return withDatabase(func(ctx context.Context, pool *pgxpool.Pool, logger *slog.Logger) error {
			version, err := schema.Migrate(ctx, pool)
			if err != nil {
				return err
			}
			logger.Info("schema migrated", "version", version)
			return nil
		})
	case len(args) == 1 && args[0] == "status":
		return withDatabase(func(ctx context.Context, pool *pgxpool.Pool, _ *slog.Logger) error {
			s, err := schema.Versions(ctx, pool)
			if err != nil {
				return err
			}
			fmt.Printf("schema at version %d; this build's newest is %d\n", s.Current, s.Newest)
			switch {
			case s.Behind():
				return &cli.ExitError{Code: 3, Err: errors.New("the database is behind this build: run api migrate")}
			case s.Ahead():
				return &cli.ExitError{Code: 4, Err: errors.New("the database is ahead of this build: a newer build migrated it")}
			}
			return nil
		})
	}
	return fmt.Errorf("takes nothing or status, got %q", strings.Join(args, " "))
}

// withDatabase runs f on the database RONOTE_DATABASE_URL names, alone of
// the configuration: migrating needs nothing else.
func withDatabase(f func(context.Context, *pgxpool.Pool, *slog.Logger) error) error {
	url, err := config.DatabaseURL(os.Getenv)
	if err != nil {
		return err
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	pool, err := db.Open(ctx, url, logger)
	if err != nil {
		return err
	}
	defer pool.Close()
	return f(ctx, pool, logger)
}

// healthcheck is the container's health check: distroless has no curl.
func healthcheck(args []string) error {
	if len(args) != 1 {
		return errors.New("want one argument, the URL of /healthz")
	}
	// Its own process: the default client is no one else's.
	return health.Get(context.Background(), http.DefaultClient, args[0], healthcheckTimeout)
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

	// The API only enqueues jobs; the worker process runs them. River keeps
	// a job's attempt limit with it, so the kinds are declared here too.
	kinds := map[string]jobs.Options{search.JobKind: search.JobOptions}
	maps.Copy(kinds, importer.Kinds)
	queue, err := riverjobs.New(pool, riverjobs.Options{Logger: logger, Kinds: kinds})
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
		Model:     anthropic.New(cfg.Chat.URL, cfg.Chat.APIKey, cfg.Chat.Model, http.DefaultClient),
		ModelName: cfg.Chat.Model, Enabled: cfg.Chat.Enabled,
		Limits: chat.Limits{
			MessagesPerDay: cfg.Limits.ChatMessagesPerDay, TokensPerDay: cfg.Limits.ChatTokensPerDay,
			SpendPerDayMicroUSD: cfg.Chat.DailySpendMicroUSD,
		},
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
		Cookies:   auth.Cookies{Secure: cfg.SecureCookies()},
		PublicURL: cfg.PublicURL,
		Limits:    httpapi.Limits{APIPerMinute: cfg.Limits.APIPerMinute, AuthPerMinute: cfg.Limits.AuthPerMinute},
		// Behind Caddy, its X-Forwarded-For names the client.
		TrustedProxies: cfg.TrustedProxies,
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
	logger.Info("api listening", "addr", ln.Addr().String(), "env", cfg.Env, "version", version.Version)

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
