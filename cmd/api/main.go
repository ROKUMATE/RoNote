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
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"

	"github.com/rokumate/ronote/internal/config"
	"github.com/rokumate/ronote/internal/db"
	"github.com/rokumate/ronote/internal/httpapi"
	"github.com/rokumate/ronote/internal/importer"
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

	st := store.New(pool)
	// The API only queues jobs; the worker process runs them.
	queue, err := importer.NewClient(pool, nil, importer.ClientOptions{Logger: logger})
	if err != nil {
		return err
	}
	var devUser uuid.UUID
	if cfg.DevUserEmail != "" {
		u, err := st.UpsertUserByEmail(ctx, cfg.DevUserEmail, devDisplayName(cfg.DevUserEmail))
		if err != nil {
			return fmt.Errorf("dev user: %w", err)
		}
		devUser = u.ID
		logger.Info("dev identity on: every /api/v1 request acts as this user", "email", u.Email)
	}

	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp", cfg.HTTPAddr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	srv := httpapi.NewServer(httpapi.New(httpapi.Options{
		Logger:  logger,
		DB:      pool,
		Backend: backend{Store: st, Service: importer.NewService(st, queue)},
		DevUser: devUser,
	}), logger)

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
	if err := <-serveErr; !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serve: %w", err)
	}
	logger.Info("api stopped")
	return nil
}

// backend serves /api/v1 from the store and the import service.
type backend struct {
	*store.Store
	*importer.Service
}

// devDisplayName is the part of an email before the @.
func devDisplayName(email string) string {
	name, _, _ := strings.Cut(email, "@")
	return name
}
