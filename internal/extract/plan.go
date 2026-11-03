package extract

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"github.com/rokumate/ronote/internal/extract/adapter"
	"github.com/rokumate/ronote/internal/ir"
)

// checkPlan reports why plan would return less content than the generic
// rules, or nil. An adapter may only drop what its Remove list names, so the
// plan fails when its root is inside removed chrome, when removed chrome in
// the root holds a <pre>, or when the generic root keeps a heading or <pre>
// that the plan's root does not hold and no removal accounts for. When the
// generic rules found no root, root is <body> and its header and footer do
// not count.
func (e *extractor) checkPlan(plan *adapter.Plan, root *html.Node, bodyOnly bool) error {
	if removedAncestor(plan, plan.Root) != nil {
		return errors.New("the content root is inside removed chrome")
	}
	if n := findFirst(plan.Root, func(n *html.Node) bool {
		return plan.Removed[n] && findFirst(n, func(c *html.Node) bool { return c.DataAtom == atom.Pre }) != nil
	}); n != nil {
		return fmt.Errorf("removed chrome <%s> holds a code block", n.Data)
	}

	var lost *html.Node
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		for c := n.FirstChild; c != nil && lost == nil; c = c.NextSibling {
			if c.Type != html.ElementNode || e.skip(c) ||
				(bodyOnly && (c.DataAtom == atom.Header || c.DataAtom == atom.Footer)) {
				continue
			}
			switch c.DataAtom {
			case atom.H1, atom.H2, atom.H3, atom.H4, atom.H5, atom.H6, atom.Pre:
				if !inside(c, plan.Root) && removedAncestor(plan, c) == nil {
					lost = c
				}
			case atom.Table:
				// Cells hold inline content only; a <pre> there is no block.
			default:
				walk(c)
			}
		}
	}
	walk(root)
	if lost != nil {
		what := "a heading"
		if lost.DataAtom == atom.Pre {
			what = "a code block"
		}
		return fmt.Errorf("the content root leaves out %s the generic rules keep: %q", what, clip(collapse(textContent(lost))))
	}
	return nil
}

// removedAncestor returns n or its nearest ancestor that plan removes, or
// nil.
func removedAncestor(plan *adapter.Plan, n *html.Node) *html.Node {
	for ; n != nil; n = n.Parent {
		if plan.Removed[n] {
			return n
		}
	}
	return nil
}

// inside reports whether n is root or inside it.
func inside(n, root *html.Node) bool {
	for ; n != nil; n = n.Parent {
		if n == root {
			return true
		}
	}
	return false
}

func clip(s string) string {
	if r := []rune(s); len(r) > 60 {
		return string(r[:60]) + "…"
	}
	return s
}

// planned converts n when the plan says how, as a code block, a tabs block
// or a callout; ok is false when the plan says nothing about n.
func (e *extractor) planned(n *html.Node) (blocks []ir.Block, ok bool) {
	if e.plan == nil {
		return nil, false
	}
	if c, ok := e.plan.Code[n]; ok {
		return blockList(e.plannedCode(n, c)), true
	}
	if tabs, ok := e.plan.Tabs[n]; ok {
		return e.tabs(tabs), true
	}
	if c, ok := e.plan.Callouts[n]; ok {
		return blockList(e.calloutBlock(n, c.Title)), true
	}
	return nil, false
}

func blockList(b *ir.Block) []ir.Block {
	if b == nil {
		return nil
	}
	return []ir.Block{*b}
}

// plannedCode builds a code block from the plan's <pre> inside block. The
// generic rules supply the title and language the plan leaves open, and a
// language class on block itself counts too, since a layout table can put
// the <pre> far below it. A title that is or holds a link to another page
// keeps that link.
func (e *extractor) plannedCode(block *html.Node, c adapter.CodePlan) *ir.Block {
	b := e.code(c.Pre)
	if b == nil {
		return nil
	}
	if len(c.Markup) > 0 {
		if b.Text = stripMarkup(b.Text, c.Markup); b.Text == "" {
			return nil
		}
	}
	if c.Title != nil {
		b.Title = collapse(textContent(c.Title))
		b.TitleLink = e.titleLink(c.Title)
	}
	switch {
	case c.Lang != "":
		b.Lang = c.Lang
	case b.Lang == "":
		b.Lang = classLang(block)
	}
	return b
}

// tabs builds a tabs block from planned tabs, dropping empty panels. If a
// panel with content has no label, the panels' blocks follow one another
// instead: a label is never made up. A panel the plan converts itself, such
// as one that is a code block's wrapper, is converted whole.
func (e *extractor) tabs(tabs []adapter.Tab) []ir.Block {
	var items []ir.Item
	labelled := true
	for _, t := range tabs {
		children, planned := e.planned(t.Panel)
		if !planned {
			children = e.blocks(t.Panel)
		}
		if len(children) == 0 {
			continue
		}
		var label string
		if t.Label != nil {
			label = collapse(textContent(t.Label))
		}
		labelled = labelled && label != ""
		items = append(items, ir.Item{Label: label, Children: children})
	}
	if len(items) == 0 {
		return nil
	}
	if !labelled {
		var out []ir.Block
		for _, item := range items {
			out = append(out, item.Children...)
		}
		return out
	}
	return []ir.Block{{Kind: ir.KindTabs, Items: items}}
}

// titleLink is the link of a code title: title itself or the first link in
// it, when it points to another page.
func (e *extractor) titleLink(title *html.Node) *ir.Link {
	a := title
	if a.DataAtom != atom.A {
		a = findFirst(title, func(n *html.Node) bool { return n.DataAtom == atom.A && hasAttr(n, "href") })
	}
	if a == nil || !hasAttr(a, "href") {
		return nil
	}
	if l := e.links.link(attr(a, "href")); l != nil && (l.Kind == ir.LinkSameDomain || l.Kind == ir.LinkExternal) {
		return l
	}
	return nil
}

// stripMarkup removes the opening and closing tags named in tags from code
// text that holds them as literal characters, keeping what is between them.
func stripMarkup(text string, tags []string) string {
	names := make([]string, len(tags))
	for i, t := range tags {
		names[i] = regexp.QuoteMeta(t)
	}
	re := regexp.MustCompile(`(?i)</?(?:` + strings.Join(names, "|") + `)(?:\s[^<>]*)?>`)
	return re.ReplaceAllString(text, "")
}
