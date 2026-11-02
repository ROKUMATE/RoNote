// Package db opens the Postgres connection pool.
package db

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Pool limits. They are set here rather than read from the URL so every
// environment runs with the same, reviewed values.
const (
	maxConns          = 10
	minConns          = 0
	maxConnLifetime   = time.Hour
	maxConnIdleTime   = 10 * time.Minute
	healthCheckPeriod = 30 * time.Second
	connectTimeout    = 5 * time.Second
)

// Startup retry policy: the first retry comes after firstRetryDelay, each
// later one waits twice as long up to maxRetryDelay, and Open gives up after
// startupTimeout.
const (
	startupTimeout  = 30 * time.Second
	firstRetryDelay = 100 * time.Millisecond
	maxRetryDelay   = 2 * time.Second
)

// Open creates a pool for url and waits until the database answers a ping,
// retrying with backoff for at most 30s.
func Open(ctx context.Context, url string, logger *slog.Logger) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("parse database URL: %w", err)
	}
	cfg.MaxConns = maxConns
	cfg.MinConns = minConns
	cfg.MaxConnLifetime = maxConnLifetime
	cfg.MaxConnIdleTime = maxConnIdleTime
	cfg.HealthCheckPeriod = healthCheckPeriod
	cfg.ConnConfig.ConnectTimeout = connectTimeout

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, startupTimeout)
	defer cancel()
	if err := waitUntil(ctx, pool.Ping, logger); err != nil {
		pool.Close()
		return nil, err
	}
	return pool, nil
}

// waitUntil calls ping until it succeeds or ctx ends, backing off between
// attempts.
func waitUntil(ctx context.Context, ping func(context.Context) error, logger *slog.Logger) error {
	delay := firstRetryDelay
	for attempt := 1; ; attempt++ {
		err := ping(ctx)
		if err == nil {
			return nil
		}
		logger.WarnContext(ctx, "database not ready", "attempt", attempt, "error", err)
		select {
		case <-ctx.Done():
			return fmt.Errorf("database not reachable after %d attempts: %w", attempt, err)
		case <-time.After(delay):
		}
		delay = min(delay*2, maxRetryDelay)
	}
}
