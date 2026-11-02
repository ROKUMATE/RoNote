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
type inliner struct {
	runs  []ir.Inline
	marks []ir.Mark // active formatting, innermost last; may repeat
	link  *ir.Link
	// spaced is true at the start of a block and after a space or line
	// break, when a further space would collapse away.
	started bool
	spaced  bool
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
			if in.started && !in.spaced {
				b.WriteByte(' ')
				in.spaced = true
			}
			continue
		}
		b.WriteRune(r)
		in.started, in.spaced = true, false
	}
	in.append(b.String())
}

// raw adds s exactly as given.
func (in *inliner) raw(s string) {
	if s == "" {
		return
	}
	in.append(s)
	in.started, in.spaced = true, false
}

// lineBreak adds a <br>: a "\n" with the spaces around it removed.
func (in *inliner) lineBreak() {
	in.trimTrailingSpace()
	in.append("\n")
	in.started, in.spaced = true, true
}

func (in *inliner) append(s string) {
	if s == "" {
		return
	}
	marks := in.activeMarks()
	if n := len(in.runs); n > 0 && slices.Equal(in.runs[n-1].Marks, marks) && sameLink(in.runs[n-1].Link, in.link) {
		in.runs[n-1].Text += s
		return
	}
	var link *ir.Link
	if in.link != nil {
		l := *in.link
		link = &l
	}
	in.runs = append(in.runs, ir.Inline{Text: s, Marks: marks, Link: link})
}

func (in *inliner) trimTrailingSpace() {
	for n := len(in.runs); n > 0; n = len(in.runs) {
		last := &in.runs[n-1]
		last.Text = strings.TrimRight(last.Text, " ")
		if last.Text != "" {
			return
		}
		in.runs = in.runs[:n-1]
	}
}

// take returns the runs gathered so far, trimmed, and starts a new block.
// Marks and link stay active.
func (in *inliner) take() []ir.Inline {
	in.trimTrailingSpace()
	for len(in.runs) > 0 && strings.TrimLeft(in.runs[0].Text, " \n") == "" {
		in.runs = in.runs[1:]
	}
	if len(in.runs) > 0 {
		in.runs[0].Text = strings.TrimLeft(in.runs[0].Text, " \n")
	}
	for n := len(in.runs); n > 0; n = len(in.runs) {
		trimmed := strings.TrimRight(in.runs[n-1].Text, " \n")
		if trimmed != "" {
			in.runs[n-1].Text = trimmed
			break
		}
		in.runs = in.runs[:n-1]
	}
	runs := in.runs
	in.runs = nil
	in.started, in.spaced = false, false
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
