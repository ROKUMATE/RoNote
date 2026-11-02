package main

import (
	"strings"
	"testing"

	"github.com/rokumate/ronote/internal/ir"
)

func runs(texts ...string) []ir.Inline {
	out := make([]ir.Inline, len(texts))
	for i, t := range texts {
		out[i] = ir.Inline{Text: t}
	}
	return out
}

func para(text string) ir.Block {
	return ir.Block{Kind: ir.KindParagraph, Content: runs(text)}
}

func TestBlockText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		block ir.Block
		want  string
	}{
		{name: "heading", block: ir.Block{Kind: ir.KindHeading, Level: 2, Content: runs("Setup")}, want: "## Setup"},
		{
			name: "marks and links",
			block: ir.Block{Kind: ir.KindParagraph, Content: []ir.Inline{
				{Text: "Run "},
				{Text: "go vet", Marks: []ir.Mark{ir.MarkCode}},
				{Text: ", press "},
				{Text: "Ctrl", Marks: []ir.Mark{ir.MarkKbd}},
				{Text: " or read "},
				{Text: "the spec", Marks: []ir.Mark{ir.MarkItalic}, Link: &ir.Link{Href: "https://go.dev/ref/spec", Kind: ir.LinkExternal}},
				{Text: " now", Marks: []ir.Mark{ir.MarkBold, ir.MarkStrike}},
			}},
			want: "Run `go vet`, press [Ctrl] or read [_the spec_](https://go.dev/ref/spec)~~** now**~~",
		},
		{
			name:  "code keeps its text",
			block: ir.Block{Kind: ir.KindCode, Lang: "go", Text: "func f() {\n\treturn  \n}\n"},
			want:  "```go\nfunc f() {\n\treturn  \n}\n```",
		},
		{
			name:  "nested lists",
			block: ir.Block{Kind: ir.KindList, Ordered: true, Items: [][]ir.Block{{para("One")}, {para("Two"), {Kind: ir.KindList, Items: [][]ir.Block{{para("Inner")}}}}}},
			want:  "1. One\n2. Two\n   - Inner",
		},
		{
			name:  "table",
			block: ir.Block{Kind: ir.KindTable, Head: [][]ir.Cell{{runs("A"), runs("B")}}, Rows: [][]ir.Cell{{runs("1"), runs("2")}}},
			want:  "| A | B |\n| --- | --- |\n| 1 | 2 |",
		},
		{
			name:  "callout",
			block: ir.Block{Kind: ir.KindCallout, Variant: ir.VariantWarning, Title: "Careful", Children: []ir.Block{para("Hot.")}},
			want:  "> [WARNING: Careful]\n> Hot.",
		},
		{
			name:  "image",
			block: ir.Block{Kind: ir.KindImage, Src: "https://x.example/a.png", Alt: "A", Caption: runs("Figure 1")},
			want:  "![A](https://x.example/a.png)\ncaption: Figure 1",
		},
		{name: "quote", block: ir.Block{Kind: ir.KindQuote, Children: []ir.Block{para("Said.")}}, want: "> Said."},
		{name: "divider", block: ir.Block{Kind: ir.KindDivider}, want: "---"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := blockText(tt.block); got != tt.want {
				t.Errorf("blockText() =\n%s\nwant\n%s", got, tt.want)
			}
		})
	}
}

func TestWriteText(t *testing.T) {
	t.Parallel()
	doc := &ir.Doc{
		Title:     "Page",
		Source:    ir.Source{FinalURL: "https://x.example/page"},
		Extractor: ir.Extractor{Name: "generic", Version: "1"},
		Blocks:    []ir.Block{para("Hello.")},
		Warnings:  []ir.Warning{{Code: ir.WarningLowConfidence, Message: "no main"}},
	}
	var b strings.Builder

	if err := writeText(&b, doc); err != nil {
		t.Fatal(err)
	}

	want := "# Page\nsource: https://x.example/page\nextractor: generic 1\n\nHello.\n\nwarnings:\n- low_confidence: no main\n"
	if b.String() != want {
		t.Errorf("writeText() =\n%s\nwant\n%s", b.String(), want)
	}
}
