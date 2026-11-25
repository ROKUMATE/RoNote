//go:build integration

package search

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"testing"

	"github.com/rokumate/ronote/internal/embed"
	"github.com/rokumate/ronote/internal/store"
)

func TestEmbedCached(t *testing.T) {
	testDB.Reset(t)
	st := store.New(testDB.Pool)
	e := &counting{}

	first, err := EmbedCached(t.Context(), st, e, []string{"alpha", "beta", "alpha"})
	if err != nil {
		t.Fatal(err)
	}
	if first.Hits != 0 || first.Sent != 2 || !slices.Equal(e.sent, []string{"alpha", "beta"}) {
		t.Errorf("first: %d hits, %d sent %q; want 0 hits, alpha and beta sent once", first.Hits, first.Sent, e.sent)
	}

	e.sent = nil
	second, err := EmbedCached(t.Context(), st, e, []string{"beta", "gamma", "alpha"})
	if err != nil {
		t.Fatal(err)
	}
	if second.Hits != 2 || second.Sent != 1 || !slices.Equal(e.sent, []string{"gamma"}) {
		t.Errorf("second: %d hits, %d sent %q; want 2 hits, gamma alone sent", second.Hits, second.Sent, e.sent)
	}
	// What the cache gives back is the vector, as close as half precision keeps it.
	want := embed.FakeVector("beta")
	for i, x := range second.Vectors[0] {
		if math.Abs(float64(x-want[i])) > 1e-3 {
			t.Fatalf("cached beta[%d] = %v, want %v", i, x, want[i])
		}
	}
}

// timesOut cancels its attempt's context as its call after the kth starts,
// as an index job's timeout would in the middle of a batch.
type timesOut struct {
	counting
	k      int
	calls  int
	cancel context.CancelFunc
}

func (e *timesOut) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	e.calls++
	if e.calls > e.k {
		e.cancel()
		return nil, ctx.Err()
	}
	return e.counting.Embed(ctx, texts)
}

// TestEmbedCachedKeepsEachBatch: an attempt cut short keeps the batches it
// finished, so the retry sends only the rest.
func TestEmbedCachedKeepsEachBatch(t *testing.T) {
	testDB.Reset(t)
	st := store.New(testDB.Pool)
	const k = 2
	texts := make([]string, k*embed.BatchSize+3)
	for i := range texts {
		texts[i] = fmt.Sprintf("text %d", i)
	}

	ctx, cancel := context.WithCancel(t.Context())
	cut := &timesOut{k: k, cancel: cancel}
	if _, err := EmbedCached(ctx, st, cut, texts); !errors.Is(err, context.Canceled) {
		t.Fatalf("the cut attempt = %v, want context.Canceled", err)
	}

	e := &counting{}
	again, err := EmbedCached(t.Context(), st, e, texts)
	if err != nil {
		t.Fatal(err)
	}
	if again.Hits != k*embed.BatchSize || !slices.Equal(e.sent, texts[k*embed.BatchSize:]) {
		t.Errorf("the retry: %d hits, sent %q; want the first %d kept and only the rest sent", again.Hits, e.sent, k*embed.BatchSize)
	}
}
