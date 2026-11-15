package docling

import (
	"encoding/json"
	"errors"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rokumate/ronote/internal/ir"
)

var pdf = PDF{Filename: "Lecture 3.pdf", SHA256: strings.Repeat("ab", 32), ConvertedAt: time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC), Docling: "1.38.2"}

// prov is a provenance on page with a box, in docling's JSON.
func prov(page int) string {
	return `[{"page_no": ` + itoa(page) + `, "bbox": {"l": 48, "t": 590, "r": 323, "b": 448, "coord_origin": "BOTTOMLEFT"}}]`
}

func itoa(i int) string { return strconv.Itoa(i) }

// text is a text item in docling's JSON.
func text(i int, label, s string, page int, extra string) string {
	return `{"self_ref": "#/texts/` + itoa(i) + `", "children": [], "content_layer": "body", "label": "` + label + `", "text": ` + quote(s) + `, "prov": ` + prov(page) + extra + `}`
}

func quote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// doc is a DoclingDocument of three pages with body children and items.
func doc(t *testing.T, body string, texts []string, extra string) *Document {
	t.Helper()
	src := `{"body": {"self_ref": "#/body", "children": [` + body + `]}, "texts": [` + strings.Join(texts, ",") + `]` + extra +
		`, "pages": {"1": {"page_no": 1, "size": {"height": 842}}, "2": {"page_no": 2, "size": {"height": 842}}, "3": {"page_no": 3, "size": {"height": 842}}}}`
	var d Document
	if err := json.Unmarshal([]byte(src), &d); err != nil {
		t.Fatalf("bad test document: %v\n%s", err, src)
	}
	return &d
}

func refs(names ...string) string {
	out := make([]string, len(names))
	for i, n := range names {
		out[i] = `{"$ref": "#/` + n + `"}`
	}
	return strings.Join(out, ",")
}

// shape is a block without its id: what a test compares.
func shape(blocks []ir.Block) []ir.Block {
	out := make([]ir.Block, len(blocks))
	for i, b := range blocks {
		b.ID = ""
		b.Children = shape(b.Children)
		for j := range b.Items {
			b.Items[j].Children = shape(b.Items[j].Children)
		}
		if len(b.Children) == 0 {
			b.Children = nil
		}
		out[i] = b
	}
	return out
}

func para(page int, s string) ir.Block {
	return ir.Block{Kind: ir.KindParagraph, Page: page, Content: []ir.Inline{{Text: s}}}
}

func TestMap(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		doc   func(t *testing.T) *Document
		layer map[string][]Line
		want  []ir.Block
		title string
	}{
		{
			name: "headings at docling's levels, and the title",
			doc: func(t *testing.T) *Document {
				return doc(t, refs("texts/0", "texts/1", "texts/2"), []string{
					text(0, "title", "Control Flow", 1, ""),
					text(1, "section_header", "4.1. if Statements", 1, `, "level": 2`),
					text(2, "section_header", "Deep", 2, `, "level": 9`),
				}, "")
			},
			want: []ir.Block{
				{Kind: ir.KindHeading, Page: 1, Level: 1, Content: []ir.Inline{{Text: "Control Flow"}}},
				{Kind: ir.KindHeading, Page: 1, Level: 2, Content: []ir.Inline{{Text: "4.1. if Statements"}}},
				{Kind: ir.KindHeading, Page: 2, Level: 6, Content: []ir.Inline{{Text: "Deep"}}},
			},
			title: "Control Flow",
		},
		{
			name: "prose labels are paragraphs; headers and footers go",
			doc: func(t *testing.T) *Document {
				return doc(t, refs("texts/0", "texts/1", "texts/2", "texts/3", "texts/4"), []string{
					text(0, "page_header", "Chapter 4", 1, ""),
					text(1, "text", "Perhaps the most well-known statement.", 1, ""),
					text(2, "footnote", "1. A note.", 2, ""),
					text(3, "formula", "a = b + c", 2, ""),
					text(4, "page_footer", "Page 2", 2, ""),
				}, "")
			},
			want:  []ir.Block{para(1, "Perhaps the most well-known statement."), para(2, "1. A note."), para(2, "a = b + c")},
			title: "Lecture 3",
		},
		{
			name: "furniture goes",
			doc: func(t *testing.T) *Document {
				furniture := strings.Replace(text(1, "text", "Running head", 1, ""), `"content_layer": "body"`, `"content_layer": "furniture"`, 1)
				return doc(t, refs("texts/0", "texts/1"), []string{text(0, "text", "Body.", 1, ""), furniture}, "")
			},
			want:  []ir.Block{para(1, "Body.")},
			title: "Lecture 3",
		},
		{
			name: "code from the text layer, and docling's language",
			doc: func(t *testing.T) *Document {
				return doc(t, refs("texts/0", "texts/1"), []string{
					text(0, "code", ">>> if x < 0: ... x = 0", 2, `, "code_language": "Python"`),
					text(1, "code", "joined by docling", 3, `, "code_language": "unknown"`),
				}, "")
			},
			layer: map[string][]Line{"#/texts/0": mono(">>> if x < 0:", "...     x = 0")},
			want: []ir.Block{
				{Kind: ir.KindCode, Page: 2, Lang: "python", Text: ">>> if x < 0:\n...     x = 0\n"},
				{Kind: ir.KindCode, Page: 3, Text: "joined by docling"},
			},
			title: "Lecture 3",
		},
		{
			name: "a label docling's code box took in is a paragraph",
			doc: func(t *testing.T) *Document {
				return doc(t, refs("texts/0"), []string{text(0, "code", "Filename: src/main.rs fn main() { } Result:", 2, "")}, "")
			},
			layer: map[string][]Line{"#/texts/0": {
				{Text: "Filename: src/main.rs"},
				{Text: "fn main() {", Mono: true},
				{Text: "", Mono: true},
				{Text: "}", Mono: true},
				{Text: "Result:"},
				{Text: " city | temp", Mono: true},
			}},
			want: []ir.Block{
				para(2, "Filename: src/main.rs"),
				{Kind: ir.KindCode, Page: 2, Text: "fn main() {\n\n}\n"},
				para(2, "Result:"),
				{Kind: ir.KindCode, Page: 2, Text: " city | temp\n"},
			},
			title: "Lecture 3",
		},
		{
			name: "prose and pictures set in monospace are code",
			doc: func(t *testing.T) *Document {
				return doc(t, refs("texts/0", "texts/1", "pictures/0"), []string{
					text(0, "text", "cd %HOMEPATH%", 1, ""),
					text(1, "text", "Open a prompt and run cd.", 1, ""),
				}, `, "pictures": [{"self_ref": "#/pictures/0", "label": "picture", "content_layer": "body", "children": [], "prov": `+prov(2)+`}]`)
			},
			layer: map[string][]Line{
				"#/texts/0":    mono("cd %HOMEPATH%"),
				"#/texts/1":    {{Text: "Open a prompt and run"}, {Text: "cd.", Mono: true}},
				"#/pictures/0": mono("$ uv run fastapi dev", "", "  Starting"),
			},
			want: []ir.Block{
				{Kind: ir.KindCode, Page: 1, Text: "cd %HOMEPATH%\n"},
				para(1, "Open a prompt and run cd."),
				{Kind: ir.KindCode, Page: 2, Text: "$ uv run fastapi dev\n\n  Starting\n"},
			},
			title: "Lecture 3",
		},
		{
			name: "ligatures expand, in prose and code",
			doc: func(t *testing.T) *Document {
				return doc(t, refs("texts/0", "texts/1"), []string{
					text(0, "text", "The \ufb01rst \ufb02ag, o\ufb00er, su\ufb03x, ba\ufb04e.", 1, ""),
					text(1, "code", "x", 1, ""),
				}, "")
			},
			layer: map[string][]Line{"#/texts/1": mono("de\ufb01ne()")},
			want:  []ir.Block{para(1, "The first flag, offer, suffix, baffle."), {Kind: ir.KindCode, Page: 1, Text: "define()\n"}},
			title: "Lecture 3",
		},
		{
			name: "a list group, ordered by its items, with a nested list",
			doc: func(t *testing.T) *Document {
				item := text(0, "list_item", "One", 1, `, "enumerated": true`)
				item = strings.Replace(item, `"children": []`, `"children": [`+refs("groups/1")+`]`, 1)
				return doc(t, refs("groups/0"), []string{item, text(1, "list_item", "Two", 2, ""), text(2, "list_item", "Inner", 1, "")},
					`, "groups": [`+
						`{"self_ref": "#/groups/0", "label": "list", "content_layer": "body", "children": [`+refs("texts/0", "texts/1")+`]},`+
						`{"self_ref": "#/groups/1", "label": "list", "content_layer": "body", "children": [`+refs("texts/2")+`]}]`)
			},
			want: []ir.Block{{Kind: ir.KindList, Page: 1, Ordered: true, Items: []ir.Item{
				{Children: []ir.Block{para(1, "One"), {Kind: ir.KindList, Page: 1, Items: []ir.Item{{Children: []ir.Block{para(1, "Inner")}}}}}},
				{Children: []ir.Block{para(2, "Two")}},
			}}},
			title: "Lecture 3",
		},
		{
			name: "an inline group is one paragraph",
			doc: func(t *testing.T) *Document {
				return doc(t, refs("groups/0"), []string{text(0, "text", "Use", 1, ""), text(1, "text", "print()", 1, "")},
					`, "groups": [{"self_ref": "#/groups/0", "label": "inline", "content_layer": "body", "children": [`+refs("texts/0", "texts/1")+`]}]`)
			},
			want:  []ir.Block{para(1, "Use print()")},
			title: "Lecture 3",
		},
		{
			name: "a table with its header row, after its caption",
			doc: func(t *testing.T) *Document {
				return doc(t, refs("tables/0", "texts/1"), []string{text(0, "caption", "Table 1: Operators", 2, ""), text(1, "text", "After.", 2, "")},
					`, "tables": [{"self_ref": "#/tables/0", "label": "table", "content_layer": "body", "children": [], "prov": `+prov(2)+`, "captions": [`+refs("texts/0")+`], "data": {"num_rows": 2, "num_cols": 2, "table_cells": [`+
						`{"text": "Op", "start_row_offset_idx": 0, "start_col_offset_idx": 0, "column_header": true},`+
						`{"text": "Means", "start_row_offset_idx": 0, "start_col_offset_idx": 1, "column_header": true},`+
						`{"text": "==", "start_row_offset_idx": 1, "start_col_offset_idx": 0},`+
						`{"text": "equal", "start_row_offset_idx": 1, "start_col_offset_idx": 1}]}}]`)
			},
			want: []ir.Block{
				para(2, "Table 1: Operators"),
				{
					Kind: ir.KindTable, Page: 2,
					Head: [][]ir.Cell{{{{Text: "Op"}}, {{Text: "Means"}}}},
					Rows: [][]ir.Cell{{{{Text: "=="}}, {{Text: "equal"}}}},
				},
				para(2, "After."),
			},
			title: "Lecture 3",
		},
		{
			name: "pictures go, their captions stay once",
			doc: func(t *testing.T) *Document {
				picture := func(i int, captions string) string {
					return `{"self_ref": "#/pictures/` + itoa(i) + `", "label": "picture", "content_layer": "body", "children": [` + refs("texts/1") + `], "prov": ` + prov(1) + `, "captions": [` + captions + `]}`
				}
				return doc(t, refs("pictures/0", "texts/0", "pictures/1"), []string{
					text(0, "caption", "Figure 1: A loop", 1, ""), text(1, "text", "label inside the figure", 1, ""),
				}, `, "pictures": [`+picture(0, refs("texts/0"))+`,`+picture(1, "")+`]`)
			},
			want:  []ir.Block{para(1, "Figure 1: A loop")},
			title: "Lecture 3",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := Map(tt.doc(t), pdf, tt.layer)
			if err != nil {
				t.Fatalf("Map() = %v", err)
			}
			if !reflect.DeepEqual(shape(got.Blocks), tt.want) {
				gotJSON, _ := json.MarshalIndent(shape(got.Blocks), "", "  ")
				wantJSON, _ := json.MarshalIndent(tt.want, "", "  ")
				t.Errorf("blocks:\n%s\nwant:\n%s", gotJSON, wantJSON)
			}
			if got.Title != tt.title {
				t.Errorf("title = %q, want %q", got.Title, tt.title)
			}
		})
	}
}

func TestMapRecordsTheSource(t *testing.T) {
	t.Parallel()
	got, err := Map(doc(t, refs("texts/0"), []string{text(0, "text", "x", 1, "")}, ""), pdf, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := ir.Source{FetchedAt: pdf.ConvertedAt, BodySHA256: pdf.SHA256, PDF: &ir.PDF{Filename: "Lecture 3.pdf", Pages: 3}}
	if !reflect.DeepEqual(got.Source, want) {
		t.Errorf("source = %+v, want %+v", got.Source, want)
	}
	if got.Extractor != (ir.Extractor{Name: "docling", Version: "1.38.2+map" + MapVersion}) {
		t.Errorf("extractor = %+v", got.Extractor)
	}
	if got.Warnings != nil {
		t.Errorf("warnings = %v, want none", got.Warnings)
	}
}

func TestMapCountsFigures(t *testing.T) {
	t.Parallel()
	picture := func(i int) string {
		return `{"self_ref": "#/pictures/` + itoa(i) + `", "label": "picture", "content_layer": "body", "children": [], "prov": ` + prov(1) + `}`
	}
	d := doc(t, refs("texts/0", "pictures/0", "pictures/1"), []string{text(0, "text", "x", 1, "")}, `, "pictures": [`+picture(0)+`,`+picture(1)+`]`)
	got, err := Map(d, pdf, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []ir.Warning{{Code: ir.WarningFiguresOmitted, Message: "2 figures from the PDF are not shown.", Count: 2}}
	if !reflect.DeepEqual(got.Warnings, want) {
		t.Errorf("warnings = %+v, want %+v", got.Warnings, want)
	}
}

func TestMapWithoutText(t *testing.T) {
	t.Parallel()
	picture := `{"self_ref": "#/pictures/0", "label": "picture", "content_layer": "body", "children": [], "prov": ` + prov(1) + `}`
	for name, d := range map[string]*Document{
		"a page that is one picture": doc(t, refs("pictures/0"), nil, `, "pictures": [`+picture+`]`),
		"blank text":                 doc(t, refs("texts/0"), []string{text(0, "text", "  \n", 1, "")}, ""),
	} {
		if _, err := Map(d, pdf, nil); !errors.Is(err, ErrNoText) {
			t.Errorf("%s: Map() = %v, want ErrNoText", name, err)
		}
	}
}

func TestMapRefusesBadReferences(t *testing.T) {
	t.Parallel()
	for _, ref := range []string{"texts/7", "nothing/0", "texts/x"} {
		d := doc(t, refs(ref), []string{text(0, "text", "x", 1, "")}, "")
		if _, err := Map(d, pdf, nil); err == nil || errors.Is(err, ErrNoText) {
			t.Errorf("Map(ref %s) = %v, want a reference error", ref, err)
		}
	}
}

// mono is lines all set in monospace.
func mono(texts ...string) []Line {
	out := make([]Line, len(texts))
	for i, t := range texts {
		out[i] = Line{Text: t, Mono: true}
	}
	return out
}

func TestLayerBoxes(t *testing.T) {
	t.Parallel()
	split := text(0, "code", "a b", 1, "")
	split = strings.Replace(split, `"prov": `+prov(1), `"prov": [{"page_no": 1, "bbox": {"l": 1, "t": 30, "r": 3, "b": 20, "coord_origin": "BOTTOMLEFT"}}, {"page_no": 2, "bbox": {"l": 1, "t": 12, "r": 3, "b": 40, "coord_origin": "TOPLEFT"}}]`, 1)
	d := doc(t, refs("texts/0", "texts/1", "texts/2", "texts/3", "pictures/0"),
		[]string{split, text(1, "text", "x", 1, ""), text(2, "section_header", "H", 1, ""), text(3, "code", "c", 3, "")},
		`, "pictures": [{"self_ref": "#/pictures/0", "label": "picture", "content_layer": "body", "children": [], "prov": `+prov(2)+`}]`)

	boxes := LayerBoxes(d)
	want := []Box{
		{Ref: "#/texts/0", Page: 1, L: 1, T: 30, R: 3, B: 20},
		{Ref: "#/texts/0", Page: 2, L: 1, T: 830, R: 3, B: 802},
		{Ref: "#/texts/1", Page: 1, L: 48, T: 590, R: 323, B: 448},
		{Ref: "#/texts/3", Page: 3, L: 48, T: 590, R: 323, B: 448},
		{Ref: "#/pictures/0", Page: 2, L: 48, T: 590, R: 323, B: 448},
	}
	if !reflect.DeepEqual(boxes, want) {
		t.Fatalf("LayerBoxes = %+v, want %+v", boxes, want)
	}
	layer, err := Layer(boxes, [][]Line{mono("def f():"), mono("    return 1"), {{Text: "x"}}, {}, {}})
	if err != nil {
		t.Fatal(err)
	}
	if want := mono("def f():", "    return 1"); !reflect.DeepEqual(layer["#/texts/0"], want) {
		t.Errorf("Layer joins a split block's pages to %+v, want %+v", layer["#/texts/0"], want)
	}
	if _, err := Layer(boxes, [][]Line{mono("x")}); err == nil {
		t.Error("Layer with fewer blocks than boxes succeeded")
	}
}

// FuzzMap feeds Map arbitrary documents: it may refuse them, but whatever
// it returns is valid IR, and it never panics.
func FuzzMap(f *testing.F) {
	f.Add(`{"body": {"children": [{"$ref": "#/texts/0"}]}, "texts": [{"self_ref": "#/texts/0", "label": "code", "text": "x", "prov": [{"page_no": 1}]}], "pages": {"1": {"page_no": 1}}}`)
	f.Add(`{"body": {"children": [{"$ref": "#/groups/0"}]}, "groups": [{"label": "list", "children": [{"$ref": "#/groups/0"}]}], "pages": {"1": {}}}`)
	f.Add(`{"body": {"children": [{"$ref": "#/tables/0"}]}, "tables": [{"data": {"num_rows": 2, "num_cols": 1, "table_cells": [{"text": "a", "start_row_offset_idx": 5}]}}], "pages": {"1": {}}}`)
	f.Fuzz(func(t *testing.T, src string) {
		var d Document
		if json.Unmarshal([]byte(src), &d) != nil {
			return
		}
		doc, err := Map(&d, pdf, nil)
		if err != nil {
			return
		}
		if err := ir.Validate(doc); err != nil {
			t.Errorf("Map returned invalid IR: %v", err)
		}
	})
}
