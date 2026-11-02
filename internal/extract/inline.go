package extract

import (
	"slices"
	"strings"
	"unicode"

	"github.com/rokumate/ronote/internal/ir"
)

// inliner gathers text into runs, collapsing whitespace the way browsers
// render normal text: any run of spaces, tabs and line breaks becomes one
// space, and spaces at the start and end of a block disappear. Other
// whitespace, such as a no-break space, is kept.
//
// Collapsed whitespace waits as a pending space and is only written before
// the next visible character, with the marks and link it had where it
// appeared, so nothing ever has to be trimmed back out. With the open run
// built in a strings.Builder, every page converts in linear time.
type inliner struct {
	runs     []ir.Inline     // finished runs of the current block
	buf      strings.Builder // text of the run being built
	runMarks []ir.Mark
	runLink  *ir.Link

	marks []ir.Mark // active formatting, innermost last; may repeat
	link  *ir.Link

	started    bool // something visible was written since the block or line began
	space      bool // collapsed whitespace waits to be written as one space
	spaceMarks []ir.Mark
	spaceLink  *ir.Link
}

func isCollapsible(r rune) bool {
	return r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '\f'
}

func isWordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r)
}

// text adds s with its whitespace collapsed.
func (in *inliner) text(s string) {
	var b strings.Builder
	for _, r := range s {
		if isCollapsible(r) {
			if in.started && !in.space {
				in.space, in.spaceMarks, in.spaceLink = true, in.activeMarks(), in.link
			}
			continue
		}
		if in.space {
			in.write(b.String())
			b.Reset()
			in.writeSpace()
		}
		b.WriteRune(r)
		in.started = true
	}
	in.write(b.String())
}

// raw adds s exactly as given.
func (in *inliner) raw(s string) {
	if s == "" {
		return
	}
	if in.space {
		in.writeSpace()
	}
	in.write(s)
	in.started = true
}

// writeSpace writes the pending space with the marks and link it had.
func (in *inliner) writeSpace() {
	in.writeWith(" ", in.spaceMarks, in.spaceLink)
	in.space = false
}

// lineBreak adds a <br>: a "\n" with the whitespace around it dropped.
func (in *inliner) lineBreak() {
	in.space = false
	in.write("\n")
	in.started = false
}

func (in *inliner) write(s string) {
	if s != "" {
		in.writeWith(s, in.activeMarks(), in.link)
	}
}

func (in *inliner) writeWith(s string, marks []ir.Mark, link *ir.Link) {
	if in.buf.Len() > 0 && (!slices.Equal(in.runMarks, marks) || !sameLink(in.runLink, link)) {
		in.closeRun()
	}
	if in.buf.Len() == 0 {
		in.runMarks = marks
		in.runLink = nil
		if link != nil {
			l := *link
			in.runLink = &l
		}
	}
	in.buf.WriteString(s)
}

func (in *inliner) closeRun() {
	if in.buf.Len() == 0 {
		return
	}
	in.runs = append(in.runs, ir.Inline{Text: in.buf.String(), Marks: in.runMarks, Link: in.runLink})
	in.buf.Reset()
}

// take returns the runs of the current block without line breaks at its
// edges, and starts a new block. Marks and link stay active.
func (in *inliner) take() []ir.Inline {
	in.closeRun()
	runs := in.runs
	in.runs = nil
	in.started, in.space = false, false

	for len(runs) > 0 {
		if text := strings.TrimLeft(runs[0].Text, "\n"); text != "" {
			runs[0].Text = text
			break
		}
		runs = runs[1:]
	}
	for len(runs) > 0 {
		last := len(runs) - 1
		if text := strings.TrimRight(runs[last].Text, "\n"); text != "" {
			runs[last].Text = text
			break
		}
		runs = runs[:last]
	}
	return runs
}

// activeMarks lists the marks in effect, once each, in markOrder.
func (in *inliner) activeMarks() []ir.Mark {
	var out []ir.Mark
	for _, m := range markOrder {
		if slices.Contains(in.marks, m) {
			out = append(out, m)
		}
	}
	return out
}

func sameLink(a, b *ir.Link) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// collapse applies the inliner's whitespace rule to a plain string.
func collapse(s string) string {
	var in inliner
	in.text(s)
	return runsText(in.take())
}
