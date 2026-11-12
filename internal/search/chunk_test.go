package search

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/rokumate/ronote/internal/doccontent"
	"github.com/rokumate/ronote/internal/ir"
)

var update = flag.Bool("update", false, "rewrite testdata/chunks/*.json from the current chunker")

// fixtures returns every golden fixture's extraction, by name.
func fixtures(t *testing.T) map[string]*ir.Doc {
	t.Helper()
	dirs, err := filepath.Glob(filepath.Join("..", "..", "testdata", "golden", "*", "expected.json"))
	if err != nil || len(dirs) == 0 {
		t.Fatalf("no golden fixtures: %v", err)
	}
	docs := map[string]*ir.Doc{}
	for _, path := range dirs {
		data, err := os.ReadFile(path) //nolint:gosec // a fixture path from a glob
		if err != nil {
			t.Fatal(err)
		}
		var doc ir.Doc
		if err := json.Unmarshal(data, &doc); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		docs[filepath.Base(filepath.Dir(path))] = &doc
	}
	return docs
}

// summary is what a chunk golden keeps: enough to review a change to the
// chunking without repeating the fixture's text.
type summary struct {
	BlockIDs    []string `json:"block_ids"`
	HeadingPath []string `json:"heading_path"`
	Kind        Kind     `json:"kind"`
	Lang        string   `json:"lang,omitempty"`
	Chars       int      `json:"chars"`
	Embed       string   `json:"embed_start"`
}

func TestChunkGoldens(t *testing.T) {
	t.Parallel()
	for name, doc := range fixtures(t) {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			chunks := Chunks(doc.Title, doccontent.FromIR(doc), nil)
			got := make([]summary, len(chunks))
			for i, c := range chunks {
				got[i] = summary{
					BlockIDs: c.BlockIDs, HeadingPath: c.HeadingPath, Kind: c.Kind, Lang: c.Lang,
					Chars: utf8.RuneCountInString(c.Text), Embed: cut(c.Embed, 120),
				}
			}
			var buf bytes.Buffer
			enc := json.NewEncoder(&buf)
			enc.SetEscapeHTML(false)
			enc.SetIndent("", "  ")
			if err := enc.Encode(got); err != nil {
				t.Fatal(err)
			}
			data := bytes.TrimSuffix(buf.Bytes(), []byte("\n"))
			path := filepath.Join("testdata", "chunks", name+".json")
			if *update {
				if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(path) //nolint:gosec // a golden path built from a fixture name
			if err != nil {
				t.Fatalf("%v; run go test ./internal/search -update", err)
			}
			if string(want) != string(data)+"\n" {
				t.Errorf("chunks of %s changed; run go test ./internal/search -update and review the diff", name)
			}
		})
	}
}

// TestChunkRules checks every fixture against the rules: each top-level
// block in exactly one chunk, in order; each code block's text whole in one
// chunk; chunks within the limit unless one block is larger; breadcrumbs
// from the title.
func TestChunkRules(t *testing.T) {
	t.Parallel()
	for name, doc := range fixtures(t) {
		content := doccontent.FromIR(doc)
		chunks := Chunks(doc.Title, content, nil)
		var order []string
		for _, c := range chunks {
			order = append(order, c.BlockIDs...)
			if !strings.HasPrefix(c.Embed, doc.Title) {
				t.Errorf("%s: chunk %v's embedding input starts %q, want the title", name, c.BlockIDs, cut(c.Embed, 40))
			}
			if len(c.BlockIDs) > 1 && utf8.RuneCountInString(c.Text) > maxChunkChars {
				t.Errorf("%s: chunk %v of several blocks is over %d characters", name, c.BlockIDs, maxChunkChars)
			}
			if utf8.RuneCountInString(c.Embed) > maxEmbedChars {
				t.Errorf("%s: chunk %v's embedding input is over %d characters", name, c.BlockIDs, maxEmbedChars)
			}
		}
		var want []string
		for _, b := range content.Blocks {
			if b.Kind == ir.KindHeading || b.Kind == ir.KindDivider {
				continue
			}
			if r := (&chunker{}).render(&b.Block); r.size > 0 {
				want = append(want, b.ID)
			}
		}
		if !slices.Equal(order, want) {
			t.Errorf("%s: chunks hold blocks %v, want each once in order: %v", name, order, want)
		}
		for i := range content.Blocks {
			walk(&content.Blocks[i].Block, func(b *ir.Block) {
				if b.Kind != ir.KindCode || strings.TrimSpace(b.Text) == "" {
					return
				}
				holders := 0
				for _, c := range chunks {
					if strings.Contains(c.Code, strings.TrimSpace(b.Text)) {
						holders++
					}
				}
				if holders == 0 {
					t.Errorf("%s: code block %s is split or missing", name, b.ID)
				}
			})
		}
	}
}

func para(id, text string) doccontent.Block {
	return doccontent.Block{Block: ir.Block{ID: id, Kind: ir.KindParagraph, Content: []ir.Inline{{Text: text}}}, Origin: doccontent.OriginSource}
}

func heading(id string, level int, text string) doccontent.Block {
	return doccontent.Block{Block: ir.Block{ID: id, Kind: ir.KindHeading, Level: level, Content: []ir.Inline{{Text: text}}}, Origin: doccontent.OriginSource}
}

func TestChunkSections(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("x", maxChunkChars+10)
	content := &doccontent.Doc{DocVersion: 1, CodeLangs: map[string]string{"c2": "python"}, Blocks: []doccontent.Block{
		heading("h1", 1, "Guide"),
		para("p1", "Intro."),
		heading("h2", 2, "Install"),
		para("p2", "Run this:"),
		{Block: ir.Block{ID: "c1", Kind: ir.KindCode, Lang: "bash", Text: "go install ./..."}, Origin: doccontent.OriginSource},
		heading("h3", 3, "Details"),
		{Block: ir.Block{ID: "c2", Kind: ir.KindCode, Text: long}, Origin: doccontent.OriginSource},
		para("p3", "After."),
		heading("h4", 2, "Use"),
		{Block: ir.Block{ID: "l1", Kind: ir.KindList, Items: []ir.Item{{Children: []ir.Block{
			{ID: "l1p", Kind: ir.KindParagraph, Content: []ir.Inline{{Text: "Call "}, {Text: "http.ServeMux", Marks: []ir.Mark{ir.MarkCode}}}},
		}}}}, Origin: doccontent.OriginSource},
	}}

	chunks := Chunks("Guide", content, []Note{
		{BlockID: "l1p", Quote: "Call", Text: "Remember this one"},
		{BlockID: "gone", Quote: "x", Text: "Its block was deleted"},
		{BlockID: "p1", Quote: "Intro", Text: "  "},
	})

	type got struct {
		ids  []string
		path []string
		kind Kind
		lang string
	}
	want := []got{
		{[]string{"p1"}, []string{}, KindProse, ""},
		{[]string{"p2", "c1"}, []string{"Install"}, KindProse, ""},
		{[]string{"c2"}, []string{"Install", "Details"}, KindCode, "python"},
		{[]string{"p3"}, []string{"Install", "Details"}, KindProse, ""},
		{[]string{"l1"}, []string{"Use"}, KindProse, ""},
		{[]string{"l1"}, []string{"Use"}, KindNote, ""},
	}
	if len(chunks) != len(want) {
		t.Fatalf("%d chunks, want %d: %+v", len(chunks), len(want), chunks)
	}
	for i, w := range want {
		c := chunks[i]
		if !slices.Equal(c.BlockIDs, w.ids) || !slices.Equal(c.HeadingPath, w.path) || c.Kind != w.kind || c.Lang != w.lang {
			t.Errorf("chunk %d = %v %q %s %q, want %v %q %s %q", i, c.BlockIDs, c.HeadingPath, c.Kind, c.Lang, w.ids, w.path, w.kind, w.lang)
		}
	}
	if !strings.HasPrefix(chunks[1].Embed, "Guide > Install\n\nRun this:\n\nCode (bash):\ngo install") {
		t.Errorf("embedding input = %q, want the breadcrumb, then code labelled", chunks[1].Embed)
	}
	if chunks[2].Text != long || utf8.RuneCountInString(chunks[2].Embed) > maxEmbedChars {
		t.Error("an oversized code block must keep its text whole and cut only its embedding input")
	}
	if !slices.Contains(chunks[4].Idents, "http.servemux") || !slices.Contains(chunks[4].Idents, "serve") {
		t.Errorf("inline code idents = %q, want http.servemux and its words", chunks[4].Idents)
	}
	if note := chunks[5]; note.Text != "Remember this one" || !strings.Contains(note.Embed, `Note on "Call": Remember this one`) {
		t.Errorf("note chunk = %+v", note)
	}
}
