// Package schema applies the embedded migrations, and checks a database's
// schema against the newest one this build knows
// (docs/adr/0014-production-topology.md).
package schema

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/rokumate/ronote/migrations"
)

// Provider is goose over the embedded migrations, on db.
func Provider(db *sql.DB) (*goose.Provider, error) {
	return goose.NewProvider(goose.DialectPostgres, db, migrations.FS)
}

// Migrate applies every embedded migration the database lacks, and returns
// the version it is then at.
func Migrate(ctx context.Context, pool *pgxpool.Pool) (int64, error) {
	return withProvider(pool, func(p *goose.Provider) (int64, error) {
		if _, err := p.Up(ctx); err != nil {
			return 0, fmt.Errorf("migrate up: %w", err)
		}
		return p.GetDBVersion(ctx)
	})
}

// Check fails when the database's schema is newer than the newest migration
// this build embeds: code that doesn't know a column or a constraint
// mustn't write around it. An older schema passes, since migrations run
// before the api and worker start.
func Check(ctx context.Context, pool *pgxpool.Pool) error {
	_, err := withProvider(pool, func(p *goose.Provider) (int64, error) {
		current, err := p.GetDBVersion(ctx)
		if err != nil {
			return 0, fmt.Errorf("read the schema version: %w", err)
		}
		sources := p.ListSources()
		newest := sources[len(sources)-1].Version
		if current > newest {
			return 0, fmt.Errorf("the database's schema is at version %d, newer than this build's newest migration, %d: run a build that knows it", current, newest)
		}
		return current, nil
	})
	return err
}

func withProvider(pool *pgxpool.Pool, f func(*goose.Provider) (int64, error)) (version int64, err error) {
	db := stdlib.OpenDBFromPool(pool)
	defer func() { err = errors.Join(err, db.Close()) }()
	p, err := Provider(db)
	if err != nil {
		return 0, fmt.Errorf("load migrations: %w", err)
	}
	return f(p)
}
