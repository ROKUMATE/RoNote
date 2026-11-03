// Package adapter describes documentation frameworks to the extractor: how
// to recognise their pages, where the content is, which chrome to drop, and
// how their callouts, tabs and code blocks are built. An adapter is
// configuration that Apply turns into a Plan for one page; the extractor
// carries the plan out, so no adapter re-implements the generic rules.
// docs/adr/0004-ir-and-links.md records the rules.
package adapter

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/andybalholm/cascadia"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// All lists the adapters in detection order: within each detection phase,
// the first adapter that matches wins.
var All = []*Adapter{starlight, docusaurus, material, sphinx}

// Adapter describes one documentation framework. Selectors are CSS
// selectors as cascadia parses them; one string may hold a comma-separated
// list.
type Adapter struct {
	Name    string
	Version string

	// Detection signals. Detect tries every adapter's Generators first, then
	// every adapter's Signature, then every adapter's Hosts.
	Generators []string // text a <meta name="generator"> contains, in any case
	Signature  string   // matches only this framework's pages
	Hosts      []string // page hosts, without a leading "www."

	// Root selects the content root: its first match in the page. Require
	// lists selectors that must each match inside the root. When Root or one
	// of them does not match, the page is not what the adapter expects.
	Root    string
	Require []string
	// Remove selects chrome, dropped with everything inside it wherever it
	// is. It is the only way an adapter drops content.
	Remove string

	Callouts []Callout
	Tabs     []Tabs
	Code     []Code

	once sync.Once
	sel  selectors
}

// Callout turns matching elements inside the root into callouts. The
// variant comes from the element's class names, as for generic callouts.
type Callout struct {
	Selector string
	// Title selects the title inside the callout; its text becomes the
	// callout's title. Empty means the generic rule: a direct child whose
	// class mentions title or heading.
	Title string
}

// Tabs turns matching containers inside the root into tabs blocks. Labels
// and panels of nested containers are left to those containers. A panel's
// label is the element its aria-labelledby names, else the element anywhere
// in the page whose aria-controls names the panel, else the Label match in
// the same position.
type Tabs struct {
	Container, Label, Panel string
}

// Code turns each element Block matches inside the root into one code
// block. Everything in the element but the code and its title is chrome.
type Code struct {
	Block string
	// Pre lists selectors for the <pre> that holds the code, tried in order
	// inside Block. Empty means Block itself when it is a <pre>, else the
	// first <pre> in it.
	Pre []string
	// Title selects the code's title inside Block. TitleBefore selects the
	// element right before Block when its text is the title.
	Title, TitleBefore string
	// NotText selects elements inside the <pre> that are never text: line
	// numbers, or lines the framework hides and its copy button leaves out.
	NotText string
	// LangAfter is a class token after which the next token of the <pre>'s
	// class names the language, as in class="brush: js".
	LangAfter string
}

// Plan is an adapter applied to one parsed page: what the extractor does
// differently from the generic rules, keyed by element. Elements that a
// callout, tab or code block takes its title or label from are in Removed,
// so their text is not repeated.
type Plan struct {
	Root     *html.Node
	Removed  map[*html.Node]bool
	Callouts map[*html.Node]CalloutPlan
	Tabs     map[*html.Node][]Tab
	Code     map[*html.Node]CodePlan
	NotText  map[*html.Node]bool
}

// Claims reports whether the plan converts n itself.
func (p *Plan) Claims(n *html.Node) bool {
	_, callout := p.Callouts[n]
	_, tabs := p.Tabs[n]
	_, code := p.Code[n]
	return callout || tabs || code
}

// CalloutPlan is a planned callout. Title is nil when the generic title rule
// applies.
type CalloutPlan struct {
	Title *html.Node
}

// Tab is one planned tab. Label is nil when the tab has none.
type Tab struct {
	Label, Panel *html.Node
}

// CodePlan is a planned code block. Title is nil and Lang is "" when the
// generic rules decide them.
type CodePlan struct {
	Pre, Title *html.Node
	Lang       string
}

// Detect returns the adapter for a page, or nil. It tries every adapter's
// generators, then every adapter's signature, then every adapter's hosts, so
// a page's own generator tag outranks its structure, which outranks where it
// is hosted. A generator matches when a generator tag contains it, as in
// "mkdocs-1.6.1, mkdocs-material-9.6.14". host is the page's host name.
func Detect(adapters []*Adapter, doc *html.Node, host string) *Adapter {
	var generators []string
	for _, meta := range cascadia.QueryAll(doc, metaGenerator) {
		generators = append(generators, strings.ToLower(attr(meta, "content")))
	}
	for _, a := range adapters {
		for _, name := range a.Generators {
			if slices.ContainsFunc(generators, func(g string) bool { return strings.Contains(g, strings.ToLower(name)) }) {
				return a
			}
		}
	}
	for _, a := range adapters {
		if s := a.compiled(); s.signature != nil && cascadia.Query(doc, s.signature) != nil {
			return a
		}
	}
	host = strings.TrimPrefix(strings.ToLower(host), "www.")
	for _, a := range adapters {
		if slices.Contains(a.Hosts, host) {
			return a
		}
	}
	return nil
}

var metaGenerator = cascadia.MustCompile(`meta[name="generator" i]`)

// Apply plans the adapter for a parsed page. It fails when the root or a
// required element is missing.
func (a *Adapter) Apply(doc *html.Node) (*Plan, error) {
	s := a.compiled()
	root := cascadia.Query(doc, s.root)
	if root == nil {
		return nil, fmt.Errorf("root %q not found", a.Root)
	}
	for i, req := range s.require {
		if cascadia.Query(root, req) == nil {
			return nil, fmt.Errorf("%q not found in the root", a.Require[i])
		}
	}

	p := &Plan{
		Root:     root,
		Removed:  map[*html.Node]bool{},
		Callouts: map[*html.Node]CalloutPlan{},
		Tabs:     map[*html.Node][]Tab{},
		Code:     map[*html.Node]CodePlan{},
		NotText:  map[*html.Node]bool{},
	}
	if s.remove != nil {
		for _, n := range cascadia.QueryAll(doc, s.remove) {
			p.Removed[n] = true
		}
	}
	for _, c := range s.callouts {
		for _, n := range cascadia.QueryAll(root, c.selector) {
			plan := CalloutPlan{}
			if c.title != nil {
				plan.Title = cascadia.Query(n, c.title)
				p.consume(plan.Title)
			}
			p.Callouts[n] = plan
		}
	}
	if len(s.tabs) > 0 {
		aria := ariaIndex(doc)
		for _, t := range s.tabs {
			for _, container := range cascadia.QueryAll(root, t.container) {
				p.Tabs[container] = p.pairTabs(container, t, aria)
			}
		}
	}
	for i, c := range s.code {
		for _, block := range cascadia.QueryAll(root, c.block) {
			p.planCode(block, c, a.Code[i].LangAfter)
		}
	}
	return p, nil
}

// consume marks n, an element whose text the plan takes as a title or
// label, as removed from the body. n may be nil.
func (p *Plan) consume(n *html.Node) {
	if n != nil {
		p.Removed[n] = true
	}
}

func (p *Plan) pairTabs(container *html.Node, t tabsSelectors, aria ariaLookup) []Tab {
	own := func(nodes []*html.Node) []*html.Node {
		var out []*html.Node
		for _, n := range nodes {
			if closest(n.Parent, t.container) == container {
				out = append(out, n)
			}
		}
		return out
	}
	var labels []*html.Node
	if t.label != nil {
		labels = own(cascadia.QueryAll(container, t.label))
	}
	var tabs []Tab
	for i, panel := range own(cascadia.QueryAll(container, t.panel)) {
		label := aria.ids[attr(panel, "aria-labelledby")]
		if label == nil {
			label = aria.controls[attr(panel, "id")]
		}
		if label == nil && i < len(labels) {
			label = labels[i]
		}
		p.consume(label)
		tabs = append(tabs, Tab{Label: label, Panel: panel})
	}
	return tabs
}

func (p *Plan) planCode(block *html.Node, c codeSelectors, langAfter string) {
	pre := block
	if block.DataAtom != atom.Pre {
		pre = nil
		for _, sel := range c.pre {
			if pre = cascadia.Query(block, sel); pre != nil {
				break
			}
		}
	}
	if pre == nil {
		return
	}
	plan := CodePlan{Pre: pre, Lang: langAfterToken(attr(pre, "class"), langAfter)}
	if c.title != nil {
		plan.Title = cascadia.Query(block, c.title)
	}
	if c.titleBefore != nil {
		if prev := previousElement(block); prev != nil && c.titleBefore.Match(prev) {
			plan.Title = prev
			p.consume(prev)
		}
	}
	if c.notText != nil {
		for _, n := range cascadia.QueryAll(pre, c.notText) {
			p.NotText[n] = true
		}
	}
	p.Code[block] = plan
}

// langAfterToken returns the class token after the token after, lowercased,
// or "".
func langAfterToken(class, after string) string {
	if after == "" {
		return ""
	}
	tokens := strings.Fields(class)
	if i := slices.Index(tokens, after); i >= 0 && i+1 < len(tokens) {
		return strings.ToLower(tokens[i+1])
	}
	return ""
}

// ariaLookup finds tab labels by ARIA attributes anywhere in the page.
type ariaLookup struct {
	ids      map[string]*html.Node // id -> element
	controls map[string]*html.Node // aria-controls value -> element
}

func ariaIndex(doc *html.Node) ariaLookup {
	l := ariaLookup{ids: map[string]*html.Node{}, controls: map[string]*html.Node{}}
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == html.ElementNode {
				if id := attr(c, "id"); id != "" && l.ids[id] == nil {
					l.ids[id] = c
				}
				if ctl := attr(c, "aria-controls"); ctl != "" && l.controls[ctl] == nil {
					l.controls[ctl] = c
				}
			}
			walk(c)
		}
	}
	walk(doc)
	return l
}

// closest returns n or its nearest ancestor that m matches, or nil.
func closest(n *html.Node, m cascadia.Matcher) *html.Node {
	for ; n != nil; n = n.Parent {
		if n.Type == html.ElementNode && m.Match(n) {
			return n
		}
	}
	return nil
}

func previousElement(n *html.Node) *html.Node {
	for s := n.PrevSibling; s != nil; s = s.PrevSibling {
		if s.Type == html.ElementNode {
			return s
		}
	}
	return nil
}

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Namespace == "" && a.Key == key {
			return a.Val
		}
	}
	return ""
}

// selectors are an adapter's compiled selectors; nil where a field is empty.
type selectors struct {
	signature, root, remove cascadia.Matcher
	require                 []cascadia.Matcher
	callouts                []calloutSelectors
	tabs                    []tabsSelectors
	code                    []codeSelectors
}

type calloutSelectors struct{ selector, title cascadia.Matcher }

type tabsSelectors struct{ container, label, panel cascadia.Matcher }

type codeSelectors struct {
	block, title, titleBefore, notText cascadia.Matcher
	pre                                []cascadia.Matcher
}

// compiled compiles the adapter's selectors once. A selector that does not
// parse is a bug in the adapter, so it panics; TestSelectorsCompile runs it
// for every adapter.
func (a *Adapter) compiled() *selectors {
	a.once.Do(func() {
		var errs []error
		optional := func(field, sel string) cascadia.Matcher {
			if sel == "" {
				return nil
			}
			m, err := cascadia.ParseGroup(sel)
			if err != nil {
				errs = append(errs, fmt.Errorf("%s %q: %w", field, sel, err))
				return nil
			}
			return m
		}
		compile := func(field, sel string) cascadia.Matcher {
			if sel == "" {
				errs = append(errs, fmt.Errorf("%s is required", field))
			}
			return optional(field, sel)
		}
		s := &a.sel
		s.signature = optional("signature", a.Signature)
		s.root = compile("root", a.Root)
		s.remove = optional("remove", a.Remove)
		for _, r := range a.Require {
			s.require = append(s.require, compile("require", r))
		}
		for _, c := range a.Callouts {
			s.callouts = append(s.callouts, calloutSelectors{compile("callout", c.Selector), optional("callout title", c.Title)})
		}
		for _, t := range a.Tabs {
			s.tabs = append(s.tabs, tabsSelectors{compile("tabs container", t.Container), optional("tab label", t.Label), compile("tab panel", t.Panel)})
		}
		for _, c := range a.Code {
			cs := codeSelectors{
				block:       compile("code block", c.Block),
				title:       optional("code title", c.Title),
				titleBefore: optional("code title before", c.TitleBefore),
				notText:     optional("code not text", c.NotText),
			}
			for _, pre := range c.Pre {
				cs.pre = append(cs.pre, compile("code pre", pre))
			}
			if len(cs.pre) == 0 {
				cs.pre = []cascadia.Matcher{anyPre}
			}
			s.code = append(s.code, cs)
		}
		if len(errs) > 0 {
			panic(fmt.Sprintf("adapter %s: %v", a.Name, errors.Join(errs...)))
		}
	})
	return &a.sel
}

var anyPre = cascadia.MustCompile("pre")
