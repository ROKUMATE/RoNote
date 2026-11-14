package chat

import (
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/rokumate/ronote/internal/store"
)

// Passage is a chunk of the owner's library as a model is sent it: a
// search result whose content is the chunk's top-level blocks, a text each.
type Passage struct {
	ChunkID    uuid.UUID
	DocumentID uuid.UUID
	// Title is "Doc title > heading path".
	Title    string
	BlockIDs []string
	Texts    []string
}

// Source names the passage to the model.
func (p Passage) Source() string { return "ronote:chunk/" + p.ChunkID.String() }

// citer turns a reply's citations into the owner's blocks
// (docs/adr/0012-chat.md). It knows every passage the turn's request sent,
// in the order the model counts them.
type citer struct {
	sent []Passage
	// cited are the valid citations so far, numbered in order.
	cited []store.Citation
	// invalid counts citations that pointed at nothing sent.
	invalid int
}

// cite returns the number of c's blocks, numbering them if they are new,
// or 0 when c points at nothing the turn sent. Its blocks and text are the
// citer's own copy of what was sent, never the model's.
func (ct *citer) cite(c Cited) int {
	if c.ResultIndex < 0 || c.ResultIndex >= len(ct.sent) {
		ct.invalid++
		return 0
	}
	p := ct.sent[c.ResultIndex]
	if c.Source != p.Source() || c.Start < 0 || c.Start >= c.End || c.End > len(p.BlockIDs) {
		ct.invalid++
		return 0
	}
	ids := p.BlockIDs[c.Start:c.End]
	for _, have := range ct.cited {
		if have.DocumentID == p.DocumentID && slices.Equal(have.BlockIDs, ids) {
			return have.N
		}
	}
	n := len(ct.cited) + 1
	ct.cited = append(ct.cited, store.Citation{
		N: n, DocumentID: p.DocumentID, BlockIDs: slices.Clone(ids),
		CitedText: strings.Join(p.Texts[c.Start:c.End], "\n\n"), Title: p.Title,
	})
	return n
}
