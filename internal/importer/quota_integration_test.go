//go:build integration

package importer

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/rokumate/ronote/internal/jobs"
	"github.com/rokumate/ronote/internal/jobs/riverjobs"
	"github.com/rokumate/ronote/internal/store"
)

// quotaService returns a Service within quota whose jobs never run, as the
// API's, for a new user.
func quotaService(t *testing.T, quota Quota) (*Service, *store.Store, uuid.UUID) {
	t.Helper()
	testDB.Reset(t)
	st := store.New(testDB.Pool)
	u, err := st.CreateUser(t.Context(), "ada@example.com", "Ada")
	if err != nil {
		t.Fatal(err)
	}
	queue, err := riverjobs.New(testDB.Pool, riverjobs.Options{Logger: discard, Kinds: map[string]jobs.Options{Kind: JobOptions}})
	if err != nil {
		t.Fatal(err)
	}
	return NewService(st, queue, quota), st, u.ID
}

func pageURL(n int) string { return fmt.Sprintf("https://docs.example.com/page-%d", n) }

func wantQuotaError(t *testing.T, err error, quota string, retry func(time.Duration) bool) {
	t.Helper()
	var over *QuotaError
	if !errors.As(err, &over) || over.Quota != quota || !retry(over.RetryAfter) {
		t.Fatalf("err = %v (%+v), want over the %s quota", err, over, quota)
	}
}

func TestImportsPerDayQuota(t *testing.T) {
	s, _, ada := quotaService(t, Quota{ImportsPerDay: 2, Documents: 100})
	ctx := t.Context()
	for i := range 2 {
		if _, err := s.Request(ctx, ada, pageURL(i)); err != nil {
			t.Fatalf("import %d: %v", i, err)
		}
	}

	_, err := s.Request(ctx, ada, pageURL(2))
	wantQuotaError(t, err, "imports per day", func(d time.Duration) bool { return d > 23*time.Hour && d <= 24*time.Hour })

	// A day after the first import, one more fits.
	if _, err := testDB.Pool.Exec(ctx,
		"UPDATE imports SET created_at = now() - interval '25 hours' WHERE canonical_url = $1", pageURL(0)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Request(ctx, ada, pageURL(2)); err != nil {
		t.Errorf("import a day later: %v, want it to fit", err)
	}
}

func TestDocumentsQuota(t *testing.T) {
	s, st, ada := quotaService(t, Quota{ImportsPerDay: 100, Documents: 2})
	ctx := t.Context()
	var first store.Import
	for i := range 2 {
		res, err := s.Request(ctx, ada, pageURL(i))
		if err != nil {
			t.Fatalf("import %d: %v", i, err)
		}
		if i == 0 {
			first = res.Import
		}
	}

	// Imports under way count: each may make a document.
	_, err := s.Request(ctx, ada, pageURL(2))
	wantQuotaError(t, err, "documents", func(d time.Duration) bool { return d == 0 })

	// A failed import makes none.
	if _, err := st.FailImport(ctx, ada, first.ID, "not_found"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Request(ctx, ada, pageURL(2)); err != nil {
		t.Errorf("import after one failed: %v, want it to fit", err)
	}
}

// TestQuotaHoldsUnderConcurrency sends many imports at once: the quota lock
// lets exactly as many through as fit.
func TestQuotaHoldsUnderConcurrency(t *testing.T) {
	const fits, tries = 3, 12
	s, _, ada := quotaService(t, Quota{ImportsPerDay: fits, Documents: 100})
	var wg sync.WaitGroup
	results := make(chan error, tries)
	for i := range tries {
		wg.Go(func() {
			_, err := s.Request(t.Context(), ada, pageURL(i))
			results <- err
		})
	}
	wg.Wait()
	close(results)
	ok, over := 0, 0
	for err := range results {
		var q *QuotaError
		switch {
		case err == nil:
			ok++
		case errors.As(err, &q):
			over++
		default:
			t.Errorf("import: %v", err)
		}
	}
	if ok != fits || over != tries-fits {
		t.Errorf("%d imports started and %d refused, want %d and %d", ok, over, fits, tries-fits)
	}
}

func TestQuotaIsPerUser(t *testing.T) {
	s, st, ada := quotaService(t, Quota{ImportsPerDay: 1, Documents: 1})
	ctx := t.Context()
	grace, err := st.CreateUser(ctx, "grace@example.com", "Grace")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Request(ctx, ada, pageURL(0)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Request(ctx, grace.ID, pageURL(0)); err != nil {
		t.Errorf("Grace's first import: %v, want Ada's usage not to count", err)
	}
}
