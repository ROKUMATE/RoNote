package extract

import (
	"fmt"
	"strings"
	"testing"

	"github.com/rokumate/ronote/internal/extract/adapter"
	"github.com/rokumate/ronote/internal/ir"
)

// testFramework is an adapter for a made-up framework, to test how plans are
// carried out without depending on a real adapter's configuration.
func testFramework() *adapter.Adapter {
	return &adapter.Adapter{
		Name: "testfw", Version: "3",
		Generators: []string{"TestFW"},
		Root:       "div.content",
		Require:    []string{".body"},
		Remove:     ".chrome",
		Callouts:   []adapter.Callout{{Selector: "aside.box", Title: ".box-title"}},
		Tabs:       []adapter.Tabs{{Container: ".tabset", Label: "[role=tab]", Panel: "[role=tabpanel]"}},
		Code: []adapter.Code{
			{Block: "div.codebox", Title: ".file", NotText: ".ln"},
			{Block: "table.lines", Pre: []string{"td.code pre"}},
			{Block: "pre.listing", TitleBefore: "span.filename"},
		},
	}
}

func extractTestFramework(t *testing.T, body string) *ir.Doc {
	t.Helper()
	page := `<html><head><meta name="generator" content="TestFW 1.0"><title>T</title></head><body>` + body + `</body></html>`
	doc, err := extractWith(strings.NewReader(page), ir.Source{URL: "https://docs.example.com/a", FinalURL: "https://docs.example.com/a"}, []*adapter.Adapter{testFramework()})
	if err != nil {
		t.Fatal(err)
	}
	if err := ir.Validate(doc); err != nil {
		t.Fatalf("Validate() = %v", err)
	}
	return doc
}

// outline renders blocks compactly: kind, then the text, title or label
// that matters for these tests.
func outline(blocks []ir.Block) string {
	var parts []string
	for _, b := range blocks {
		switch b.Kind {
		case ir.KindHeading, ir.KindParagraph:
			parts = append(parts, fmt.Sprintf("%s(%s)", b.Kind, runsText(b.Content)))
		case ir.KindCode:
			parts = append(parts, fmt.Sprintf("code(%s %q %q)", b.Lang, b.Title, b.Text))
		case ir.KindCallout:
			parts = append(parts, fmt.Sprintf("callout(%s %q: %s)", b.Variant, b.Title, outline(b.Children)))
		case ir.KindTabs:
			var tabs []string
			for _, item := range b.Items {
				tabs = append(tabs, item.Label+": "+outline(item.Children))
			}
			parts = append(parts, "tabs("+strings.Join(tabs, " | ")+")")
		default:
			parts = append(parts, string(b.Kind))
		}
	}
	return strings.Join(parts, " ")
}

func TestPlannedPage(t *testing.T) {
	t.Parallel()
	doc := extractTestFramework(t, `<main><p>outside the adapter root</p><div class="content">
		<h1>Title</h1>
		<p class="chrome">Edit this page</p>
		<p class="body">Text.</p>
		<aside class="box tip"><p class="box-title">Tip</p><p>Inside.</p></aside>
		<div class="tabset"><ul><li role="tab" id="t1">npm</li><li role="tab" id="t2">pnpm</li></ul>
			<div role="tabpanel" aria-labelledby="t1"><pre>npm i</pre></div>
			<div role="tabpanel" aria-labelledby="t2" hidden><pre>pnpm i</pre></div></div>
		<div class="tabset"><span role="tab"> </span><div role="tabpanel"><p>Unlabelled.</p></div></div>
		<div class="codebox"><div class="file">main.go</div><button>Copy</button><pre class="language-go"><span class="ln">1</span>package main</pre></div>
		<table class="lines language-sql"><tr><td><pre>1</pre></td><td class="code"><div><pre>x := 1</pre></div></td></tr></table>
		<span class="filename">Filename: src/main.rs</span><pre class="listing">fn main() {}</pre>
	</div></main>`)

	if doc.Extractor != (ir.Extractor{Name: "testfw", Version: "3"}) || len(doc.Warnings) > 0 {
		t.Errorf("extractor = %v, warnings = %v; want testfw 3 and none", doc.Extractor, doc.Warnings)
	}
	want := `heading(Title) paragraph(Text.) callout(tip "Tip": paragraph(Inside.)) ` +
		`tabs(npm: code( "" "npm i") | pnpm: code( "" "pnpm i")) paragraph(Unlabelled.) ` +
		`code(go "main.go" "package main") code(sql "" "x := 1") code( "Filename: src/main.rs" "fn main() {}")`
	if got := outline(doc.Blocks); got != want {
		t.Errorf("blocks =\n%s\nwant\n%s", got, want)
	}
	if doc.Title != "Title" {
		t.Errorf("title = %q", doc.Title)
	}
}

func TestPlanFallsBack(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, body, reason string
	}{
		{name: "no root", body: `<main><p class="body">x</p></main>`, reason: `root "div.content" not found`},
		{name: "required element missing", body: `<main><div class="content"><p>x</p></div></main>`, reason: `".body" not found in the root`},
		{
			name:   "a heading outside the root",
			body:   `<main><h2>Lost</h2><div class="content"><p class="body">x</p></div></main>`,
			reason: `the content root leaves out a heading the generic rules keep: "Lost"`,
		},
		{
			name:   "a code block outside the root",
			body:   `<main><pre>lost()</pre><div class="content"><p class="body">x</p></div></main>`,
			reason: `the content root leaves out a code block the generic rules keep: "lost()"`,
		},
		{
			name:   "removed chrome holds code",
			body:   `<main><div class="content"><p class="body">x</p><div class="chrome"><pre>code()</pre></div></div></main>`,
			reason: "removed chrome <div> holds a code block",
		},
		{
			name:   "the root is inside removed chrome",
			body:   `<main><div class="chrome"><div class="content"><p class="body">x</p></div></div></main>`,
			reason: "the content root is inside removed chrome",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			doc := extractTestFramework(t, tt.body)
			if doc.Extractor.Name != extractorName || len(doc.Warnings) == 0 || doc.Warnings[0].Code != ir.WarningAdapterFallback {
				t.Fatalf("extractor = %v, warnings = %v; want generic with adapter_fallback", doc.Extractor, doc.Warnings)
			}
			if want := "testfw adapter: " + tt.reason + "; used the generic rules"; doc.Warnings[0].Message != want {
				t.Errorf("warning = %q\nwant      %q", doc.Warnings[0].Message, want)
			}
		})
	}

	t.Run("removed chrome outside the root may hold headings", func(t *testing.T) {
		t.Parallel()
		doc := extractTestFramework(t, `<main><div class="chrome"><h2>Feedback</h2></div><div class="content"><p class="body">x</p></div></main>`)
		if doc.Extractor.Name != "testfw" {
			t.Errorf("extractor = %v, warnings = %v", doc.Extractor, doc.Warnings)
		}
	})
}
