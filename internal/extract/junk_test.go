package extract_test

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/rokumate/ronote/internal/ir"
)

// TestGoldenHasNoJunk requires every fixture to come out free of page chrome.
func TestGoldenHasNoJunk(t *testing.T) {
	for _, name := range fixtures(t) {
		t.Run(name, func(t *testing.T) {
			if junk := findJunk(extractFixture(t, name)); len(junk) > 0 {
				t.Errorf("junk found:\n%s", strings.Join(junk, "\n"))
			}
		})
	}
}

// junkPhrases only ever appear in page chrome, never in documentation text.
var junkPhrases = []string{"Section titled", "Edit this page", "Edit page", "Was this page helpful", "$29", "Help improve MDN"}

// junkLabels are control labels: junk when they are a whole block's text,
// or a whole table cell's.
var junkLabels = []string{"Copy", "Copied!", "Copy code", "Copy to clipboard", "Pause", "Play", "Idle", "Subscribe"}

// codeLanguages are language names that sites print as a label above code.
var codeLanguages = []string{
	"bash", "c", "console", "cpp", "css", "go", "html", "java", "javascript", "js", "json", "jsx",
	"plaintext", "py", "python", "rust", "sh", "shell", "sql", "text", "toml", "ts", "tsx", "typescript", "yaml",
}

var (
	// pageMeta matches "last updated" lines, which carry a full date; an
	// author's note such as "Last updated: March 2026. ..." is content.
	pageMeta     = regexp.MustCompile(`^(Last updated|Last modified|This page was last modified)\b.*\b[A-Z][a-z]{2,8}\.? \d{1,2}, \d{4}`)
	pagination   = regexp.MustCompile(`^Previous\b.*\bNext\b`)
	versionBadge = regexp.MustCompile(`^Version: \d+(\.\d+)+$`)
	// lineNumber matches a line that starts with the number n left behind by
	// a line-number gutter, alone or before the code ("1def", "2    return"),
	// but not a numbered item such as "1. Load" or "1) Load".
	lineNumber = func(n int) *regexp.Regexp {
		return regexp.MustCompile(fmt.Sprintf(`^\s*%d([^\d.)]|$)`, n))
	}
	lineNumbers = []*regexp.Regexp{lineNumber(1), lineNumber(2), lineNumber(3)}
)

// findJunk lists page chrome that ended up in doc: control labels, edit and
// feedback links, pagination, version badges, breadcrumbs, language labels
// above code, line numbers or copy labels inside code, and any of the
// site-specific phrases. A site phrase that starts with "=" must be a whole
// block's text, like a label; one that starts with "code:" is looked for in
// code.
func findJunk(doc *ir.Doc, site ...string) []string {
	j := junkFinder{phrases: junkPhrases, labels: junkLabels}
	for _, s := range site {
		if label, ok := strings.CutPrefix(s, "="); ok {
			j.labels = append(j.labels, label)
		} else if inCode, ok := strings.CutPrefix(s, "code:"); ok {
			j.codePhrases = append(j.codePhrases, inCode)
		} else {
			j.phrases = append(j.phrases, s)
		}
	}
	j.blocks(doc.Blocks, true)
	return j.found
}

type junkFinder struct {
	phrases, labels, codePhrases []string
	found                        []string
	seenHeading                  bool
}

func (j *junkFinder) add(what, text string) {
	if len([]rune(text)) > 60 {
		text = string([]rune(text)[:60]) + "…"
	}
	j.found = append(j.found, fmt.Sprintf("%s %q", what, text))
}

func (j *junkFinder) blocks(blocks []ir.Block, top bool) {
	for i := range blocks {
		b := &blocks[i]
		if top && b.Kind == ir.KindHeading {
			j.seenHeading = true
		}
		if top && !j.seenHeading && isBreadcrumb(b) {
			j.add("breadcrumb", plainText(b))
		}
		if b.Kind == ir.KindParagraph && i+1 < len(blocks) && blocks[i+1].Kind == ir.KindCode && isLanguageLabel(runText(b.Content), blocks[i+1].Lang) {
			j.add("language label", runText(b.Content))
		}
		switch b.Kind {
		case ir.KindHeading, ir.KindParagraph:
			j.prose(runText(b.Content))
		case ir.KindCode:
			j.code(b.Text)
		case ir.KindTable:
			for _, row := range append(append([][]ir.Cell{}, b.Head...), b.Rows...) {
				for _, cell := range row {
					j.prose(runText(cell))
				}
			}
		case ir.KindCallout:
			j.prose(b.Title)
		}
		for _, item := range b.Items {
			if item.Label != "" {
				j.prose(item.Label)
			}
			j.blocks(item.Children, false)
		}
		j.blocks(b.Children, false)
	}
}

func (j *junkFinder) prose(text string) {
	text = strings.TrimSpace(text)
	for _, phrase := range j.phrases {
		if strings.Contains(text, phrase) {
			j.add("phrase", text)
			return
		}
	}
	for _, label := range j.labels {
		if text == label {
			j.add("label", text)
			return
		}
	}
	if pageMeta.MatchString(text) || pagination.MatchString(text) || versionBadge.MatchString(text) {
		j.add("page meta", text)
	}
}

func (j *junkFinder) code(text string) {
	if strings.Contains(text, "Copy to clipboard") || strings.Contains(text, "Copied!") {
		j.add("copy label in code", text)
	}
	for _, phrase := range j.codePhrases {
		if strings.Contains(text, phrase) {
			j.add("phrase in code", text)
		}
	}
	lines := strings.SplitN(text, "\n", 4)
	if len(lines) < 3 {
		return
	}
	for i, re := range lineNumbers {
		if !re.MatchString(lines[i]) {
			return
		}
	}
	j.add("line numbers in code", text)
}

// isLanguageLabel reports whether a paragraph just above a code block only
// names its language.
func isLanguageLabel(text, lang string) bool {
	text = strings.ToLower(strings.TrimSpace(text))
	return text != "" && (text == strings.ToLower(lang) || slices.Contains(codeLanguages, text))
}

// isBreadcrumb matches a trail of links: a list whose items are single
// paragraphs, all but the last fully linked, or a paragraph that starts with
// a link and separates its parts with an arrow.
func isBreadcrumb(b *ir.Block) bool {
	switch b.Kind {
	case ir.KindList:
		if len(b.Items) < 2 {
			return false
		}
		for i, item := range b.Items {
			if len(item.Children) != 1 || item.Children[0].Kind != ir.KindParagraph {
				return false
			}
			if i < len(b.Items)-1 && !allLinked(item.Children[0].Content) {
				return false
			}
		}
		return true
	case ir.KindParagraph:
		text := runText(b.Content)
		return len(b.Content) > 0 && b.Content[0].Link != nil &&
			(strings.Contains(text, " → ") || strings.Contains(text, " › ") || strings.Contains(text, " » "))
	}
	return false
}

func allLinked(runs []ir.Inline) bool {
	for _, r := range runs {
		if r.Link == nil && strings.TrimSpace(r.Text) != "" {
			return false
		}
	}
	return len(runs) > 0
}

func runText(runs []ir.Inline) string {
	var b strings.Builder
	for _, r := range runs {
		b.WriteString(r.Text)
	}
	return b.String()
}

// plainText is a block's text for messages.
func plainText(b *ir.Block) string {
	if b.Kind != ir.KindList {
		return runText(b.Content)
	}
	var parts []string
	for _, item := range b.Items {
		for i := range item.Children {
			parts = append(parts, plainText(&item.Children[i]))
		}
	}
	return strings.Join(parts, " | ")
}

func TestFindJunk(t *testing.T) {
	t.Parallel()
	p := func(text string) ir.Block {
		return ir.Block{Kind: ir.KindParagraph, Content: []ir.Inline{{Text: text}}}
	}
	link := func(text string) ir.Inline {
		return ir.Inline{Text: text, Link: &ir.Link{Href: "https://x.example/" + text, Kind: ir.LinkSameDomain}}
	}
	code := func(lang, text string) ir.Block { return ir.Block{Kind: ir.KindCode, Lang: lang, Text: text} }
	heading := ir.Block{Kind: ir.KindHeading, Level: 1, Content: []ir.Inline{{Text: "Title"}}}

	tests := []struct {
		name   string
		blocks []ir.Block
		site   []string
		want   int
	}{
		{name: "clean", blocks: []ir.Block{heading, p("Copy that to a file main.py."), code("go", "1. Load\n2. Chunk\n3. Embed\n")}},
		{name: "anchor text", blocks: []ir.Block{heading, p("Section titled “Setup”")}, want: 1},
		{name: "control label", blocks: []ir.Block{heading, p("Copied!")}, want: 1},
		{name: "page meta with a date", blocks: []ir.Block{p("Last updated on Jul 10, 2026 by A")}, want: 1},
		{name: "an author's note is content", blocks: []ir.Block{p("Last updated: March 2026. Examples use v1.")}},
		{name: "pagination", blocks: []ir.Block{p("Previous Setup Next Usage")}, want: 1},
		{name: "version badge", blocks: []ir.Block{p("Version: 3.10.2")}, want: 1},
		{name: "language label", blocks: []ir.Block{p("js"), code("", "x()")}, want: 1},
		{name: "line numbers glued to code", blocks: []ir.Block{code("py", "1def f():\n2    return 1\n3\n")}, want: 1},
		{name: "copy label in code", blocks: []ir.Block{code("sh", "ls\nCopy to clipboard")}, want: 1},
		{
			name: "breadcrumb list before the first heading",
			blocks: []ir.Block{{Kind: ir.KindList, Items: []ir.Item{
				{Children: []ir.Block{{Kind: ir.KindParagraph, Content: []ir.Inline{link("Docs")}}}},
				{Children: []ir.Block{p("Page")}},
			}}, heading},
			want: 1,
		},
		{
			name:   "breadcrumb paragraph",
			blocks: []ir.Block{{Kind: ir.KindParagraph, Content: []ir.Inline{link("Docs"), {Text: " → "}, link("v18")}}, heading},
			want:   1,
		},
		{name: "site label must be whole", blocks: []ir.Block{heading, p("Prevent this"), p("Prev")}, site: []string{"=Prev"}, want: 1},
		{name: "site phrase", blocks: []ir.Block{heading, p("Supported Versions: 18")}, site: []string{"Supported Versions:"}, want: 1},
		{name: "site phrase in code", blocks: []ir.Block{heading, code("console", "$ <font>run</font>")}, site: []string{"code:<font"}, want: 1},
		{
			name:   "tab labels are checked",
			blocks: []ir.Block{heading, {Kind: ir.KindTabs, Items: []ir.Item{{Label: "Copy", Children: []ir.Block{p("x")}}}}},
			want:   1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := findJunk(&ir.Doc{Blocks: tt.blocks}, tt.site...); len(got) != tt.want {
				t.Errorf("findJunk() = %q, want %d finding(s)", got, tt.want)
			}
		})
	}
}
