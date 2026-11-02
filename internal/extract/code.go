package extract

import (
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// preText is the exact text content of a <pre>: text nodes concatenated in
// order, so highlight spans flatten, and <br> as "\n". Nothing is trimmed.
func preText(pre *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			switch {
			case c.Type == html.TextNode:
				b.WriteString(c.Data)
			case c.Type == html.ElementNode && c.DataAtom == atom.Br:
				b.WriteByte('\n')
			default:
				walk(c)
			}
		}
	}
	walk(pre)
	return b.String()
}

var langPrefixes = []string{"language-", "lang-", "highlight-"}

// codeLang finds a code block's language: a language-x, lang-x or highlight-x
// class on the <pre>, its <code> child or its two nearest ancestors, else a
// data-lang attribute on the <pre> or <code>. It is "" when there is none.
func codeLang(pre *html.Node) string {
	candidates := []*html.Node{pre}
	for c := pre.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode {
			if c.DataAtom == atom.Code {
				candidates = append(candidates, c)
			}
			break
		}
	}
	tagged := candidates
	for p, depth := pre.Parent, 0; p != nil && p.Type == html.ElementNode && depth < 2; p, depth = p.Parent, depth+1 {
		candidates = append(candidates, p)
	}

	for _, n := range candidates {
		for _, class := range strings.Fields(attr(n, "class")) {
			for _, prefix := range langPrefixes {
				if lang, ok := strings.CutPrefix(strings.ToLower(class), prefix); ok && lang != "" {
					return lang
				}
			}
		}
	}
	for _, n := range tagged {
		if lang := strings.TrimSpace(attr(n, "data-lang")); lang != "" {
			return strings.ToLower(lang)
		}
	}
	return ""
}
