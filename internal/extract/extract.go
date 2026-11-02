// Package extract turns an HTML page into IR blocks with generic rules that
// work on any documentation site. Framework adapters (P07) refine the result.
// docs/adr/0004-ir-and-links.md records the rules.
package extract

import (
	"bytes"
	"fmt"
	"io"
	"net/url"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"github.com/rokumate/ronote/internal/ir"
)

const (
	extractorName    = "generic"
	extractorVersion = "1"
)

// Elements dropped wherever they appear in the content.
var removedTags = map[atom.Atom]bool{
	atom.Nav:      true,
	atom.Aside:    true,
	atom.Script:   true,
	atom.Style:    true,
	atom.Noscript: true,
	atom.Template: true,
	atom.Form:     true,
	atom.Button:   true,
	atom.Iframe:   true,
	atom.Svg:      true,
}

// Extract parses a page (UTF-8) and returns its content as an IR document.
// src says where the page came from and is copied into the result;
// src.FinalURL must be absolute because links and images resolve against it,
// or against <base href> when the page has one.
//
// The content root is the first <main>, else <article>, else [role=main].
// Without one, Extract uses <body> minus header and footer and adds a
// low_confidence warning; it does not guess further.
//
// A page the HTML parser rejects, such as one nested deeper than it allows,
// gives a document with no blocks and a parse_failed warning; Extract only
// fails for a bad src or a read error.
//
// A code block's text is the exact text content of its <pre>. Two changes
// come from HTML parsing itself: the parser drops one newline right after
// <pre>, and it turns CR LF and lone CR into LF.
func Extract(r io.Reader, src ir.Source) (*ir.Doc, error) {
	raw, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("read page: %w", err)
	}
	page, err := url.Parse(src.FinalURL)
	if err != nil || (page.Scheme != "http" && page.Scheme != "https") || page.Host == "" {
		return nil, fmt.Errorf("final URL %q is not an absolute http(s) URL", src.FinalURL)
	}
	out := &ir.Doc{
		IRVersion: ir.Version,
		Source:    src,
		Extractor: ir.Extractor{Name: extractorName, Version: extractorVersion},
	}
	// Invalid UTF-8 becomes U+FFFD, as browsers decode it.
	doc, err := html.Parse(bytes.NewReader(bytes.ToValidUTF8(raw, []byte("\uFFFD"))))
	if err != nil {
		out.Warnings = append(out.Warnings, ir.Warning{Code: ir.WarningParseFailed, Message: err.Error()})
		return out, nil
	}

	e := &extractor{links: newLinker(doc, page), anchors: map[string]bool{}}
	root := findFirst(doc, func(n *html.Node) bool { return n.DataAtom == atom.Main })
	if root == nil {
		root = findFirst(doc, func(n *html.Node) bool { return n.DataAtom == atom.Article })
	}
	if root == nil {
		root = findFirst(doc, func(n *html.Node) bool { return strings.EqualFold(attr(n, "role"), "main") })
	}
	if root == nil {
		root = findFirst(doc, func(n *html.Node) bool { return n.DataAtom == atom.Body })
		e.withoutChrome = true
		out.Warnings = append(out.Warnings, ir.Warning{
			Code:    ir.WarningLowConfidence,
			Message: "no main, article or [role=main] element; used the body without its header and footer",
		})
	}

	out.Source.CanonicalURL = e.canonical(doc)
	out.Blocks = e.blocks(root)
	out.Title = e.title(doc, root)
	ir.AssignIDs(out.Blocks)
	return out, nil
}

type extractor struct {
	links *linker
	// withoutChrome drops header and footer too, when the root is <body>.
	withoutChrome bool
	// anchors holds every heading anchor given out, to keep them unique.
	anchors map[string]bool
}

// skip reports whether n and everything inside it stays out of the result.
func (e *extractor) skip(n *html.Node) bool {
	if n.Type != html.ElementNode {
		return false
	}
	switch {
	case removedTags[n.DataAtom]:
		return true
	case e.withoutChrome && (n.DataAtom == atom.Header || n.DataAtom == atom.Footer):
		return true
	case hasAttr(n, "hidden"), strings.EqualFold(attr(n, "aria-hidden"), "true"):
		return true
	case n.DataAtom == atom.A:
		return isSkipLink(n) || isPermalink(n)
	}
	return false
}

// isSkipLink matches "skip to content" style links.
func isSkipLink(n *html.Node) bool {
	if !strings.HasPrefix(attr(n, "href"), "#") {
		return false
	}
	text := strings.ToLower(collapse(textContent(n)))
	return strings.HasPrefix(text, "skip to") || strings.HasPrefix(text, "skip navigation")
}

// isPermalink matches the "¶" or "#" links docs sites put next to headings.
func isPermalink(n *html.Node) bool {
	if !strings.HasPrefix(attr(n, "href"), "#") {
		return false
	}
	text := collapse(textContent(n))
	if len([]rune(text)) > 2 {
		return false
	}
	for _, r := range text {
		if isWordRune(r) {
			return false
		}
	}
	return true
}

// title is the first <h1> in the content, else the page's <title>.
func (e *extractor) title(doc, root *html.Node) string {
	if h1 := e.findKept(root, func(n *html.Node) bool { return n.DataAtom == atom.H1 }); h1 != nil {
		if t := strings.TrimSpace(runsText(e.inline(h1))); t != "" {
			return t
		}
	}
	if t := findFirst(doc, func(n *html.Node) bool { return n.DataAtom == atom.Title }); t != nil {
		return collapse(textContent(t))
	}
	return ""
}

// canonical is the page's <link rel=canonical> as an absolute URL, if any.
func (e *extractor) canonical(doc *html.Node) string {
	link := findFirst(doc, func(n *html.Node) bool {
		if n.DataAtom != atom.Link {
			return false
		}
		return containsToken(attr(n, "rel"), "canonical")
	})
	if link == nil {
		return ""
	}
	u, ok := e.links.resolve(attr(link, "href"))
	if !ok {
		return ""
	}
	return u.String()
}

// findKept is findFirst that does not look inside skipped elements.
func (e *extractor) findKept(n *html.Node, match func(*html.Node) bool) *html.Node {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type != html.ElementNode || e.skip(c) {
			continue
		}
		if match(c) {
			return c
		}
		if found := e.findKept(c, match); found != nil {
			return found
		}
	}
	return nil
}

// findFirst returns the first element under n, in document order, that
// matches.
func findFirst(n *html.Node, match func(*html.Node) bool) *html.Node {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && match(c) {
			return c
		}
		if found := findFirst(c, match); found != nil {
			return found
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

func hasAttr(n *html.Node, key string) bool {
	for _, a := range n.Attr {
		if a.Namespace == "" && a.Key == key {
			return true
		}
	}
	return false
}

// containsToken reports whether the space-separated list s holds token,
// ignoring case.
func containsToken(s, token string) bool {
	for _, t := range strings.Fields(s) {
		if strings.EqualFold(t, token) {
			return true
		}
	}
	return false
}

// textContent concatenates the text of every node under n.
func textContent(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == html.TextNode {
				b.WriteString(c.Data)
			} else {
				walk(c)
			}
		}
	}
	walk(n)
	return b.String()
}
