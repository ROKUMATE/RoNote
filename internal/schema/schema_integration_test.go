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

func version(t *testing.T) int64 {
	t.Helper()
	var v int64
	if err := testDB.Pool.QueryRow(t.Context(), `SELECT max(version_id) FROM goose_db_version WHERE is_applied`).Scan(&v); err != nil {
		t.Fatal(err)
	}
	return v
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
