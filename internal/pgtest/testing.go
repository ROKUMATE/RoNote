//go:build integration

package pgtest

import (
	"context"
	"fmt"
	"os"
	"testing"
)

// Main starts one DB for the package's tests, stores it in *db, runs the
// tests and closes the DB before exiting, whatever the tests did.
func Main(m *testing.M, db **DB) {
	d, err := Start(context.Background())
	if err != nil {
		fmt.Fprintln(os.Stderr, "pgtest:", err)
		os.Exit(1)
	}
	*db = d
	code := m.Run()
	if err := d.Close(); err != nil {
		fmt.Fprintln(os.Stderr, "pgtest:", err)
		code = 1
	}
	os.Exit(code)
}

// New starts a DB for one test and closes it when the test ends, even if it
// fails or panics.
func New(t testing.TB) *DB {
	t.Helper()
	d, err := Start(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := d.Close(); err != nil {
			t.Errorf("pgtest: %v", err)
		}
	})
	return d
}

// Reset empties every table except goose's version table, so a test starts
// from the migrated schema with no rows.
func (d *DB) Reset(t testing.TB) {
	t.Helper()
	_, err := d.Pool.Exec(t.Context(), `
		DO $$
		DECLARE
			tables text;
		BEGIN
			SELECT string_agg(format('%I.%I', schemaname, tablename), ', ') INTO tables
			FROM pg_tables
			WHERE schemaname = 'public' AND tablename <> 'goose_db_version';
			IF tables IS NOT NULL THEN
				EXECUTE 'TRUNCATE ' || tables || ' RESTART IDENTITY CASCADE';
			END IF;
		END $$`)
	if err != nil {
		t.Fatalf("pgtest: reset: %v", err)
	}
}
