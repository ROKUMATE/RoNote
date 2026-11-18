package chat

import (
	"regexp"
	"unicode"
	"unicode/utf8"
)

var (
	// fencedCode is a fenced code block, an unterminated one running to the
	// end; inlineCode is a code span on one line.
	fencedCode = regexp.MustCompile("(?s)```.*?```|```.*$")
	inlineCode = regexp.MustCompile("`[^`\\n]*`")
	// marker is a citation marker as a model writes one itself: [1], [1-2],
	// [1–2] or [1, 2].
	marker = regexp.MustCompile(`\[\d+(?:\s*[-–,]\s*\d+)*\]`)
)

// countMarkers counts the citation markers the model wrote into text
// outside code. Citations attach to a reply's sentences on their own, so a
// marker written by hand is a number nothing stands behind. A bracketed
// number right after a word, as in a[3], is an index, not a marker. The
// text is never changed.
func countMarkers(text string) int {
	text = fencedCode.ReplaceAllString(text, " ")
	text = inlineCode.ReplaceAllString(text, " ")
	n := 0
	for _, at := range marker.FindAllStringIndex(text, -1) {
		if before, _ := utf8.DecodeLastRuneInString(text[:at[0]]); at[0] > 0 && (unicode.IsLetter(before) || unicode.IsDigit(before) || before == '_') {
			continue
		}
		n++
	}
	return n
}
