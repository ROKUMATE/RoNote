package extract_test

import (
	"bytes"
	"slices"
	"strings"
	"testing"

	"golang.org/x/net/html"

	"github.com/rokumate/ronote/internal/ir"
)

// TestCodeIsVerbatim requires every code block the extractor produces to equal,
// byte for byte, what an independent reading of the same HTML finds. The
// reading (codeByTokens) walks the raw token stream, while the extractor walks
// a parsed tree, and it implements the documented rules on its own without
// sharing code with the extractor, so agreement is evidence rather than a
// tautology. Framework rules come from adapterRules, keyed by the adapter the
// extractor chose; detection itself is checked by the golden files.
func TestCodeIsVerbatim(t *testing.T) {
	total := 0
	for _, name := range fixtures(t) {
		doc := extractFixture(t, name)
		want := codeByTokens(readFixture(t, name, "input.html"), adapterRules[doc.Extractor.Name])
		total += len(want)
		t.Run(name, func(t *testing.T) {
			got := codeTexts(doc.Blocks)
			if len(got) != len(want) {
				t.Fatalf("extractor found %d code blocks, the token reading %d:\n got %q\nwant %q", len(got), len(want), got, want)
			}
			for i := range want {
				if got[i] != want[i] {
					t.Errorf("code block %d differs:\n got %q\nwant %q", i, got[i], want[i])
				}
			}
		})
	}
	if total == 0 {
		t.Fatal("the fixtures contain no code blocks")
	}
}

// codeTexts lists the text of every code block in document order, nested
// ones included.
func codeTexts(blocks []ir.Block) []string {
	var out []string
	for _, b := range blocks {
		if b.Kind == ir.KindCode {
			out = append(out, b.Text)
		}
		for _, item := range b.Items {
			out = append(out, codeTexts(item.Children)...)
		}
		out = append(out, codeTexts(b.Children)...)
	}
	return out
}

// tokenRules are one framework's code rules, written down here apart from
// its adapter's configuration.
type tokenRules struct {
	root      string   // the content root as tag, tag#id or tag.class; empty for the generic rule
	skipped   []string // elements, in the same form, whose content is never kept
	tabPanels bool     // hidden role=tabpanel elements are content
}

// adapterRules holds the rules for each adapter's pages.
var adapterRules = map[string]tokenRules{
	// Islands are interactive widgets.
	"starlight": {skipped: []string{"astro-island"}, tabPanels: true},
}

// matcher turns tag, tag#id or tag.class into a match function.
func matcher(s string) func(tag string, attrs map[string]string) bool {
	tag, class, _ := strings.Cut(s, ".")
	tag, id, _ := strings.Cut(tag, "#")
	return func(t string, attrs map[string]string) bool {
		return t == tag && (id == "" || attrs["id"] == id) && (class == "" || slices.Contains(strings.Fields(attrs["class"]), class))
	}
}

// Elements whose content is never kept.
var tokenSkipped = map[string]bool{
	"nav": true, "aside": true, "script": true, "style": true, "noscript": true,
	"template": true, "form": true, "button": true, "iframe": true, "svg": true,
}

// Elements that only hold inline content, where a <pre> is not a code block.
var tokenInlineOnly = map[string]bool{
	"h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true,
	"table": true, "figcaption": true, "summary": true, "dt": true,
}

var voidElements = map[string]bool{
	"area": true, "base": true, "br": true, "col": true, "embed": true, "hr": true, "img": true,
	"input": true, "link": true, "meta": true, "source": true, "track": true, "wbr": true,
}

type tokenRoot struct {
	ordinal  int  // index of the root's start tag among all start tags; -1 for the whole document
	fallback bool // the root is <body>, so header and footer are left out too
}

// contentRoot applies the root rule to the token stream: the first element
// the framework's root matches, or the generic rule: the first <main>, else
// the first <article>, else the first role=main element, else <body>.
func contentRoot(input []byte, rules tokenRules) tokenRoot {
	if rules.root != "" {
		match := matcher(rules.root)
		z := html.NewTokenizer(bytes.NewReader(input))
		for ordinal := 0; ; {
			tt := z.Next()
			if tt == html.ErrorToken {
				return tokenRoot{ordinal: -1}
			}
			if tt == html.StartTagToken || tt == html.SelfClosingTagToken {
				if tag, attrs := tagAndAttrs(z); match(tag, attrs) {
					return tokenRoot{ordinal: ordinal}
				}
				ordinal++
			}
		}
	}
	firsts := map[string]int{}
	z := html.NewTokenizer(bytes.NewReader(input))
	for ordinal := 0; ; {
		tt := z.Next()
		if tt == html.ErrorToken {
			break
		}
		if tt != html.StartTagToken && tt != html.SelfClosingTagToken {
			continue
		}
		tag, attrs := tagAndAttrs(z)
		for _, key := range []string{tag, "role=" + strings.ToLower(attrs["role"])} {
			if _, seen := firsts[key]; !seen {
				firsts[key] = ordinal
			}
		}
		ordinal++
	}
	for _, key := range []string{"main", "article", "role=main"} {
		if ordinal, ok := firsts[key]; ok {
			return tokenRoot{ordinal: ordinal}
		}
	}
	if ordinal, ok := firsts["body"]; ok {
		return tokenRoot{ordinal: ordinal, fallback: true}
	}
	return tokenRoot{ordinal: -1, fallback: true}
}

// codeByTokens returns the text of every <pre> that should become a code
// block, reading tokens directly:
//   - only inside the content root (see contentRoot), and not inside a
//     skipped element, a hidden or aria-hidden=true element, an element that
//     only holds inline content, or, when the root is <body>, a header or
//     footer;
//   - inside a code frame (<figure class="frame">) whose copy button carries
//     the clipboard text in data-code, the text is that attribute with each
//     U+007F as "\n";
//   - otherwise the text is every text token inside the <pre>, decoded, with
//     <br> as "\n" and one "\n" dropped when it is the very next thing after
//     the <pre> start tag, leaving out what is inside an element styled
//     user-select: none or with class linenos;
//   - a <pre> with no text gives no code block;
//   - rules add the framework's own root, skipped elements and tab panels.
func codeByTokens(input []byte, rules tokenRules) []string {
	type element struct {
		tag                      string
		root, excluded, inlineOK bool
		notText                  bool // inside a <pre>, drawn beside the code
		frame                    bool
	}
	// frame is the innermost open code frame: the index in codes of its
	// <pre>, and its copy payload.
	type frame struct {
		depth, code int
		payload     *string
	}
	root := contentRoot(input, rules)
	var skipped []func(string, map[string]string) bool
	for _, s := range rules.skipped {
		skipped = append(skipped, matcher(s))
	}
	isSkipped := func(tag string, attrs map[string]string) bool {
		return slices.ContainsFunc(skipped, func(m func(string, map[string]string) bool) bool { return m(tag, attrs) })
	}
	var stack []element
	var frames []frame
	var codes []string
	var code *strings.Builder
	codeDepth := -1
	afterPreStart := false

	inRoot := func() bool {
		if root.ordinal < 0 {
			return true
		}
		for _, e := range stack {
			if e.root {
				return true
			}
		}
		return false
	}
	// finish records the open <pre>, even when empty: a frame's payload may
	// still replace it. Empty texts are dropped at the end.
	finish := func() {
		codes = append(codes, code.String())
		if len(frames) > 0 {
			frames[len(frames)-1].code = len(codes) - 1
		}
		code, codeDepth = nil, -1
	}
	result := func() []string {
		var out []string
		for _, c := range codes {
			if c != "" {
				out = append(out, c)
			}
		}
		return out
	}

	z := html.NewTokenizer(bytes.NewReader(input))
	for ordinal := 0; ; {
		tt := z.Next()
		justOpened := afterPreStart
		afterPreStart = false
		switch tt {
		case html.ErrorToken:
			if code != nil {
				finish()
			}
			return result()
		case html.TextToken:
			if code != nil && !stack[len(stack)-1].notText {
				text := string(z.Text())
				if justOpened {
					text = strings.TrimPrefix(text, "\n")
				}
				code.WriteString(text)
			}
		case html.StartTagToken, html.SelfClosingTagToken:
			tag, attrs := tagAndAttrs(z)
			isRoot := ordinal == root.ordinal
			ordinal++
			var parent element
			if len(stack) > 0 {
				parent = stack[len(stack)-1]
			}
			if payload, ok := attrs["data-code"]; ok && len(frames) > 0 {
				frames[len(frames)-1].payload = &payload
			}
			if code != nil && tag == "br" && !parent.notText {
				code.WriteString("\n")
			}
			if voidElements[tag] {
				continue
			}
			_, hidden := attrs["hidden"]
			if rules.tabPanels && attrs["role"] == "tabpanel" {
				hidden = false
			}
			style := strings.ToLower(strings.Join(strings.Fields(attrs["style"]), ""))
			e := element{
				tag:  tag,
				root: isRoot,
				excluded: parent.excluded || tokenSkipped[tag] || hidden || isSkipped(tag, attrs) ||
					strings.EqualFold(attrs["aria-hidden"], "true") ||
					(root.fallback && (tag == "header" || tag == "footer")),
				inlineOK: parent.inlineOK || tokenInlineOnly[tag],
				notText: code != nil && (parent.notText || strings.Contains(style, "user-select:none") ||
					slices.Contains(strings.Fields(attrs["class"]), "linenos")),
				frame: tag == "figure" && slices.Contains(strings.Fields(attrs["class"]), "frame"),
			}
			stack = append(stack, e)
			if e.frame {
				frames = append(frames, frame{depth: len(stack) - 1, code: -1})
			}
			if tag == "pre" && code == nil && inRoot() && !e.excluded && !e.inlineOK {
				code = &strings.Builder{}
				codeDepth = len(stack) - 1
				afterPreStart = true
			}
		case html.EndTagToken:
			tag, _ := tagAndAttrs(z)
			for i := len(stack) - 1; i >= 0; i-- {
				if stack[i].tag != tag {
					continue
				}
				if code != nil && i <= codeDepth {
					finish()
				}
				for len(frames) > 0 && frames[len(frames)-1].depth >= i {
					f := frames[len(frames)-1]
					if f.payload != nil && f.code >= 0 {
						codes[f.code] = strings.ReplaceAll(*f.payload, "\x7f", "\n")
					}
					frames = frames[:len(frames)-1]
				}
				stack = stack[:i]
				break
			}
		}
	}
}

func tagAndAttrs(z *html.Tokenizer) (string, map[string]string) {
	name, more := z.TagName()
	attrs := map[string]string{}
	for more {
		var key, val []byte
		key, val, more = z.TagAttr()
		attrs[string(key)] = string(val)
	}
	return string(name), attrs
}
