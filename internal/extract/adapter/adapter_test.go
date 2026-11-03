package adapter

import (
	"slices"
	"strings"
	"testing"

	"golang.org/x/net/html"
)

func parse(t *testing.T, page string) *html.Node {
	t.Helper()
	doc, err := html.Parse(strings.NewReader(page))
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

// text is the text content of n, or "<nil>".
func text(n *html.Node) string {
	if n == nil {
		return "<nil>"
	}
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return b.String()
}

func TestSelectorsCompile(t *testing.T) {
	t.Parallel()
	for _, a := range All {
		t.Run(a.Name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatal(r)
				}
			}()
			a.compiled()
		})
	}
	names := map[string]bool{}
	for _, a := range All {
		if a.Name == "" || a.Version == "" || names[a.Name] || a.Name == "generic" {
			t.Errorf("adapter %q (version %q) needs a unique name other than generic and a version", a.Name, a.Version)
		}
		names[a.Name] = true
	}
}

func TestBadSelectorPanics(t *testing.T) {
	t.Parallel()
	defer func() {
		if r := recover(); r == nil || !strings.Contains(r.(string), `root "main["`) {
			t.Errorf("recover() = %v, want a panic naming the bad root", r)
		}
	}()
	(&Adapter{Name: "bad", Root: "main["}).compiled()
}

func TestDetect(t *testing.T) {
	t.Parallel()
	fw := func(name string, generators []string, signature string, hosts ...string) *Adapter {
		return &Adapter{Name: name, Version: "1", Generators: generators, Signature: signature, Hosts: hosts, Root: "main"}
	}
	alpha := fw("alpha", []string{"Alpha"}, ".alpha-content", "alpha.example")
	beta := fw("beta", []string{"Beta Docs"}, ".beta-content", "docs.beta.example")
	gamma := fw("gamma", nil, ".alpha-content", "gamma.example")
	adapters := []*Adapter{alpha, beta, gamma}

	tests := []struct {
		name, head, body, host string
		want                   *Adapter
	}{
		{name: "generator, any case", head: `<meta name="generator" content="alpha v2.1">`, host: "x.example", want: alpha},
		{name: "generator inside a list", head: `<meta name="generator" content="kit-1.0, beta docs-4">`, host: "x.example", want: beta},
		{name: "generator name in another case", head: `<meta name="Generator" content="Beta Docs 4">`, host: "x.example", want: beta},
		{name: "one of several generators", head: `<meta name="generator" content="Astro v5"><meta name="generator" content="Beta Docs 1">`, host: "x.example", want: beta},
		{name: "signature", body: `<div class="beta-content"></div>`, host: "x.example", want: beta},
		{name: "host, ignoring www.", host: "www.gamma.example", want: gamma},
		{name: "nothing matches", head: `<meta name="generator" content="Hugo 0.140">`, body: `<main></main>`, host: "x.example"},
		{name: "generator outranks signature", head: `<meta name="generator" content="Beta Docs">`, body: `<div class="alpha-content"></div>`, host: "x.example", want: beta},
		{name: "generator outranks host", head: `<meta name="generator" content="Beta Docs">`, host: "gamma.example", want: beta},
		{name: "signature outranks host", body: `<div class="beta-content"></div>`, host: "alpha.example", want: beta},
		{name: "registry order breaks ties", body: `<div class="alpha-content"></div>`, host: "x.example", want: alpha},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			doc := parse(t, "<html><head>"+tt.head+"</head><body>"+tt.body+"</body></html>")
			if got := Detect(adapters, doc, tt.host); got != tt.want {
				t.Errorf("Detect() = %v, want %v", name(got), name(tt.want))
			}
		})
	}
}

func name(a *Adapter) string {
	if a == nil {
		return "nil"
	}
	return a.Name
}

func TestApplyNeedsRootAndRequired(t *testing.T) {
	t.Parallel()
	a := &Adapter{Name: "t", Version: "1", Root: "div.content", Require: []string{".body"}}

	if _, err := a.Apply(parse(t, `<main><div class="body"></div></main>`)); err == nil || !strings.Contains(err.Error(), `root "div.content" not found`) {
		t.Errorf("Apply() without the root = %v", err)
	}
	if _, err := a.Apply(parse(t, `<div class="content"></div><div class="body"></div>`)); err == nil || !strings.Contains(err.Error(), `".body" not found in the root`) {
		t.Errorf("Apply() with the required element outside the root = %v", err)
	}
	p, err := a.Apply(parse(t, `<div class="content" id="c"><p class="body"></p></div>`))
	if err != nil || attr(p.Root, "id") != "c" {
		t.Errorf("Apply() = %v, %v; want the root", p, err)
	}
}

func TestApplyPlans(t *testing.T) {
	t.Parallel()
	a := &Adapter{
		Name: "t", Version: "1", Root: "main",
		Remove:   ".chrome, .edit",
		Callouts: []Callout{{Selector: "aside.box", Title: ".box-title"}, {Selector: "div.plain"}},
		Tabs:     []Tabs{{Container: ".tabset", Label: "[role=tab]", Panel: "[role=tabpanel]"}},
		Code: []Code{
			{Block: "div.codebox", Title: ".file", NotText: ".ln"},
			{Block: "table.lines", Pre: []string{"td.code pre"}},
			{Block: "pre.listing", TitleBefore: "span.filename", LangAfter: "brush:"},
			{Block: "div.codebox, pre.listing", Title: ".other"},
		},
	}
	doc := parse(t, `<header class="chrome">top</header><main>
		<p class="edit">Edit</p>
		<aside class="box note"><p class="box-title">Note</p><p>Body</p></aside>
		<div class="plain warning"><p>Plain</p></div>
		<div class="tabset">
			<button role="tab" id="t-a">A</button><button role="tab" id="t-b">B</button>
			<div role="tabpanel" aria-labelledby="t-b">second</div><div role="tabpanel" aria-labelledby="t-a" hidden>first</div>
			<div class="tabset"><span role="tab">Inner</span><div role="tabpanel">inner panel</div></div>
		</div>
		<div class="tabset"><span role="tab">One</span><span role="tab">Two</span><div role="tabpanel">1</div><div role="tabpanel">2</div></div>
		<ul class="nav"><li><button role="tab" aria-controls="p1">Outside</button></li></ul>
		<div class="tabset"><div role="tabpanel" id="p1">by controls</div></div>
		<div class="codebox"><div class="file">main.go</div><pre><span class="ln">1</span>package main</pre></div>
		<table class="lines"><tr><td class="nums"><pre>1</pre></td><td class="code"><pre>x</pre></td></tr></table>
		<span class="filename">Filename: src/main.rs</span><pre class="listing brush: Rust">fn main() {}</pre>
	</main>`)

	p, err := a.Apply(doc)
	if err != nil {
		t.Fatal(err)
	}

	removed := 0
	for n := range p.Removed {
		if attr(n, "class") == "chrome" || attr(n, "class") == "edit" {
			removed++
		}
	}
	if removed != 2 {
		t.Errorf("removed %d chrome elements, want 2 (one outside the root)", removed)
	}

	var callouts []string
	for n, c := range p.Callouts {
		callouts = append(callouts, attr(n, "class")+"="+text(c.Title))
	}
	if strings.Join(sorted(callouts), ",") != "box note=Note,plain warning=<nil>" {
		t.Errorf("callouts = %v", sorted(callouts))
	}

	var tabSets []string
	for _, tabs := range p.Tabs {
		var set []string
		for _, tab := range tabs {
			set = append(set, text(tab.Label)+":"+text(tab.Panel))
		}
		tabSets = append(tabSets, strings.Join(set, "|"))
	}
	want := []string{"B:second|A:first", "Inner:inner panel", "One:1|Two:2", "Outside:by controls"}
	// Panels keep page order and pair by aria-labelledby, by aria-controls
	// anywhere in the page, or in order; a nested set keeps its own.
	if got := sorted(tabSets); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("tab sets = %q, want %q", got, want)
	}

	var code []string
	for block, c := range p.Code {
		code = append(code, block.Data+":"+text(c.Pre)+":"+text(c.Title)+":"+c.Lang)
	}
	if got := strings.Join(sorted(code), ","); got != "div:1package main:main.go:,pre:fn main() {}:Filename: src/main.rs:rust,table:x:<nil>:" {
		t.Errorf("code = %s", got)
	}
	if len(p.NotText) != 1 {
		t.Errorf("NotText has %d elements, want the line number", len(p.NotText))
	}

	for _, consumed := range []string{"Note", "A", "B", "Outside", "Filename: src/main.rs"} {
		found := false
		for n := range p.Removed {
			found = found || text(n) == consumed
		}
		if !found {
			t.Errorf("%q, used as a title or label, is not removed from the body", consumed)
		}
	}
}

func sorted(s []string) []string {
	return slices.Sorted(slices.Values(s))
}
