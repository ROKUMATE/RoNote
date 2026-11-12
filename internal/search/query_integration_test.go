//go:build integration

package search

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/rokumate/ronote/internal/embed"
	"github.com/rokumate/ronote/internal/ir"
	"github.com/rokumate/ronote/internal/store"
)

// codeDoc is a document of sections, each one code block, the heading
// naming what the block shows.
func codeDoc(sections map[string]string) *ir.Doc {
	doc := &ir.Doc{IRVersion: ir.Version, Title: "Snippets"}
	n := 0
	for name, code := range sections {
		n++
		id := strings.Repeat("0", 11) + string(rune('a'+n))
		doc.Blocks = append(doc.Blocks,
			ir.Block{ID: "h" + id[1:], Kind: ir.KindHeading, Level: 2, Content: []ir.Inline{{Text: name}}},
			ir.Block{ID: "c" + id[1:], Kind: ir.KindCode, Text: code},
		)
	}
	return doc
}

// indexed stores doc as owner's document and indexes it with the fake
// embedder.
func indexed(t *testing.T, st *store.Store, owner uuid.UUID, doc *ir.Doc) uuid.UUID {
	t.Helper()
	id := importDoc(t, st, owner, doc)
	if _, err := NewIndexer(st, embed.Fake{}, discard).Index(t.Context(), owner, id); err != nil {
		t.Fatal(err)
	}
	return id
}

// TestIdentifierQueries searches code by identifiers, among near misses
// that share some of their words, by words alone and hybrid.
func TestIdentifierQueries(t *testing.T) {
	testDB.Reset(t)
	st := store.New(testDB.Pool)
	ada := newUser(t, st)
	indexed(t, st, ada, codeDoc(map[string]string{
		"A mux":          "type Router struct{ mux *http.ServeMux }",
		"A server":       "srv := &http.Server{Addr: addr} // serve and mux elsewhere",
		"A group":        "g, ctx := errgroup.WithContext(ctx)",
		"A cancel":       "ctx, cancel := context.WithCancel(ctx)",
		"A missing file": "Error [ERR_MODULE_NOT_FOUND]: Cannot find module 'vite'",
		"Not found":      "404 not found: the module is gone",
		"A constructor":  "class Point:\n    def __init__(self, x):\n        self.x = x",
		"Initialize":     "def initialize(self): pass",
		"Skip the hooks": "git commit --no-verify -m wip",
		"Verify":         "git verify-commit HEAD",
		"An effect":      "useEffect(() => { document.title = title })",
		"State":          "const [state, setState] = useState(0)",
	}))
	s := NewSearcher(st, embed.Fake{}, discard)
	tests := []struct{ query, want string }{
		{"ServeMux", "mux *http.ServeMux"},
		{"http.ServeMux", "mux *http.ServeMux"},
		{"errgroup.WithContext", "errgroup.WithContext"},
		{"ERR_MODULE_NOT_FOUND", "[ERR_MODULE_NOT_FOUND]"},
		{"__init__", "def __init__(self, x)"},
		{"--no-verify", "commit --no-verify"},
		{"useEffect", "useEffect(() =>"},
	}
	for _, mode := range []Mode{WordsOnly, Hybrid} {
		for _, tt := range tests {
			res, err := s.Search(t.Context(), Request{Owner: ada, Query: tt.query, Limit: 5, Mode: mode})
			if err != nil {
				t.Fatal(err)
			}
			if len(res.Hits) == 0 {
				t.Errorf("mode %d, %q: no hits", mode, tt.query)
				continue
			}
			if top := snippetText(res.Hits[0]); !strings.Contains(top, tt.want) {
				t.Errorf("mode %d, %q: top hit %q, want the one with %q", mode, tt.query, top, tt.want)
			}
		}
	}
}

func snippetText(h Hit) string {
	var sb strings.Builder
	for _, s := range h.Snippet {
		sb.WriteString(s.Text)
	}
	return sb.String()
}

// TestOwnerIsolation gives two users the same pages: each search, by words
// and by meaning, finds the searcher's own documents alone, and naming the
// other's document finds nothing.
func TestOwnerIsolation(t *testing.T) {
	testDB.Reset(t)
	st := store.New(testDB.Pool)
	ada, bob := newUser(t, st), newUser(t, st)
	page := fixture(t, "real-python-controlflow")
	adaDoc := indexed(t, st, ada, page)
	bobDoc := indexed(t, st, bob, page)
	s := NewSearcher(st, embed.Fake{}, discard)

	for _, mode := range []Mode{WordsOnly, MeaningOnly, Hybrid} {
		res, err := s.Search(t.Context(), Request{Owner: bob, Query: "for statement", Limit: 50, Mode: mode})
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Hits) == 0 {
			t.Errorf("mode %d: Bob found nothing in his own document", mode)
		}
		for _, h := range res.Hits {
			if h.DocumentID != bobDoc {
				t.Errorf("mode %d: Bob's search found %v, not his", mode, h.DocumentID)
			}
		}
		other, err := s.Search(t.Context(), Request{Owner: bob, Query: "for statement", Document: adaDoc, Limit: 50, Mode: mode})
		if err != nil {
			t.Fatal(err)
		}
		if len(other.Hits) != 0 {
			t.Errorf("mode %d: Bob's search of Ada's document found %d hits, want none", mode, len(other.Hits))
		}
	}
}

func TestDegradedSearch(t *testing.T) {
	testDB.Reset(t)
	st := store.New(testDB.Pool)
	ada := newUser(t, st)
	indexed(t, st, ada, fixture(t, "real-python-controlflow"))
	down := broken{err: errors.New("connection refused")}
	s := NewSearcher(st, down, discard)

	res, err := s.Search(t.Context(), Request{Owner: ada, Query: "for statements", Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Degraded || len(res.Hits) == 0 {
		t.Fatalf("with the embedder down: degraded %v, %d hits; want degraded word matches", res.Degraded, len(res.Hits))
	}
	marked := false
	for _, seg := range res.Hits[0].Snippet {
		marked = marked || (seg.Mark && strings.HasPrefix(strings.ToLower(seg.Text), "statement"))
	}
	if !marked {
		t.Errorf("snippet %+v marks no match of statements", res.Hits[0].Snippet)
	}
	if _, err := s.Search(t.Context(), Request{Owner: ada, Query: "for", Limit: 5, Mode: MeaningOnly}); !errors.Is(err, ErrNoEmbedder) {
		t.Errorf("meaning only with the embedder down = %v, want ErrNoEmbedder", err)
	}
}
