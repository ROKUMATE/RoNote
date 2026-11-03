//go:build live

package extract_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"text/tabwriter"

	"golang.org/x/net/html"

	"github.com/rokumate/ronote/internal/extract"
	"github.com/rokumate/ronote/internal/fetch"
	"github.com/rokumate/ronote/internal/ir"
)

// livePage is a real documentation page the extractor must handle cleanly.
type livePage struct {
	url     string
	adapter string // the adapter that must be chosen
	// content names the element that holds the page's main content, as
	// tag#id.class; the expected code count is taken inside it.
	content string
	// warnings lists the warning codes this page is expected to get.
	warnings []ir.WarningCode
	// junk lists chrome specific to this site, in findJunk's form.
	junk []string
}

var livePages = []livePage{
	{url: "https://myengineeringpath.dev/programming/python/rag-pipeline-tutorial/", adapter: "starlight", content: "div.sl-markdown-content"},
	{
		url: "https://docusaurus.io/docs/markdown-features/code-blocks", adapter: "docusaurus", content: "div.theme-doc-markdown",
		junk: []string{"=http://localhost:3000", "=Live Editor", "=Result"},
	},
	{url: "https://fastapi.tiangolo.com/tutorial/first-steps/", adapter: "material", content: "article.md-content__inner"},
	{url: "https://docs.python.org/3/tutorial/controlflow.html", adapter: "sphinx", content: "div.body"},
	{url: "https://requests.readthedocs.io/en/latest/user/quickstart/", adapter: "sphinx", content: "div.body"},
	{url: "https://vite.dev/guide/", adapter: "vitepress", content: "div.vp-doc"},
	{url: "https://doc.rust-lang.org/book/ch03-02-data-types.html", adapter: "mdbook", content: "main"},
	{url: "https://kubernetes.io/docs/concepts/workloads/pods/", adapter: "docsy", content: "div.td-content", junk: []string{"=Feedback"}},
	{url: "https://go.dev/doc/effective_go", adapter: "godev", content: "article.Doc"},
	{
		url: "https://developer.mozilla.org/en-US/docs/Web/JavaScript/Guide/Closures", adapter: "mdn", content: "main#content",
		junk: []string{"Learn how to contribute", "View this page on GitHub", "Report a problem with this content"},
	},
	{
		url: "https://www.postgresql.org/docs/current/tutorial-select.html", adapter: "docbook", content: "div#docContent",
		junk: []string{"Released!", "Supported Versions:", "Unsupported versions:", "Submit correction", "=Prev", "=Next", "=Up", "=Home"},
	},
}

// TestLive fetches every page in livePages with the production fetcher and
// prints what the extractor made of it. It needs the network, so it only
// builds with -tags live (make test-live) and never runs in CI.
func TestLive(t *testing.T) {
	f := fetch.New(fetch.Options{})
	rows := []string{"url\tadapter\tblocks\tcode found/expected\tverbatim\twarnings\tjunk"}
	var problems []string
	for _, p := range livePages {
		row, junk, failed := checkLivePage(t.Context(), f, p)
		rows = append(rows, strings.Join(row, "\t"))
		for _, j := range junk {
			problems = append(problems, p.url+": junk: "+j)
		}
		problems = append(problems, failed...)
	}
	var table strings.Builder
	tw := tabwriter.NewWriter(&table, 0, 0, 2, ' ', 0)
	for _, row := range rows {
		if _, err := fmt.Fprintln(tw, row); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Flush(); err != nil {
		t.Fatal(err)
	}
	t.Log("\n" + table.String())
	for _, p := range problems {
		t.Error(p)
	}
}

// checkLivePage returns the table row for p, the junk found, and every
// requirement p failed.
func checkLivePage(ctx context.Context, f *fetch.Fetcher, p livePage) (row, junk, failed []string) {
	short := strings.TrimPrefix(strings.TrimPrefix(p.url, "https://"), "www.")
	res, err := f.Fetch(ctx, fetch.Request{URL: p.url})
	if err != nil {
		reason := err.Error()
		if errors.Is(err, fetch.ErrDisallowedByRobots) {
			reason = "robots.txt disallows RoNoteBot"
		}
		return []string{short, "-", reason, "-", "-", "-", "-"}, nil, []string{p.url + ": " + reason}
	}
	doc, err := extract.Extract(bytes.NewReader(res.Body), ir.Source{
		URL: p.url, FinalURL: res.FinalURL, FetchedAt: res.FetchedAt, BodySHA256: res.BodySHA256,
	})
	if err == nil {
		err = ir.Validate(doc)
	}
	if err != nil {
		return []string{short, "-", err.Error(), "-", "-", "-", "-"}, nil, []string{p.url + ": " + err.Error()}
	}

	adapter := doc.Extractor.Name
	if adapter != p.adapter {
		failed = append(failed, fmt.Sprintf("%s: adapter %s, want %s", p.url, adapter, p.adapter))
		adapter += " (want " + p.adapter + ")"
	}

	found := codeTexts(doc.Blocks)
	expected := expectedCodeCount(res.Body, p.content)
	if len(found) != expected {
		failed = append(failed, fmt.Sprintf("%s: %d code blocks, an independent count finds %d", p.url, len(found), expected))
	}

	verbatim := "ok"
	if want := codeByTokens(res.Body, adapterRules[doc.Extractor.Name]); len(want) != len(found) {
		verbatim = fmt.Sprintf("token reading has %d", len(want))
	} else if n := differing(found, want); n > 0 {
		verbatim = fmt.Sprintf("%d differ", n)
	}
	if verbatim != "ok" {
		failed = append(failed, p.url+": verbatim: "+verbatim)
	}

	var codes []string
	for _, w := range doc.Warnings {
		codes = append(codes, string(w.Code))
		if !slices.Contains(p.warnings, w.Code) {
			failed = append(failed, fmt.Sprintf("%s: unexpected warning %s: %s", p.url, w.Code, w.Message))
		}
	}

	junk = findJunk(doc, p.junk...)
	return []string{
		short, adapter, blockKinds(doc.Blocks),
		fmt.Sprintf("%d/%d", len(found), expected), verbatim,
		strings.Join(codes, ",") + dashIfEmpty(codes), fmt.Sprint(len(junk)),
	}, junk, failed
}

func dashIfEmpty(s []string) string {
	if len(s) == 0 {
		return "-"
	}
	return ""
}

func differing(got, want []string) int {
	n := 0
	for i := range got {
		if got[i] != want[i] {
			n++
		}
	}
	return n
}

// kindShort abbreviates block kinds for the table, in the order shown.
var kindShort = []struct {
	kind ir.Kind
	abbr string
}{
	{ir.KindHeading, "h"},
	{ir.KindParagraph, "p"},
	{ir.KindList, "list"},
	{ir.KindCode, "code"},
	{ir.KindTable, "table"},
	{ir.KindCallout, "callout"},
	{ir.KindTabs, "tabs"},
	{ir.KindImage, "img"},
	{ir.KindQuote, "quote"},
	{ir.KindDivider, "hr"},
}

// blockKinds counts blocks by kind, nested ones included.
func blockKinds(blocks []ir.Block) string {
	counts := map[ir.Kind]int{}
	var walk func([]ir.Block)
	walk = func(blocks []ir.Block) {
		for _, b := range blocks {
			counts[b.Kind]++
			for _, item := range b.Items {
				walk(item.Children)
			}
			walk(b.Children)
		}
	}
	walk(blocks)
	var parts []string
	for _, k := range kindShort {
		if counts[k.kind] > 0 {
			parts = append(parts, fmt.Sprintf("%s%d", k.abbr, counts[k.kind]))
		}
	}
	return strings.Join(parts, " ")
}

// expectedCodeCount counts the code blocks of a page without the extractor:
// the copy payloads (data-code attributes) inside the content element if the
// page has any, else its <pre> elements, leaving out line-number gutters.
// content is tag, tag#id or tag.class.
func expectedCodeCount(page []byte, content string) int {
	match := matcher(content)
	type element struct {
		tag             string
		content, gutter bool
	}
	var stack []element
	payloads, pres := 0, 0
	gutterPre := false
	z := html.NewTokenizer(bytes.NewReader(page))
	for {
		tt := z.Next()
		switch tt {
		case html.ErrorToken:
			if payloads > 0 {
				return payloads
			}
			return pres
		case html.StartTagToken, html.SelfClosingTagToken:
			tag, attrs := tagAndAttrs(z)
			var parent element
			if len(stack) > 0 {
				parent = stack[len(stack)-1]
			}
			e := element{
				tag:     tag,
				content: parent.content || match(tag, attrs),
				gutter:  parent.gutter || hasClass(attrs, "linenos") || hasClass(attrs, "linenodiv"),
			}
			if e.content {
				if _, ok := attrs["data-code"]; ok {
					payloads++
				}
				if tag == "pre" && !e.gutter {
					pres++
					gutterPre = false
				}
				if tag == "span" && hasClass(attrs, "lnt") {
					gutterPre = true // a Chroma line-number table cell
				}
			}
			if !voidElements[tag] && tt == html.StartTagToken {
				stack = append(stack, e)
			}
		case html.EndTagToken:
			tag, _ := tagAndAttrs(z)
			if tag == "pre" && gutterPre {
				pres--
				gutterPre = false
			}
			for i := len(stack) - 1; i >= 0; i-- {
				if stack[i].tag == tag {
					stack = stack[:i]
					break
				}
			}
		}
	}
}

func hasClass(attrs map[string]string, class string) bool {
	return slices.Contains(strings.Fields(attrs["class"]), class)
}
