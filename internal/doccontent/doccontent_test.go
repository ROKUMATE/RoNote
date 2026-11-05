package doccontent

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/google/uuid"

	"github.com/rokumate/ronote/internal/ir"
)

func paragraph(id, text string) ir.Block {
	return ir.Block{ID: id, Kind: ir.KindParagraph, Content: []ir.Inline{{Text: text}}}
}

func source(b ir.Block, edited bool) Block {
	return Block{Block: b, Origin: OriginSource, Edited: &edited, SourceBlockID: b.ID}
}

func user(b ir.Block) Block {
	return Block{Block: b, Origin: OriginUser}
}

func newID() string { return uuid.Must(uuid.NewV7()).String() }

func TestValidate(t *testing.T) {
	t.Parallel()
	code := ir.Block{ID: "c1", Kind: ir.KindCode, Text: "x := 1\n"}
	note := newID()
	tests := []struct {
		name   string
		blocks []Block
		want   Code
	}{
		{name: "source and user blocks", blocks: []Block{source(paragraph("p1", "a"), false), source(code, true), user(paragraph(note, "mine"))}},
		{name: "a user list of paragraphs", blocks: []Block{user(ir.Block{ID: newID(), Kind: ir.KindList, Items: []ir.Item{{Children: []ir.Block{paragraph(newID(), "x")}}}})}},
		{name: "a user divider", blocks: []Block{user(ir.Block{ID: newID(), Kind: ir.KindDivider})}},
		{name: "a user code block", blocks: []Block{user(ir.Block{ID: newID(), Kind: ir.KindCode, Text: "x\n"})}, want: CodeBlockKindNotAllowed},
		{
			name:   "code nested in a user list",
			blocks: []Block{user(ir.Block{ID: newID(), Kind: ir.KindList, Items: []ir.Item{{Children: []ir.Block{{ID: newID(), Kind: ir.KindCode, Text: "x\n"}}}}})},
			want:   CodeBlockKindNotAllowed,
		},
		{name: "a user table", blocks: []Block{user(ir.Block{ID: newID(), Kind: ir.KindTable, Rows: [][]ir.Cell{{{{Text: "x"}}}}})}, want: CodeBlockKindNotAllowed},
		{name: "an invalid IR block", blocks: []Block{user(ir.Block{ID: newID(), Kind: ir.KindParagraph})}, want: CodeInvalidContent},
		{name: "repeated ids", blocks: []Block{source(paragraph("p1", "a"), false), source(paragraph("p1", "b"), false)}, want: CodeInvalidContent},
		{name: "a source block without edited", blocks: []Block{{Block: paragraph("p1", "a"), Origin: OriginSource, SourceBlockID: "p1"}}, want: CodeInvalidContent},
		{name: "a source block without its source id", blocks: []Block{{Block: paragraph("p1", "a"), Origin: OriginSource, Edited: new(bool)}}, want: CodeInvalidContent},
		{name: "a source block under another id", blocks: []Block{{Block: paragraph("p1", "a"), Origin: OriginSource, Edited: new(bool), SourceBlockID: "p2"}}, want: CodeInvalidContent},
		{name: "a user block with source fields", blocks: []Block{{Block: paragraph(note, "a"), Origin: OriginUser, Edited: new(bool)}}, want: CodeInvalidContent},
		{name: "a user block id that is not a UUIDv7", blocks: []Block{user(paragraph(uuid.NewString(), "a"))}, want: CodeInvalidContent},
		{name: "an unknown origin", blocks: []Block{{Block: paragraph("p1", "a"), Origin: "ai"}}, want: CodeInvalidContent},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := Validate(&Doc{DocVersion: Version, Blocks: tt.blocks})
			var p *Problem
			switch {
			case tt.want == "" && err != nil:
				t.Errorf("Validate() = %v, want nil", err)
			case tt.want != "" && (!errors.As(err, &p) || p.Code != tt.want):
				t.Errorf("Validate() = %v, want a %s problem", err, tt.want)
			}
		})
	}

	if err := Validate(&Doc{DocVersion: 2}); err == nil {
		t.Error("Validate() accepts doc_version 2")
	}
}

func TestValidateCodeLangs(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		langs map[string]string
		want  Code
	}{
		{name: "none"},
		{name: "a top-level code block", langs: map[string]string{"c1": "go"}},
		{name: "code in a list and in tabs", langs: map[string]string{"c2": "bash", "c3": "plaintext"}},
		{name: "an unknown block", langs: map[string]string{"c9": "go"}, want: CodeInvalidContent},
		{name: "a block that is not code", langs: map[string]string{"p1": "go"}, want: CodeInvalidContent},
		{name: "a nested block that is not code", langs: map[string]string{"p2": "go"}, want: CodeInvalidContent},
		{name: "a language RoNote does not offer", langs: map[string]string{"c1": "cobol"}, want: CodeInvalidContent},
		{name: "a language under another name", langs: map[string]string{"c1": "golang"}, want: CodeInvalidContent},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			doc := FromIR(sampleExtraction())
			doc.CodeLangs = tt.langs
			err := Validate(doc)
			var p *Problem
			switch {
			case tt.want == "" && err != nil:
				t.Errorf("Validate() = %v, want nil", err)
			case tt.want != "" && (!errors.As(err, &p) || p.Code != tt.want):
				t.Errorf("Validate() = %v, want a %s problem", err, tt.want)
			}
		})
	}
}

func TestCodeLangsAreOptional(t *testing.T) {
	t.Parallel()
	data, err := json.Marshal(FromIR(sampleExtraction()))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "code_langs") {
		t.Errorf("a document without chosen languages writes code_langs: %s", data)
	}
}

// TestFromIRKeepsEveryGolden makes a document of every golden extraction:
// each is valid, and its blocks are the extraction's, byte for byte.
func TestFromIRKeepsEveryGolden(t *testing.T) {
	t.Parallel()
	goldens := os.DirFS(filepath.Join("..", "..", "testdata", "golden"))
	files, err := fs.Glob(goldens, "*/expected.json")
	if err != nil || len(files) == 0 {
		t.Fatalf("no golden files: %v", err)
	}
	for _, file := range files {
		data, err := fs.ReadFile(goldens, file)
		if err != nil {
			t.Fatal(err)
		}
		var extraction ir.Doc
		if err := json.Unmarshal(data, &extraction); err != nil {
			t.Fatal(err)
		}
		doc := FromIR(&extraction)
		if err := Validate(doc); err != nil {
			t.Errorf("%s: %v", file, err)
		}
		for i, b := range doc.Blocks {
			got, _ := json.Marshal(b.Block)
			want, _ := json.Marshal(extraction.Blocks[i])
			if string(got) != string(want) || b.Origin != OriginSource || *b.Edited || b.SourceBlockID != b.ID {
				t.Errorf("%s: block %d = %s, want the extraction's %s as an unedited source block", file, i, got, want)
			}
		}
	}
}

// TestBlockText reads the plain text of every block in the shared fixture,
// which the web app's anchoring test reads too.
func TestBlockText(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "anchoring", "blocktext.json"))
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		Content Doc `json:"content"`
		Texts   map[string]struct {
			Text   string `json:"text"`
			Length int    `json:"length"`
		} `json:"texts"`
		None []string `json:"none"`
	}
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatal(err)
	}
	for id, want := range f.Texts {
		got, ok := BlockText(&f.Content, id)
		if !ok || got != want.Text {
			t.Errorf("BlockText(%q) = %q, %v, want %q", id, got, ok, want.Text)
		}
		if n := len(utf16.Encode([]rune(got))); n != want.Length {
			t.Errorf("BlockText(%q) is %d UTF-16 code units, want %d", id, n, want.Length)
		}
	}
	for _, id := range f.None {
		if got, ok := BlockText(&f.Content, id); ok {
			t.Errorf("BlockText(%q) = %q, want none", id, got)
		}
	}
}
