// Package annotation holds the rules for a user's highlights and notes
// (docs/adr/0007-annotations-and-anchoring.md): a selector points into one
// block's plain text, counted in UTF-16 code units as the browser counts,
// and must quote that text exactly when it is written.
package annotation

import (
	"fmt"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/rokumate/ronote/internal/doccontent"
)

// Limits on what an annotation may hold.
const (
	// MaxPerDocument is how many annotations one document may have.
	MaxPerDocument = 2000
	// MaxNote is the longest note, in characters.
	MaxNote = 5000
	// MaxContext is the longest prefix or suffix, in UTF-16 code units.
	MaxContext = 32
)

// Selector is a range of a block's plain text, and the text it held.
type Selector struct {
	Position Position `json:"position"`
	Quote    Quote    `json:"quote"`
}

// Position is a range of UTF-16 code units, end exclusive.
type Position struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

// Quote is the text a selector covers, and the text just around it, which
// tells repeated quotes apart when the selector is re-anchored.
type Quote struct {
	Exact  string `json:"exact"`
	Prefix string `json:"prefix"`
	Suffix string `json:"suffix"`
}

// Code is a stable reason an annotation cannot be written. The values are
// the API's error codes.
type Code string

// The codes.
const (
	CodeInvalidRequest Code = "invalid_request"
	CodeUnknownBlock   Code = "unknown_block"
	CodeQuoteMismatch  Code = "quote_mismatch"
	CodeLimit          Code = "annotation_limit"
)

// Problem is why an annotation cannot be written.
type Problem struct {
	Code Code
	Err  error
}

func (p *Problem) Error() string { return fmt.Sprintf("%s: %v", p.Code, p.Err) }

func (p *Problem) Unwrap() error { return p.Err }

func problem(code Code, format string, args ...any) *Problem {
	return &Problem{Code: code, Err: fmt.Errorf(format, args...)}
}

// LimitReached is the problem of a document that has MaxPerDocument
// annotations already.
func LimitReached() *Problem {
	return problem(CodeLimit, "a document holds at most %d annotations", MaxPerDocument)
}

// Check checks that sel points into the plain text of block blockID of
// content, and quotes it exactly there. It returns a *Problem or nil.
func Check(content *doccontent.Doc, blockID string, sel Selector) error {
	text, ok := doccontent.BlockText(content, blockID)
	if !ok {
		return problem(CodeUnknownBlock, "the document has no block %q that takes highlights", blockID)
	}
	units := utf16.Encode([]rune(text))
	start, end := sel.Position.Start, sel.Position.End
	switch {
	case start < 0 || end <= start:
		return problem(CodeInvalidRequest, "position %d-%d is not a range", start, end)
	case len(utf16.Encode([]rune(sel.Quote.Prefix))) > MaxContext || len(utf16.Encode([]rune(sel.Quote.Suffix))) > MaxContext:
		return problem(CodeInvalidRequest, "prefix and suffix hold at most %d characters", MaxContext)
	case end > len(units):
		return problem(CodeQuoteMismatch, "position %d-%d runs past the block's %d characters", start, end, len(units))
	case string(utf16.Decode(units[start:end])) != sel.Quote.Exact:
		return problem(CodeQuoteMismatch, "the block's text at %d-%d is not the quote", start, end)
	}
	return nil
}

// CheckNote checks a note's length. It returns a *Problem or nil.
func CheckNote(note string) error {
	if n := utf8.RuneCountInString(note); n > MaxNote {
		return problem(CodeInvalidRequest, "the note has %d characters, at most %d are allowed", n, MaxNote)
	}
	return nil
}
