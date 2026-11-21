//go:build integration

package search

import (
	"testing"

	"github.com/rokumate/ronote/internal/ir"
	"github.com/rokumate/ronote/internal/store"
)

// The restore drill's search (docs/adr/0015-operations.md): on a restored
// library it finds the newest chunk by a word of its own, and with nothing
// indexed it has nothing to search, which is no failure.
func TestCheckSearch(t *testing.T) {
	testDB.Reset(t)
	st := store.New(testDB.Pool)
	if hits, searched, err := CheckSearch(t.Context(), st); err != nil || searched || hits != 0 {
		t.Fatalf("CheckSearch with nothing indexed = %d hits, searched %v, %v; want nothing searched", hits, searched, err)
	}
	indexed(t, st, newUser(t, st), codeDoc(map[string]string{"A cancel": "ctx, cancel := context.WithCancel(ctx)"}))
	hits, searched, err := CheckSearch(t.Context(), st)
	if err != nil || !searched || hits < 1 {
		t.Errorf("CheckSearch on an indexed library = %d hits, searched %v, %v; want at least one hit", hits, searched, err)
	}
}

// Prose is searched as English, which drops stopwords: a chunk that opens
// with "This" or "with" must still be found by a word search keeps.
func TestCheckSearchFindsProseThatOpensWithStopwords(t *testing.T) {
	testDB.Reset(t)
	st := store.New(testDB.Pool)
	doc := &ir.Doc{IRVersion: ir.Version, Title: "Loops", Blocks: []ir.Block{
		{ID: "p00000000001", Kind: ir.KindParagraph, Content: []ir.Inline{{Text: "This with that: loops iterate over sequences."}}},
	}}
	indexed(t, st, newUser(t, st), doc)
	hits, searched, err := CheckSearch(t.Context(), st)
	if err != nil || !searched || hits < 1 {
		t.Errorf("CheckSearch on prose opening with stopwords = %d hits, searched %v, %v; want at least one hit", hits, searched, err)
	}
}
