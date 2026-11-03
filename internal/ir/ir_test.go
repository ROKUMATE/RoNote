package ir

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

func text(s string, marks ...Mark) Inline {
	return Inline{Text: s, Marks: marks}
}

func para(s string) Block {
	return Block{Kind: KindParagraph, Content: []Inline{text(s)}}
}

// sampleDoc is a valid document that uses every kind, mark and link kind.
func sampleDoc() *Doc {
	doc := &Doc{
		IRVersion: Version,
		Title:     "Sample",
		Source: Source{
			URL:          "https://example.com/doc",
			FinalURL:     "https://example.com/doc/",
			CanonicalURL: "https://example.com/doc/",
			FetchedAt:    time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC),
			BodySHA256:   strings.Repeat("ab", 32),
			ETag:         `"v1"`,
			LastModified: "Mon, 02 Nov 2026 00:00:00 GMT",
		},
		Extractor: Extractor{Name: "test", Version: "1"},
		Blocks: []Block{
			{Kind: KindHeading, Level: 1, Anchor: "intro", Content: []Inline{text("Intro")}},
			{Kind: KindParagraph, Content: []Inline{
				text("Press "),
				text("Ctrl", MarkKbd),
				text(" and read "),
				{Text: "the spec", Marks: []Mark{MarkItalic}, Link: &Link{Href: "https://example.com/spec", Kind: LinkSameDomain}},
				{Text: " or ", Link: nil},
				{Text: "jump", Link: &Link{Href: "#intro", Kind: LinkAnchor}},
				{Text: " to ", Marks: []Mark{MarkBold, MarkStrike, MarkSub, MarkSup, MarkCode}},
				{Text: "mail", Link: &Link{Href: "mailto:a@example.org", Kind: LinkExternal}},
			}},
			{Kind: KindList, Ordered: true, Items: []Item{
				{Children: []Block{para("one")}},
				{Children: []Block{para("two"), {Kind: KindList, Items: []Item{{Children: []Block{para("nested")}}}}}},
			}},
			{Kind: KindCode, Lang: "go", Title: "main.go", Text: "func main() {\n\tprintln(\"hi\")\n}\n"},
			{Kind: KindTable, Head: [][]Cell{{{text("Name")}, {text("Value")}}}, Rows: [][]Cell{{{text("a")}, {}}}},
			{Kind: KindCallout, Variant: VariantWarning, Title: "Careful", Children: []Block{para("Hot.")}},
			{Kind: KindImage, Src: "https://example.com/a.png", Alt: "A diagram", Caption: []Inline{text("Figure 1")}},
			{Kind: KindQuote, Children: []Block{para("Quoted.")}},
			{Kind: KindDivider},
			{Kind: KindTabs, Items: []Item{
				{Label: "npm", Children: []Block{{Kind: KindCode, Lang: "sh", Text: "npm install\n"}}},
				{Label: "pnpm", Children: []Block{{Kind: KindCode, Lang: "sh", Text: "pnpm install\n"}}},
			}},
		},
		Warnings: []Warning{{Code: WarningLowConfidence, Message: "no main element"}},
	}
	AssignIDs(doc.Blocks)
	return doc
}

func TestValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		mutate  func(*Doc)
		wantErr string
	}{
		{name: "valid document", mutate: func(*Doc) {}},
		{name: "wrong IR version", mutate: func(d *Doc) { d.IRVersion = 2 }, wantErr: "ir_version: is 2, want 1"},
		{name: "relative final URL", mutate: func(d *Doc) { d.Source.FinalURL = "/doc" }, wantErr: `source.final_url: "/doc" is not an absolute http(s) URL`},
		{name: "missing final URL", mutate: func(d *Doc) { d.Source.FinalURL = "" }, wantErr: "source.final_url: is empty"},
		{name: "missing extractor", mutate: func(d *Doc) { d.Extractor = Extractor{} }, wantErr: "extractor: needs a name and a version"},
		{name: "unknown warning", mutate: func(d *Doc) { d.Warnings[0].Code = "odd" }, wantErr: `warnings[0]: unknown code "odd"`},
		{name: "unknown kind", mutate: func(d *Doc) { d.Blocks[8].Kind = "marquee" }, wantErr: `blocks[8]: unknown kind "marquee"`},
		{name: "field from another kind", mutate: func(d *Doc) { d.Blocks[1].Lang = "go" }, wantErr: "blocks[1]: paragraph block sets lang"},
		{name: "title on a paragraph", mutate: func(d *Doc) { d.Blocks[1].Title = "main.go" }, wantErr: "blocks[1]: paragraph block sets title"},
		{name: "missing id", mutate: func(d *Doc) { d.Blocks[1].ID = "" }, wantErr: "blocks[1]: has no id"},
		{name: "duplicate id", mutate: func(d *Doc) { d.Blocks[2].ID = d.Blocks[1].ID }, wantErr: "blocks[2]: repeats id"},
		{name: "heading level 0", mutate: func(d *Doc) { d.Blocks[0].Level = 0 }, wantErr: "blocks[0]: heading level 0 is not 1-6"},
		{name: "heading level 7", mutate: func(d *Doc) { d.Blocks[0].Level = 7 }, wantErr: "blocks[0]: heading level 7 is not 1-6"},
		{name: "empty paragraph", mutate: func(d *Doc) { d.Blocks[1].Content = nil }, wantErr: "blocks[1].content: is empty"},
		{name: "empty text run", mutate: func(d *Doc) { d.Blocks[1].Content[0].Text = "" }, wantErr: "blocks[1].content[0]: text is empty"},
		{name: "unknown mark", mutate: func(d *Doc) { d.Blocks[1].Content[1].Marks = []Mark{"blink"} }, wantErr: `blocks[1].content[1]: unknown mark "blink"`},
		{name: "repeated mark", mutate: func(d *Doc) { d.Blocks[1].Content[1].Marks = []Mark{MarkKbd, MarkKbd} }, wantErr: `repeats mark "kbd"`},
		{name: "relative same_domain link", mutate: func(d *Doc) { d.Blocks[1].Content[3].Link.Href = "/spec" }, wantErr: `same_domain href "/spec" is not an absolute http(s) URL`},
		{name: "anchor without fragment", mutate: func(d *Doc) { d.Blocks[1].Content[5].Link.Href = "intro" }, wantErr: `anchor href "intro" is not #fragment`},
		{name: "javascript external link", mutate: func(d *Doc) { d.Blocks[1].Content[7].Link.Href = "javascript:alert(1)" }, wantErr: "is not an absolute http(s) or mailto URL"},
		{name: "unknown link kind", mutate: func(d *Doc) { d.Blocks[1].Content[3].Link.Kind = "sideways" }, wantErr: `unknown link kind "sideways"`},
		{name: "list without items", mutate: func(d *Doc) { d.Blocks[2].Items = nil }, wantErr: "blocks[2]: list has no items"},
		{name: "empty list item", mutate: func(d *Doc) { d.Blocks[2].Items[0].Children = nil }, wantErr: "blocks[2].items[0]: list item is empty"},
		{name: "list item with a label", mutate: func(d *Doc) { d.Blocks[2].Items[0].Label = "one" }, wantErr: "blocks[2].items[0]: list item has a label"},
		{
			name:    "nested block problem",
			mutate:  func(d *Doc) { d.Blocks[2].Items[1].Children[1].Items[0].Children[0].Kind = "x" },
			wantErr: `blocks[2].items[1].children[1].items[0].children[0]: unknown kind "x"`,
		},
		{name: "empty code text", mutate: func(d *Doc) { d.Blocks[3].Text = "" }, wantErr: "blocks[3]: code text is empty"},
		{name: "table without rows", mutate: func(d *Doc) { d.Blocks[4].Head, d.Blocks[4].Rows = nil, nil }, wantErr: "blocks[4]: table has no rows"},
		{name: "bad cell run", mutate: func(d *Doc) { d.Blocks[4].Rows[0][0][0].Text = "" }, wantErr: "blocks[4].rows[0][0][0]: text is empty"},
		{name: "unknown callout variant", mutate: func(d *Doc) { d.Blocks[5].Variant = "fun" }, wantErr: `unknown callout variant "fun"`},
		{name: "empty callout", mutate: func(d *Doc) { d.Blocks[5].Title, d.Blocks[5].Children = "", nil }, wantErr: "callout has neither title nor children"},
		{name: "relative image src", mutate: func(d *Doc) { d.Blocks[6].Src = "a.png" }, wantErr: `blocks[6].src: "a.png" is not an absolute http(s) URL`},
		{name: "data image src", mutate: func(d *Doc) { d.Blocks[6].Src = "data:image/png;base64,AA" }, wantErr: "is not an absolute http(s) URL"},
		{name: "empty quote", mutate: func(d *Doc) { d.Blocks[7].Children = nil }, wantErr: "blocks[7]: quote has no children"},
		{name: "tabs without tabs", mutate: func(d *Doc) { d.Blocks[9].Items = nil }, wantErr: "blocks[9]: tabs block has no tabs"},
		{name: "tab without a label", mutate: func(d *Doc) { d.Blocks[9].Items[1].Label = "" }, wantErr: "blocks[9].items[1]: tab has no label"},
		{name: "empty tab", mutate: func(d *Doc) { d.Blocks[9].Items[0].Children = nil }, wantErr: "blocks[9].items[0]: tab is empty"},
		{name: "problem inside a tab", mutate: func(d *Doc) { d.Blocks[9].Items[0].Children[0].Text = "" }, wantErr: "blocks[9].items[0].children[0]: code text is empty"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			doc := sampleDoc()
			tt.mutate(doc)

			err := Validate(doc)

			switch {
			case tt.wantErr == "" && err != nil:
				t.Fatalf("Validate() = %v, want nil", err)
			case tt.wantErr != "" && err == nil:
				t.Fatalf("Validate() = nil, want error containing %q", tt.wantErr)
			case tt.wantErr != "" && !strings.Contains(err.Error(), tt.wantErr):
				t.Fatalf("Validate() = %v, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}

func TestJSONRoundTrip(t *testing.T) {
	t.Parallel()
	doc := sampleDoc()

	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	var back Doc
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(doc, &back) {
		t.Errorf("round trip changed the document:\n got %+v\nwant %+v", back, *doc)
	}
	if err := Validate(&back); err != nil {
		t.Errorf("Validate(round trip) = %v", err)
	}
	if again, _ := json.Marshal(&back); string(again) != string(data) {
		t.Errorf("second marshal differs:\n%s\n%s", again, data)
	}
}

func TestAssignIDs(t *testing.T) {
	t.Parallel()

	page := func(edited string) []Block {
		return []Block{
			{Kind: KindHeading, Level: 1, Content: []Inline{text("Guide")}},
			para("First."),
			para(edited),
			{Kind: KindHeading, Level: 2, Content: []Inline{text("Details")}},
			para("First."),
			para("Same."),
			para("Same."),
			{Kind: KindCallout, Variant: VariantNote, Children: []Block{para("Inside.")}},
		}
	}
	ids := func(blocks []Block) []string {
		var out []string
		for _, b := range blocks {
			out = append(out, b.ID)
			for _, c := range b.Children {
				out = append(out, c.ID)
			}
		}
		return out
	}

	first, again, edited := page("Second."), page("Second."), page("Second, edited.")
	AssignIDs(first)
	AssignIDs(again)
	AssignIDs(edited)
	a, b, c := ids(first), ids(again), ids(edited)

	t.Run("same input gives the same IDs", func(t *testing.T) {
		if !reflect.DeepEqual(a, b) {
			t.Errorf("IDs differ:\n%v\n%v", a, b)
		}
	})
	t.Run("editing one paragraph changes only its ID", func(t *testing.T) {
		var changed []int
		for i := range a {
			if a[i] != c[i] {
				changed = append(changed, i)
			}
		}
		if !reflect.DeepEqual(changed, []int{2}) {
			t.Errorf("changed IDs at %v, want only [2]", changed)
		}
	})
	t.Run("heading path separates equal text", func(t *testing.T) {
		if a[1] == a[4] {
			t.Errorf("\"First.\" under different headings shares ID %s", a[1])
		}
	})
	t.Run("duplicates get a suffix", func(t *testing.T) {
		if a[6] != a[5]+"-2" {
			t.Errorf("duplicate IDs %s, %s; want the second to be the first plus -2", a[5], a[6])
		}
	})
	t.Run("nested blocks get IDs", func(t *testing.T) {
		if a[8] == "" || a[8] == a[7] {
			t.Errorf("callout child ID %q, callout ID %q", a[8], a[7])
		}
	})
	t.Run("tab labels are part of the ID", func(t *testing.T) {
		tabs := func(label string) []Block {
			return []Block{{Kind: KindTabs, Items: []Item{{Label: label, Children: []Block{para("Same.")}}}}}
		}
		npm, yarn := tabs("npm"), tabs("yarn")
		AssignIDs(npm)
		AssignIDs(yarn)
		if npm[0].ID == yarn[0].ID {
			t.Errorf("tabs labelled npm and yarn share ID %s", npm[0].ID)
		}
		if npm[0].Items[0].Children[0].ID == "" {
			t.Error("a block inside a tab has no ID")
		}
	})
	t.Run("a code title is part of the ID", func(t *testing.T) {
		code := []Block{{Kind: KindCode, Lang: "go", Text: "x := 1\n"}, {Kind: KindCode, Lang: "go", Title: "main.go", Text: "x := 1\n"}}
		AssignIDs(code)
		if code[0].ID == code[1].ID || code[1].ID == code[0].ID+"-2" {
			t.Errorf("code with and without a title got IDs %s and %s", code[0].ID, code[1].ID)
		}
	})
}
