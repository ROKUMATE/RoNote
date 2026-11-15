//go:build integration

package search

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rokumate/ronote/internal/embed"
	"github.com/rokumate/ronote/internal/ir"
	"github.com/rokumate/ronote/internal/store"
)

// TestSearchFindsPDFBlocks indexes a document made from a PDF like any
// other, and search finds a code block of it: the Think Python chapter's
// IR, as docling and pdftext made it from the recorded fixture.
func TestSearchFindsPDFBlocks(t *testing.T) {
	testDB.Reset(t)
	ctx := t.Context()
	st := store.New(testDB.Pool)
	ada := newUser(t, st)
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "pdf", "think-python-iteration.ir.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc ir.Doc
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	src, snap, err := st.SavePDFSource(ctx, store.PDFSource{OwnerID: ada, SHA256: doc.Source.BodySHA256, BlobKey: "pdf/x", ConvertedAt: doc.Source.FetchedAt})
	if err != nil {
		t.Fatal(err)
	}
	ext, err := st.SaveExtraction(ctx, snap, &doc)
	if err != nil {
		t.Fatal(err)
	}
	id, _, err := st.CreateDocumentIfAbsent(ctx, store.NewDocument{OwnerID: ada, SourceID: src, ExtractionID: ext.ID, Content: ext.Doc})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewIndexer(st, embed.Fake{}, discard).Index(ctx, ada, id); err != nil {
		t.Fatal(err)
	}

	s := NewSearcher(st, embed.Fake{}, discard)
	for _, mode := range []Mode{WordsOnly, Hybrid} {
		res, err := s.Search(ctx, Request{Owner: ada, Query: "Blastoff", Limit: 5, Mode: mode})
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Hits) == 0 {
			t.Fatalf("mode %d: no hits for Blastoff", mode)
		}
		top := res.Hits[0]
		if top.DocumentID != id || !strings.Contains(snippetText(top), "Blastoff") {
			t.Errorf("mode %d: top hit %q in %s, want the countdown code of the PDF's document", mode, snippetText(top), top.DocumentID)
		}
	}
}
