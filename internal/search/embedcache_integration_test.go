//go:build integration

package search

import (
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
