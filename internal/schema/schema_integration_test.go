//go:build integration

package schema_test

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"github.com/rokumate/ronote/internal/pgtest"
	"github.com/rokumate/ronote/internal/schema"
)

var testDB *pgtest.DB

func TestMain(m *testing.M) { pgtest.Main(m, &testDB) }

func TestMigrateIsANoOpOnAMigratedDatabase(t *testing.T) {
	before := version(t)
	got, err := schema.Migrate(t.Context(), testDB.Pool)
	if err != nil {
		t.Fatal(err)
	}
	if got != before || got == 0 {
		t.Errorf("Migrate = version %d, want %d again", got, before)
	}
}

// A build older than the database refuses it, naming both versions; the
// database's own version passes.
func TestCheckRefusesANewerSchema(t *testing.T) {
	ctx := t.Context()
	if err := schema.Check(ctx, testDB.Pool); err != nil {
		t.Fatalf("Check on the newest schema: %v", err)
	}
	newest := version(t)
	future := newest + 1
	if _, err := testDB.Pool.Exec(ctx, `INSERT INTO goose_db_version (version_id, is_applied) VALUES ($1, true)`, future); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		// t.Context is done by the time cleanups run.
		if _, err := testDB.Pool.Exec(context.Background(), `DELETE FROM goose_db_version WHERE version_id = $1`, future); err != nil {
			t.Error(err)
		}
	})
	err := schema.Check(ctx, testDB.Pool)
	if err == nil {
		t.Fatal("Check passed a schema newer than the build")
	}
	for _, want := range []string{"version " + itoa(future), itoa(newest)} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q doesn't say %q", err, want)
		}
	}
}

// Versions tells the database's version from the build's newest: equal on
// the migrated database, behind once its newest migration is undone, and
// ahead once one the build doesn't know is recorded.
func TestVersionsSaysBehindOrAhead(t *testing.T) {
	ctx := t.Context()
	newest := version(t)
	got, err := schema.Versions(ctx, testDB.Pool)
	if err != nil {
		t.Fatal(err)
	}
	if got != (schema.Status{Current: newest, Newest: newest}) || got.Behind() || got.Ahead() {
		t.Errorf("Versions on the migrated database = %+v, want %d and %d", got, newest, newest)
	}

	for _, tc := range []struct {
		name          string
		change, undo  string
		current       int64
		behind, ahead bool
	}{
		{"behind", `DELETE FROM goose_db_version WHERE version_id = $1`, `INSERT INTO goose_db_version (version_id, is_applied) VALUES ($1, true)`, newest - 1, true, false},
		{"ahead", `INSERT INTO goose_db_version (version_id, is_applied) VALUES ($1 + 1, true)`, `DELETE FROM goose_db_version WHERE version_id = $1 + 1`, newest + 1, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := testDB.Pool.Exec(ctx, tc.change, newest); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				// The test's context is done by the time cleanups run.
				if _, err := testDB.Pool.Exec(context.WithoutCancel(ctx), tc.undo, newest); err != nil {
					t.Error(err)
				}
			})
			got, err := schema.Versions(ctx, testDB.Pool)
			if err != nil {
				t.Fatal(err)
			}
			want := schema.Status{Current: tc.current, Newest: newest}
			if got != want || got.Behind() != tc.behind || got.Ahead() != tc.ahead {
				t.Errorf("Versions = %+v (behind %v, ahead %v), want %+v (behind %v, ahead %v)",
					got, got.Behind(), got.Ahead(), want, tc.behind, tc.ahead)
			}
		})
	}
}

func version(t *testing.T) int64 {
	t.Helper()
	var v int64
	if err := testDB.Pool.QueryRow(t.Context(), `SELECT max(version_id) FROM goose_db_version WHERE is_applied`).Scan(&v); err != nil {
		t.Fatal(err)
	}
	return v
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
