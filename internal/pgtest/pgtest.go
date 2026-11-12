// Package pgtest starts throwaway, migrated Postgres containers for
// integration tests and cmd/eval. Ryuk is off on podman, so nothing reaps
// containers for us: Main, New and Start's callers always close what they
// start. Only tests and cmd/eval may link it (make lint-go checks the
// commands).
package pgtest

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
)

// Image is the dev stack's Postgres image; TestImageMatchesCompose keeps the
// two in sync.
const Image = "pgvector/pgvector:0.8.7-pg18-trixie@sha256:5895949dfcdf308eb0cd31c950c93c14895c77f33fbedfecf1fa511ab1d4bcd2"

// label marks containers started here so scripts/container.sh can spot leaks.
const label = "ronote.pgtest"

// DB is a running Postgres with every migration applied.
type DB struct {
	URL       string
	Pool      *pgxpool.Pool
	container *tcpostgres.PostgresContainer
}

// Start runs a Postgres container and applies the migrations. On error
// nothing is left running.
func Start(ctx context.Context) (*DB, error) {
	ctr, err := tcpostgres.Run(ctx, Image,
		tcpostgres.WithDatabase("ronote"),
		tcpostgres.WithUsername("ronote"),
		tcpostgres.WithPassword("ronote"),
		tcpostgres.BasicWaitStrategies(),
		testcontainers.WithLabels(map[string]string{label: "true"}),
	)
	if err != nil {
		err = fmt.Errorf("start postgres (is a container runtime up? on macOS run podman machine start): %w", err)
		return nil, errors.Join(err, testcontainers.TerminateContainer(ctr))
	}
	d := &DB{container: ctr}
	if err := d.init(ctx); err != nil {
		return nil, errors.Join(err, d.Close())
	}
	return d, nil
}

func (d *DB) init(ctx context.Context) error {
	url, err := d.container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		return fmt.Errorf("connection string: %w", err)
	}
	d.URL = url
	if d.Pool, err = pgxpool.New(ctx, url); err != nil {
		return fmt.Errorf("open pool: %w", err)
	}
	return migrate(ctx, d.Pool)
}

func migrate(ctx context.Context, pool *pgxpool.Pool) (err error) {
	sqlDB := stdlib.OpenDBFromPool(pool)
	defer func() { err = errors.Join(err, sqlDB.Close()) }()
	provider, err := goose.NewProvider(goose.DialectPostgres, sqlDB, os.DirFS(filepath.Join(repoRoot(), "migrations")))
	if err != nil {
		return fmt.Errorf("load migrations: %w", err)
	}
	if _, err := provider.Up(ctx); err != nil {
		return fmt.Errorf("migrate up: %w", err)
	}
	return nil
}

// MigrateTo moves d's schema up or down to goose version version, so a test
// can check a migration in both directions.
func (d *DB) MigrateTo(ctx context.Context, version int64) (err error) {
	sqlDB := stdlib.OpenDBFromPool(d.Pool)
	defer func() { err = errors.Join(err, sqlDB.Close()) }()
	provider, err := goose.NewProvider(goose.DialectPostgres, sqlDB, os.DirFS(filepath.Join(repoRoot(), "migrations")))
	if err != nil {
		return fmt.Errorf("load migrations: %w", err)
	}
	current, err := provider.GetDBVersion(ctx)
	if err != nil {
		return fmt.Errorf("read version: %w", err)
	}
	if version < current {
		_, err = provider.DownTo(ctx, version)
	} else {
		_, err = provider.UpTo(ctx, version)
	}
	return err
}

// Stop stops the container but keeps it for Close, so a test can watch the
// database go away.
func (d *DB) Stop(ctx context.Context) error {
	return d.container.Stop(ctx, nil)
}

// Close closes the pool and removes the container with its anonymous volumes.
func (d *DB) Close() error {
	if d.Pool != nil {
		d.Pool.Close()
	}
	return testcontainers.TerminateContainer(d.container)
}

func repoRoot() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..")
}
