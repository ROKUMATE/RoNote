//go:build integration

package httpapi

import (
	"net/http"
	"testing"

	"github.com/rokumate/ronote/internal/db"
	"github.com/rokumate/ronote/internal/pgtest"
)

// TestReadyzFollowsDatabase stops its database on purpose, so it uses its
// own container instead of a shared one.
func TestReadyzFollowsDatabase(t *testing.T) {
	pg := pgtest.New(t)
	pool, err := db.Open(t.Context(), pg.URL, discardLogger)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	h := New(Options{Logger: discardLogger, DB: pool})

	steps := []struct {
		name       string
		before     func() error
		wantStatus int
	}{
		{name: "database up", wantStatus: http.StatusOK},
		{name: "database stopped", before: func() error { return pg.Stop(t.Context()) }, wantStatus: http.StatusServiceUnavailable},
	}

	for _, step := range steps {
		if step.before != nil {
			if err := step.before(); err != nil {
				t.Fatalf("%s: %v", step.name, err)
			}
		}
		rec := record(h, newRequest(t, http.MethodGet, "/readyz"))
		if rec.Code != step.wantStatus {
			t.Errorf("%s: status = %d, want %d (body %s)", step.name, rec.Code, step.wantStatus, rec.Body)
		}
		if step.wantStatus == http.StatusServiceUnavailable {
			if code := decodeError(t, rec).Code; code != "not_ready" {
				t.Errorf("%s: error code = %q, want not_ready", step.name, code)
			}
		}
	}
}
