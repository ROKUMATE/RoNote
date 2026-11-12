//go:build integration

package store

import (
	"crypto/sha256"
	"slices"
	"testing"
)

func someChunks(texts ...string) []NewChunk {
	var cs []NewChunk
	for _, text := range texts {
		cs = append(cs, NewChunk{
			BlockIDs: []string{"b1", "b2"}, HeadingPath: []string{"Guide"}, Kind: "prose", Text: text,
			Headings: "Guide", Prose: text, Idents: "'http.servemux':1A 'servemux':2A",
		})
	}
	return cs
}

func TestReplaceChunks(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()
	doc := f.document(t, f.ada)
	count := func() int {
		t.Helper()
		n, err := f.s.CountChunks(ctx, f.ada.ID, doc)
		if err != nil {
			t.Fatal(err)
		}
		return n
	}

	if err := f.s.ReplaceChunks(ctx, f.ada.ID, doc, someChunks("one", "two", "three")); err != nil {
		t.Fatal(err)
	}
	if err := f.s.ReplaceChunks(ctx, f.ada.ID, doc, someChunks("four", "five")); err != nil {
		t.Fatal(err)
	}
	if n := count(); n != 2 {
		t.Errorf("%d chunks after replacing 3 with 2, want 2", n)
	}
	var hits int
	if err := testDB.Pool.QueryRow(ctx, "SELECT count(*) FROM chunks WHERE tsv @@ 'http.servemux'::tsquery").Scan(&hits); err != nil || hits != 2 {
		t.Errorf("identifier hits = %d (%v), want 2", hits, err)
	}
	// Another owner's id with Ada's document is refused by the composite key.
	if err := f.s.ReplaceChunks(ctx, f.grace.ID, doc, someChunks("x")); err == nil {
		t.Error("chunks for Ada's document were stored under Grace")
	}

	if _, err := testDB.Pool.Exec(ctx, "DELETE FROM documents WHERE id = $1", doc); err != nil {
		t.Fatal(err)
	}
	if n := count(); n != 0 {
		t.Errorf("%d chunks left after their document went, want 0", n)
	}
}

func TestChunksGoWithTheirUser(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()
	doc := f.document(t, f.ada)
	if err := f.s.ReplaceChunks(ctx, f.ada.ID, doc, someChunks("one")); err != nil {
		t.Fatal(err)
	}

	if err := f.s.DeleteUser(ctx, f.ada.ID); err != nil {
		t.Fatal(err)
	}

	var left int
	if err := testDB.Pool.QueryRow(ctx, "SELECT count(*) FROM chunks").Scan(&left); err != nil || left != 0 {
		t.Errorf("%d chunks left after their user went (%v), want 0", left, err)
	}
}

func TestEmbeddingCache(t *testing.T) {
	testDB.Reset(t)
	s := New(testDB.Pool)
	ctx := t.Context()
	vec := make([]float32, 1024)
	vec[0], vec[1], vec[1023] = 0.5, -0.25, 1
	hit, miss := sha256.Sum256([]byte("kept")), sha256.Sum256([]byte("not kept"))

	if err := s.CacheEmbedding(ctx, "m1", hit[:], vec); err != nil {
		t.Fatal(err)
	}
	// A second write for the same text keeps the first.
	other := slices.Clone(vec)
	other[0] = 0.75
	if err := s.CacheEmbedding(ctx, "m1", hit[:], other); err != nil {
		t.Fatal(err)
	}

	got, err := s.CachedEmbeddings(ctx, "m1", [][]byte{hit[:], miss[:]})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || !slices.Equal(got[hit], vec) {
		t.Errorf("cached = %d entries, first %v; want the kept vector alone", len(got), got[hit][:2])
	}
	if other, err := s.CachedEmbeddings(ctx, "m2", [][]byte{hit[:]}); err != nil || len(other) != 0 {
		t.Errorf("another model's cache = %v, %v; want nothing", other, err)
	}
}
