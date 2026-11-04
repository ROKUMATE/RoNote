package doccontent

import (
	"errors"
	"testing"

	"github.com/rokumate/ronote/internal/ir"
)

// sampleExtraction has a paragraph, a code block, a list holding code,
// a table and tabs.
func sampleExtraction() *ir.Doc {
	return &ir.Doc{Blocks: []ir.Block{
		paragraph("p1", "Install it."),
		{ID: "c1", Kind: ir.KindCode, Lang: "sh", Title: "install.sh", TitleLink: &ir.Link{Href: "https://x.example/install.sh", Kind: ir.LinkExternal}, Text: "go install .\n"},
		{ID: "l1", Kind: ir.KindList, Items: []ir.Item{{Children: []ir.Block{paragraph("p2", "Run:"), {ID: "c2", Kind: ir.KindCode, Text: "go run .\n"}}}}},
		{ID: "t1", Kind: ir.KindTable, Rows: [][]ir.Cell{{{{Text: "a"}}}}},
		{ID: "tb1", Kind: ir.KindTabs, Items: []ir.Item{{Label: "Go", Children: []ir.Block{{ID: "c3", Kind: ir.KindCode, Text: "x\n"}}}}},
	}}
}

func TestCheckSource(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		change func(d *Doc)
		want   Code
	}{
		{name: "as extracted", change: func(*Doc) {}},
		{name: "an edited paragraph", change: func(d *Doc) {
			d.Blocks[0].Content = []ir.Inline{{Text: "Install it now."}}
			*d.Blocks[0].Edited = true
		}},
		{name: "an edited list around unchanged code", change: func(d *Doc) {
			d.Blocks[2].Items[0].Children[0].Content = []ir.Inline{{Text: "Then run:"}}
			*d.Blocks[2].Edited = true
		}},
		{name: "a deleted source block", change: func(d *Doc) { d.Blocks = d.Blocks[1:] }},
		{name: "a user block between", change: func(d *Doc) {
			d.Blocks = append(d.Blocks[:1], append([]Block{user(paragraph(newID(), "mine"))}, d.Blocks[1:]...)...)
		}},
		{name: "an edit marked unedited", change: func(d *Doc) {
			d.Blocks[0].Content = []ir.Inline{{Text: "Install it now."}}
		}, want: CodeSourceBlockModified},
		{name: "changed code", change: func(d *Doc) {
			d.Blocks[1].Text = "go install ./...\n"
			*d.Blocks[1].Edited = true
		}, want: CodeSourceBlockModified},
		{name: "a changed code title link", change: func(d *Doc) {
			d.Blocks[1].TitleLink = nil
			*d.Blocks[1].Edited = true
		}, want: CodeSourceBlockModified},
		{name: "changed code inside a list", change: func(d *Doc) {
			d.Blocks[2].Items[0].Children[1].Text = "go run ./cmd\n"
			*d.Blocks[2].Edited = true
		}, want: CodeSourceBlockModified},
		{name: "new code inside a list", change: func(d *Doc) {
			d.Blocks[2].Items[0].Children = append(d.Blocks[2].Items[0].Children, ir.Block{ID: "c9", Kind: ir.KindCode, Text: "rm -rf /\n"})
			*d.Blocks[2].Edited = true
		}, want: CodeSourceBlockModified},
		{name: "a changed table", change: func(d *Doc) {
			d.Blocks[3].Rows = [][]ir.Cell{{{{Text: "b"}}}}
			*d.Blocks[3].Edited = true
		}, want: CodeSourceBlockModified},
		{name: "changed tabs", change: func(d *Doc) {
			d.Blocks[4].Items[0].Label = "Rust"
			*d.Blocks[4].Edited = true
		}, want: CodeSourceBlockModified},
		{name: "an unknown source block", change: func(d *Doc) {
			d.Blocks[0].ID, d.Blocks[0].SourceBlockID = "p9", "p9"
		}, want: CodeUnknownSourceBlock},
		{name: "a nested block as a top-level source", change: func(d *Doc) {
			d.Blocks[0] = source(paragraph("p2", "Run:"), false)
		}, want: CodeUnknownSourceBlock},
		{name: "a source block used twice", change: func(d *Doc) {
			d.Blocks = append(d.Blocks, d.Blocks[0])
		}, want: CodeUnknownSourceBlock},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			extraction := sampleExtraction()
			doc := FromIR(sampleExtraction())
			tt.change(doc)
			err := CheckSource(doc, extraction)
			var p *Problem
			switch {
			case tt.want == "" && err != nil:
				t.Errorf("CheckSource() = %v, want nil", err)
			case tt.want != "" && (!errors.As(err, &p) || p.Code != tt.want):
				t.Errorf("CheckSource() = %v, want a %s problem", err, tt.want)
			}
		})
	}
}

func TestRevert(t *testing.T) {
	t.Parallel()
	extraction := sampleExtraction()
	doc := FromIR(sampleExtraction())
	doc.Blocks[0].Content = []ir.Inline{{Text: "Install it now."}}
	*doc.Blocks[0].Edited = true
	note := user(paragraph(newID(), "mine"))
	doc.Blocks = append(doc.Blocks, note)

	got, err := Revert(doc, extraction, "p1")
	if err != nil {
		t.Fatal(err)
	}
	if !same(&got.Blocks[0].Block, &extraction.Blocks[0]) || *got.Blocks[0].Edited || got.Blocks[0].Origin != OriginSource {
		t.Errorf("reverted block = %+v, want the extraction's, unedited", got.Blocks[0])
	}
	if err := CheckSource(got, extraction); err != nil {
		t.Errorf("reverted document breaks a rule: %v", err)
	}
	if !*doc.Blocks[0].Edited {
		t.Error("Revert changed the document it was given")
	}
	for _, id := range []string{note.ID, "nope"} {
		if _, err := Revert(doc, extraction, id); !errors.Is(err, ErrNoSourceBlock) {
			t.Errorf("Revert(%q) = %v, want ErrNoSourceBlock", id, err)
		}
	}
}
