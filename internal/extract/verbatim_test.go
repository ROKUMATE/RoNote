package extract_test

import (
	"bytes"
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
// tautology.
func TestCodeIsVerbatim(t *testing.T) {
	total := 0
	for _, name := range fixtures(t) {
		want := codeByTokens(readFixture(t, name, "input.html"))
		total += len(want)
		t.Run(name, func(t *testing.T) {
			got := codeTexts(extractFixture(t, name).Blocks)
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
			out = append(out, codeTexts(item)...)
		}
		out = append(out, codeTexts(b.Children)...)
	}
	return out
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

// contentRoot applies the root rule to the token stream: the first <main>,
// else the first <article>, else the first role=main element, else <body>.
func contentRoot(input []byte) tokenRoot {
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
//   - the text is every text token inside the <pre>, decoded, with <br> as
//     "\n" and one "\n" dropped when it is the very next thing after the
//     <pre> start tag;
//   - a <pre> with no text gives no code block.
func codeByTokens(input []byte) []string {
	type element struct {
		tag                      string
		root, excluded, inlineOK bool
	}
	root := contentRoot(input)
	var stack []element
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
	finish := func() {
		if text := code.String(); text != "" {
			codes = append(codes, text)
		}
		code, codeDepth = nil, -1
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
			return codes
		case html.TextToken:
			if code != nil {
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
			if code != nil && tag == "br" {
				code.WriteString("\n")
			}
			if voidElements[tag] {
				continue
			}
			var parent element
			if len(stack) > 0 {
				parent = stack[len(stack)-1]
			}
			_, hidden := attrs["hidden"]
			e := element{
				tag:  tag,
				root: isRoot,
				excluded: parent.excluded || tokenSkipped[tag] || hidden ||
					strings.EqualFold(attrs["aria-hidden"], "true") ||
					(root.fallback && (tag == "header" || tag == "footer")),
				inlineOK: parent.inlineOK || tokenInlineOnly[tag],
			}
			stack = append(stack, e)
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
