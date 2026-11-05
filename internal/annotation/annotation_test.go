package annotation

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rokumate/ronote/internal/doccontent"
)

// fixture is the document of testdata/anchoring/blocktext.json.
func fixture(t *testing.T) *doccontent.Doc {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "anchoring", "blocktext.json"))
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		Content doccontent.Doc `json:"content"`
	}
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatal(err)
	}
	return &f.Content
}

func sel(start, end int, exact string) Selector {
	return Selector{Position: Position{Start: start, End: end}, Quote: Quote{Exact: exact}}
}

func TestCheck(t *testing.T) {
	t.Parallel()
	doc := fixture(t)
	tests := []struct {
		name  string
		block string
		sel   Selector
		want  Code
	}{
		{name: "words of a paragraph", block: "p1", sel: sel(4, 12, "go build")},
		{name: "the whole heading", block: "h1", sel: sel(0, 15, "Getting started")},
		{name: "across runs", block: "p1", sel: sel(7, 23, "build, then read")},
		{name: "after an emoji, counted in UTF-16", block: "p2", sel: sel(11, 14, "now")},
		{name: "the emoji itself", block: "p2", sel: sel(8, 10, "🚀")},
		{name: "code across lines", block: "c1", sel: sel(12, 20, "{\n\tfmt.P")},
		{name: "code in a list", block: "l1c", sel: sel(0, 4, "make")},
		{name: "code in tabs", block: "t1c", sel: sel(0, 3, "npm")},
		{name: "a paragraph in a quote", block: "q1p", sel: sel(0, 6, "Quoted")},
		{name: "with context", block: "p1", sel: Selector{Position: Position{Start: 4, End: 12}, Quote: Quote{Exact: "go build", Prefix: "Run ", Suffix: ", then"}}},
		{name: "a quote that is not the text", block: "p1", sel: sel(4, 12, "go bui1d"), want: CodeQuoteMismatch},
		{name: "offsets counted in bytes", block: "p2", sel: sel(13, 16, "now"), want: CodeQuoteMismatch},
		{name: "past the end", block: "q1p", sel: sel(2, 9, "oted!!!"), want: CodeQuoteMismatch},
		{name: "an empty range", block: "p1", sel: sel(4, 4, ""), want: CodeInvalidRequest},
		{name: "a negative start", block: "p1", sel: sel(-1, 3, "Run"), want: CodeInvalidRequest},
		{name: "a long prefix", block: "p1", sel: Selector{Position: Position{Start: 4, End: 12}, Quote: Quote{Exact: "go build", Prefix: strings.Repeat("x", 33)}}, want: CodeInvalidRequest},
		{name: "a list block", block: "l1", sel: sel(0, 2, "In"), want: CodeUnknownBlock},
		{name: "a table", block: "tb1", sel: sel(0, 4, "cell"), want: CodeUnknownBlock},
		{name: "no such block", block: "missing", sel: sel(0, 1, "x"), want: CodeUnknownBlock},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := Check(doc, tt.block, tt.sel)
			var p *Problem
			switch {
			case tt.want == "" && err != nil:
				t.Errorf("Check() = %v, want nil", err)
			case tt.want != "" && (!errors.As(err, &p) || p.Code != tt.want):
				t.Errorf("Check() = %v, want a %s problem", err, tt.want)
			}
		})
	}
}

func TestCheckNote(t *testing.T) {
	t.Parallel()
	if err := CheckNote(strings.Repeat("é", MaxNote)); err != nil {
		t.Errorf("CheckNote(%d characters) = %v, want nil", MaxNote, err)
	}
	var p *Problem
	if err := CheckNote(strings.Repeat("é", MaxNote+1)); !errors.As(err, &p) || p.Code != CodeInvalidRequest {
		t.Errorf("CheckNote(%d characters) = %v, want an invalid_request problem", MaxNote+1, err)
	}
}
