package extract_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/rokumate/ronote/internal/extract"
	"github.com/rokumate/ronote/internal/ir"
)

const pageURL = "https://docs.example.com/guide/start"

func extractDoc(t *testing.T, html string) *ir.Doc {
	t.Helper()
	doc, err := extract.Extract(strings.NewReader(html), ir.Source{URL: pageURL, FinalURL: pageURL})
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	if err := ir.Validate(doc); err != nil {
		t.Fatalf("Validate() = %v", err)
	}
	return doc
}

// summary renders blocks one per line in a compact form for assertions:
// marks as text{bold,code}, links as [text](kind href).
func summary(blocks []ir.Block) []string {
	var out []string
	for _, b := range blocks {
		out = append(out, blockLine(b))
	}
	return out
}

func blockLine(b ir.Block) string {
	switch b.Kind {
	case ir.KindHeading:
		return fmt.Sprintf("h%d#%s %s", b.Level, b.Anchor, runs(b.Content))
	case ir.KindParagraph:
		return "p " + runs(b.Content)
	case ir.KindCode:
		return fmt.Sprintf("code(%s) %q", b.Lang, b.Text)
	case ir.KindList:
		tag := "ul"
		if b.Ordered {
			tag = "ol"
		}
		var items []string
		for _, item := range b.Items {
			items = append(items, strings.Join(summary(item), " / "))
		}
		return tag + "[" + strings.Join(items, "; ") + "]"
	case ir.KindTable:
		return "table head=" + cells(b.Head) + " rows=" + cells(b.Rows)
	case ir.KindCallout:
		return fmt.Sprintf("callout(%s %q)[%s]", b.Variant, b.Title, strings.Join(summary(b.Children), " / "))
	case ir.KindImage:
		return fmt.Sprintf("img %s alt=%q caption=%q", b.Src, b.Alt, runs(b.Caption))
	case ir.KindQuote:
		return "quote[" + strings.Join(summary(b.Children), " / ") + "]"
	default:
		return string(b.Kind)
	}
}

func runs(rs []ir.Inline) string {
	var b strings.Builder
	for _, r := range rs {
		text := r.Text
		if len(r.Marks) > 0 {
			var marks []string
			for _, m := range r.Marks {
				marks = append(marks, string(m))
			}
			text += "{" + strings.Join(marks, ",") + "}"
		}
		if r.Link != nil {
			text = fmt.Sprintf("[%s](%s %s)", text, r.Link.Kind, r.Link.Href)
		}
		b.WriteString(text)
	}
	return b.String()
}

func cells(rows [][]ir.Cell) string {
	var out []string
	for _, row := range rows {
		var cs []string
		for _, c := range row {
			cs = append(cs, runs(c))
		}
		out = append(out, strings.Join(cs, "|"))
	}
	return "[" + strings.Join(out, "; ") + "]"
}

func TestExtract(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		html string
		want []string
	}{
		{
			name: "main wins over article",
			html: `<article><p>Teaser</p></article><main><p>Main text</p></main>`,
			want: []string{"p Main text"},
		},
		{
			name: "article when there is no main",
			html: `<div><p>Outside</p></div><article><p>Article text</p></article>`,
			want: []string{"p Article text"},
		},
		{
			name: "role=main when there is no main or article",
			html: `<div role="main"><p>Role text</p></div><p>Outside</p>`,
			want: []string{"p Role text"},
		},
		{
			name: "chrome is removed inside main",
			html: `<main><nav><p>Nav</p></nav><aside><p>Aside</p></aside><script>x()</script><style>p{}</style>
				<noscript>No JS</noscript><template><p>T</p></template><form><p>Form</p></form><button>Click</button>
				<iframe src="https://x.test"></iframe><svg><text>SVG</text></svg><p hidden>Hidden</p>
				<p aria-hidden="true">Aria hidden</p><a href="#content">Skip to content</a><p>Kept</p>
				<header><p>Header in main is kept</p></header></main>`,
			want: []string{"p Kept", "p Header in main is kept"},
		},
		{
			name: "whitespace collapses but no-break spaces stay",
			html: "<main><p>  one \n\t two  three  </p></main>",
			want: []string{"p one two  three"},
		},
		{
			name: "br inside a paragraph",
			html: `<main><p>line one   <br>   line two</p></main>`,
			want: []string{"p line one\nline two"},
		},
		{
			name: "stray inline content becomes paragraphs",
			html: `<main><div>Before <b>bold</b><p>Para</p>after</div></main>`,
			want: []string{"p Before bold{bold}", "p Para", "p after"},
		},
		{
			name: "marks",
			html: `<main><p><b>b</b><strong>s</strong><i>i</i><em>e</em><code>c</code><kbd>k</kbd>` +
				`<s>x</s><del>d</del><sub>2</sub><sup>3</sup><b><i>both</i></b><b><strong>once</strong></b></p></main>`,
			want: []string{"p bs{bold}ie{italic}c{code}k{kbd}xd{strike}2{sub}3{sup}both{bold,italic}once{bold}"},
		},
		{
			name: "headings get anchors",
			html: `<main><h1 id="top">Title</h1><h2>Getting started</h2><h2>Getting started</h2>` +
				`<h3><a name="old"></a>Named</h3><h2 id="x">Permalink <a class="headerlink" href="#x">¶</a></h2>` +
				`<section id="setup"><span id="target"></span><h2>Install</h2><p>x</p></section><section id="later"><p>y</p><h2>Not first</h2></section></main>`,
			want: []string{
				"h1#top Title", "h2#getting-started Getting started", "h2#getting-started-2 Getting started", "h3#old Named",
				"h2#x Permalink", "h2#setup Install", "p x", "p y", "h2#not-first Not first",
			},
		},
		{
			name: "lists",
			html: `<main><ul><li>One</li><li>Two<ol><li>Nested</li></ol></li><li></li></ul>` +
				`<dl><dt>Term</dt><dd>Meaning</dd><dd><p>More</p></dd></dl></main>`,
			want: []string{"ul[p One; p Two / ol[p Nested]]", "ul[p Term{bold} / p Meaning / p More]"},
		},
		{
			name: "tables",
			html: `<main><table><thead><tr><th>Name</th><th>Type</th></tr></thead>` +
				`<tbody><tr><td><code>id</code></td><td>int <em>required</em></td></tr></tbody></table>` +
				`<table><tr><th>A</th></tr><tr><td><p>x</p><p>y</p></td></tr></table></main>`,
			want: []string{"table head=[Name|Type] rows=[id{code}|int required{italic}]", "table head=[A] rows=[x y]"},
		},
		{
			name: "quote and divider",
			html: `<main><blockquote><p>Quoted</p></blockquote><hr></main>`,
			want: []string{"quote[p Quoted]", "divider"},
		},
		{
			name: "callouts",
			html: `<main><div class="admonition warning"><p class="admonition-title">Careful</p><p>Hot.</p></div>` +
				`<div class="markdown-alert markdown-alert-tip"><p class="markdown-alert-title">Tip</p><p>Try it.</p></div>` +
				`<div class="alert alert-danger" role="alert">Do not.</div><div role="note"><p>Plain note</p></div>` +
				`<div class="theme-admonition theme-admonition-info alert alert--info"><div class="admonitionHeading_x">info</div><div><p>Docusaurus</p></div></div></main>`,
			want: []string{
				`callout(warning "Careful")[p Hot.]`,
				`callout(tip "Tip")[p Try it.]`,
				`callout(danger "")[p Do not.]`,
				`callout(note "")[p Plain note]`,
				`callout(info "info")[p Docusaurus]`,
			},
		},
		{
			name: "details",
			html: `<main><details><summary>More</summary><p>Hidden detail</p></details></main>`,
			want: []string{"p More{bold}", "p Hidden detail"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := summary(extractDoc(t, tt.html).Blocks); !slices.Equal(got, tt.want) {
				t.Errorf("blocks:\n got %q\nwant %q", got, tt.want)
			}
		})
	}
}

func TestCodeBlocks(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		html string
		want string
	}{
		{name: "tabs and trailing spaces are kept", html: "<pre>func f() {\n\treturn  \n}\n</pre>", want: `code() "func f() {\n\treturn  \n}\n"`},
		{name: "the parser drops one leading newline", html: "<pre>\n\nfirst</pre>", want: `code() "\nfirst"`},
		{name: "br becomes a newline", html: "<pre>a<br>b<br/>c</pre>", want: `code() "a\nb\nc"`},
		{name: "highlight spans flatten", html: `<pre><code><span class="k">if</span> <span><span>x</span> &lt; y</span> {}</code></pre>`, want: `code() "if x < y {}"`},
		{name: "entities are decoded", html: "<pre>&lt;a href=&quot;x&quot;&gt;&amp;amp;&lt;/a&gt;</pre>", want: `code() "<a href=\"x\">&amp;</a>"`},
		{name: "CRLF becomes LF", html: "<pre>a\r\nb\rc</pre>", want: `code() "a\nb\nc"`},
		{name: "language- class on code", html: `<pre><code class="language-go">x</code></pre>`, want: `code(go) "x"`},
		{name: "lang- class on pre", html: `<pre class="lang-Python">x</pre>`, want: `code(python) "x"`},
		{name: "highlight- class on an ancestor", html: `<div class="highlight-shell"><div class="highlight"><pre>x</pre></div></div>`, want: `code(shell) "x"`},
		{name: "data-lang", html: `<pre data-lang="SQL">x</pre>`, want: `code(sql) "x"`},
		{name: "no language", html: `<pre class="chroma">x</pre>`, want: `code() "x"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := summary(extractDoc(t, "<main>"+tt.html+"</main>").Blocks)
			if len(got) != 1 || got[0] != tt.want {
				t.Errorf("blocks = %q, want [%q]", got, tt.want)
			}
		})
	}

	t.Run("an empty pre is dropped", func(t *testing.T) {
		t.Parallel()
		if got := summary(extractDoc(t, "<main><pre>\n</pre><p>after</p></main>").Blocks); !slices.Equal(got, []string{"p after"}) {
			t.Errorf("blocks = %q", got)
		}
	})
}

func TestLinksAndImages(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		head string
		body string
		want string
	}{
		{name: "relative same-domain link", body: `<a href="../api/x">x</a>`, want: "p [x](same_domain https://docs.example.com/api/x)"},
		{name: "www. is ignored", body: `<a href="https://www.docs.example.com/y">y</a>`, want: "p [y](same_domain https://www.docs.example.com/y)"},
		{name: "other host", body: `<a href="https://example.com/">z</a>`, want: "p [z](external https://example.com/)"},
		{name: "subdomain is another host", body: `<a href="https://api.docs.example.com/">s</a>`, want: "p [s](external https://api.docs.example.com/)"},
		{name: "fragment link", body: `<a href="#install">i</a>`, want: "p [i](anchor #install)"},
		{name: "this page with a fragment", body: `<a href="/guide/start#step-2">s</a>`, want: "p [s](anchor #step-2)"},
		{name: "another page with a fragment", body: `<a href="other#step-2">o</a>`, want: "p [o](same_domain https://docs.example.com/guide/other#step-2)"},
		{name: "mailto", body: `<a href="mailto:docs@example.com">m</a>`, want: "p [m](external mailto:docs@example.com)"},
		{name: "javascript keeps only the text", body: `<a href="javascript:alert(1)">j</a>`, want: "p j"},
		{name: "base href is used for resolving", head: `<base href="https://cdn.example.net/v2/">`, body: `<a href="page">p</a>`, want: "p [p](external https://cdn.example.net/v2/page)"},
		{name: "relative base href", head: `<base href="/v3/">`, body: `<a href="page">p</a>`, want: "p [p](same_domain https://docs.example.com/v3/page)"},
		{name: "image src is absolute", body: `<img src="img/a.png" alt=" A  diagram ">`, want: `img https://docs.example.com/guide/img/a.png alt="A diagram" caption=""`},
		{name: "lazy image", body: `<img data-src="/lazy.png" alt="">`, want: `img https://docs.example.com/lazy.png alt="" caption=""`},
		{name: "data image is dropped", body: `<img src="data:image/png;base64,AA" alt="x"><p>after</p>`, want: "p after"},
		{name: "figure caption", body: `<figure><img src="/f.png" alt="F"><figcaption>Figure <em>1</em></figcaption></figure>`, want: `img https://docs.example.com/f.png alt="F" caption="Figure 1{italic}"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := summary(extractDoc(t, "<head>"+tt.head+"</head><main><p>"+tt.body+"</p></main>").Blocks)
			if len(got) == 0 || got[0] != tt.want {
				t.Errorf("blocks = %q, want first %q", got, tt.want)
			}
		})
	}

	t.Run("an image inside a paragraph splits it", func(t *testing.T) {
		t.Parallel()
		got := summary(extractDoc(t, `<main><p>before <img src="/i.png" alt="i"> after</p></main>`).Blocks)
		want := []string{"p before", `img https://docs.example.com/i.png alt="i" caption=""`, "p after"}
		if !slices.Equal(got, want) {
			t.Errorf("blocks = %q, want %q", got, want)
		}
	})
}

func TestDocument(t *testing.T) {
	t.Parallel()

	t.Run("title is the first h1 in main", func(t *testing.T) {
		t.Parallel()
		doc := extractDoc(t, `<title>Site title</title><h1>Outside</h1><main><h1>The <code>Page</code></h1></main>`)
		if doc.Title != "The Page" {
			t.Errorf("Title = %q", doc.Title)
		}
	})
	t.Run("title leaves out a heading permalink", func(t *testing.T) {
		t.Parallel()
		doc := extractDoc(t, `<main><h1>Tools<a class="headerlink" href="#tools">¶</a></h1></main>`)
		if doc.Title != "Tools" {
			t.Errorf("Title = %q", doc.Title)
		}
	})
	t.Run("title falls back to the title element", func(t *testing.T) {
		t.Parallel()
		doc := extractDoc(t, `<title> Site   title </title><main><p>x</p></main>`)
		if doc.Title != "Site title" {
			t.Errorf("Title = %q", doc.Title)
		}
	})
	t.Run("canonical URL is resolved", func(t *testing.T) {
		t.Parallel()
		doc := extractDoc(t, `<link rel="canonical" href="/guide/start/"><main><p>x</p></main>`)
		if doc.Source.CanonicalURL != "https://docs.example.com/guide/start/" {
			t.Errorf("CanonicalURL = %q", doc.Source.CanonicalURL)
		}
	})
	t.Run("without main the body is used, with a warning", func(t *testing.T) {
		t.Parallel()
		doc := extractDoc(t, `<header><p>Logo</p></header><div><p>Body text</p></div><footer><p>Legal</p></footer>`)
		if got := summary(doc.Blocks); !slices.Equal(got, []string{"p Body text"}) {
			t.Errorf("blocks = %q", got)
		}
		if len(doc.Warnings) != 1 || doc.Warnings[0].Code != ir.WarningLowConfidence {
			t.Errorf("Warnings = %+v, want one low_confidence", doc.Warnings)
		}
	})
	t.Run("a relative final URL is refused", func(t *testing.T) {
		t.Parallel()
		if _, err := extract.Extract(strings.NewReader("<p>x</p>"), ir.Source{FinalURL: "/x"}); err == nil {
			t.Error("Extract() error = nil, want an error")
		}
	})
	t.Run("extractor and IDs are set", func(t *testing.T) {
		t.Parallel()
		doc := extractDoc(t, `<main><p>a</p><p>a</p></main>`)
		if doc.Extractor.Name != "generic" || doc.IRVersion != ir.Version {
			t.Errorf("doc = %+v", doc)
		}
		if doc.Blocks[1].ID != doc.Blocks[0].ID+"-2" {
			t.Errorf("IDs %q, %q", doc.Blocks[0].ID, doc.Blocks[1].ID)
		}
	})
}
