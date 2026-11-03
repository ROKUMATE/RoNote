package extract

import (
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"github.com/rokumate/ronote/internal/ir"
)

// code converts a <pre> to a code block, or nil when it has no text.
func (e *extractor) code(pre *html.Node) *ir.Block {
	text := codeText(pre)
	if text == "" {
		return nil
	}
	return &ir.Block{Kind: ir.KindCode, Lang: codeLang(pre), Title: codeTitle(pre), Text: text}
}

// codeText is a code block's text under the verbatim rule: what the page's
// copy button puts on the clipboard when the page carries that text, else the
// text content of the <pre>.
func codeText(pre *html.Node) string {
	if text, ok := copyPayload(pre); ok {
		return text
	}
	return preText(pre)
}

// preText is the exact text content of a <pre>: text nodes concatenated in
// order, so highlight spans flatten, and <br> as "\n". Nothing is trimmed.
// Parts that are never text are left out; see neverText.
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
			case neverText(c):
			default:
				walk(c)
			}
		}
	}
	walk(pre)
	return b.String()
}

// neverText reports whether an element inside a <pre> is drawn next to the
// code rather than being part of it: anything styled user-select: none, such
// as a shell prompt a highlighter renders apart from the command, and
// Pygments line numbers (class linenos).
func neverText(n *html.Node) bool {
	if n.Type != html.ElementNode {
		return false
	}
	style := strings.ToLower(strings.Join(strings.Fields(attr(n, "style")), ""))
	return strings.Contains(style, "user-select:none") || containsToken(attr(n, "class"), "linenos")
}

// codeFrame returns the Expressive Code frame around pre, a <figure
// class="frame"> that holds the code, its title bar and its copy button, or
// nil.
func codeFrame(pre *html.Node) *html.Node {
	if p := pre.Parent; p != nil && p.DataAtom == atom.Figure && containsToken(attr(p, "class"), "frame") {
		return p
	}
	return nil
}

// framedPre returns the <pre> of an Expressive Code frame, or nil when n is
// not one.
func framedPre(n *html.Node) *html.Node {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.DataAtom == atom.Pre && codeFrame(c) == n {
			return c
		}
	}
	return nil
}

// copyPayload is the text pre's copy button puts on the clipboard, when the
// page carries it. Expressive Code keeps it in the button's data-code
// attribute with U+007F for each line break, because its lines are separate
// elements with no newline between them.
func copyPayload(pre *html.Node) (string, bool) {
	frame := codeFrame(pre)
	if frame == nil {
		return "", false
	}
	button := findFirst(frame, func(n *html.Node) bool { return hasAttr(n, "data-code") })
	if button == nil {
		return "", false
	}
	return strings.ReplaceAll(attr(button, "data-code"), "\x7f", "\n"), true
}

// codeTitle is the label of pre's frame, such as a file name, or "".
func codeTitle(pre *html.Node) string {
	frame := codeFrame(pre)
	if frame == nil {
		return ""
	}
	caption := findFirst(frame, func(n *html.Node) bool { return n.DataAtom == atom.Figcaption })
	if caption == nil {
		return ""
	}
	title := findFirst(caption, func(n *html.Node) bool { return containsToken(attr(n, "class"), "title") })
	if title == nil {
		return ""
	}
	return collapse(textContent(title))
}

var langPrefixes = []string{"language-", "lang-", "highlight-"}

// codeLang finds a code block's language: a language-x, lang-x or highlight-x
// class on the <pre>, its <code> child or its two nearest ancestors, else a
// data-lang or data-language attribute on the <pre> or <code>. It is "" when
// there is none.
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
		for _, key := range []string{"data-lang", "data-language"} {
			if lang := strings.TrimSpace(attr(n, key)); lang != "" {
				return strings.ToLower(lang)
			}
		}
	}
	return ""
}
