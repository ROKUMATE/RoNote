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

// Status is a database's schema version against the newest migration this
// build embeds.
type Status struct {
	Current, Newest int64
}

// Behind reports a database that needs this build's migrations.
func (s Status) Behind() bool { return s.Current < s.Newest }

// Ahead reports a database a newer build migrated.
func (s Status) Ahead() bool { return s.Current > s.Newest }

// Versions reads the database's schema version and this build's newest.
func Versions(ctx context.Context, pool *pgxpool.Pool) (Status, error) {
	var s Status
	_, err := withProvider(pool, func(p *goose.Provider) (int64, error) {
		current, err := p.GetDBVersion(ctx)
		if err != nil {
			return 0, fmt.Errorf("read the schema version: %w", err)
		}
		sources := p.ListSources()
		s = Status{Current: current, Newest: sources[len(sources)-1].Version}
		return current, nil
	})
	return s, err
}

// Check fails when the database's schema is newer than the newest migration
// this build embeds: code that doesn't know a column or a constraint
// mustn't write around it. An older schema passes, since migrations run
// before the api and worker start.
func Check(ctx context.Context, pool *pgxpool.Pool) error {
	s, err := Versions(ctx, pool)
	if err != nil {
		return err
	}
	if s.Ahead() {
		return fmt.Errorf("the database's schema is at version %d, newer than this build's newest migration, %d: run a build that knows it", s.Current, s.Newest)
	}
	return nil
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
