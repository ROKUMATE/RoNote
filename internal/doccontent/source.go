package doccontent

import (
	"bytes"
	"encoding/json"
	"errors"
	"slices"

	"github.com/rokumate/ronote/internal/ir"
)

// lockedKinds are the kinds the editor shows as atomic: inside a source
// block they must stay exactly as extracted.
var lockedKinds = []ir.Kind{ir.KindCode, ir.KindTable, ir.KindTabs, ir.KindImage}

// CheckSource checks doc's source blocks against the extraction the document
// was made from (ADR 0006). Each names a top-level block of the extraction,
// at most once; every code, table, tabs and image block inside it equals the
// extraction's block with the same id; and one marked unedited equals its
// extraction block. It returns a *Problem or nil. Call Validate first.
func CheckSource(doc *Doc, extraction *ir.Doc) error {
	top := map[string]*ir.Block{}
	for i := range extraction.Blocks {
		top[extraction.Blocks[i].ID] = &extraction.Blocks[i]
	}
	all := map[string]*ir.Block{}
	for i := range extraction.Blocks {
		index(&extraction.Blocks[i], all)
	}

	used := map[string]bool{}
	for i := range doc.Blocks {
		b := &doc.Blocks[i]
		if b.Origin != OriginSource {
			continue
		}
		src, ok := top[b.SourceBlockID]
		switch {
		case !ok:
			return problem(CodeUnknownSourceBlock, "blocks[%d]: no block %q in the extraction", i, b.SourceBlockID)
		case used[b.SourceBlockID]:
			return problem(CodeUnknownSourceBlock, "blocks[%d]: block %q of the extraction is used twice", i, b.SourceBlockID)
		}
		used[b.SourceBlockID] = true
		if id := changedLocked(&b.Block, all); id != "" {
			return problem(CodeSourceBlockModified, "blocks[%d]: block %q is not as extracted, and cannot be edited", i, id)
		}
		if !*b.Edited && !same(&b.Block, src) {
			return problem(CodeSourceBlockModified, "blocks[%d]: block %q is marked unedited but differs from the extraction", i, b.ID)
		}
	}
	return nil
}

// index adds b and every block inside it to byID.
func index(b *ir.Block, byID map[string]*ir.Block) {
	byID[b.ID] = b
	for _, child := range children(b) {
		index(child, byID)
	}
}

// changedLocked returns the id of the first locked block in b, b included,
// that is not its extraction block, or "".
func changedLocked(b *ir.Block, extracted map[string]*ir.Block) string {
	if slices.Contains(lockedKinds, b.Kind) {
		if src, ok := extracted[b.ID]; !ok || !same(b, src) {
			return b.ID
		}
		return ""
	}
	for _, child := range children(b) {
		if id := changedLocked(child, extracted); id != "" {
			return id
		}
	}
	return ""
}

// same reports whether a and b are the same block, byte for byte as JSON.
func same(a, b *ir.Block) bool {
	ja, errA := json.Marshal(a)
	jb, errB := json.Marshal(b)
	return errA == nil && errB == nil && bytes.Equal(ja, jb)
}

// ErrNoSourceBlock means a document has no source block with the id asked for.
var ErrNoSourceBlock = errors.New("no such source block")

// Revert returns doc with its source block blockID replaced by the
// extraction's block, unedited. It returns ErrNoSourceBlock when doc has no
// source block blockID, or the extraction no longer has it.
func Revert(doc *Doc, extraction *ir.Doc, blockID string) (*Doc, error) {
	i := slices.IndexFunc(doc.Blocks, func(b Block) bool { return b.Origin == OriginSource && b.ID == blockID })
	if i < 0 {
		return nil, ErrNoSourceBlock
	}
	j := slices.IndexFunc(extraction.Blocks, func(b ir.Block) bool { return b.ID == doc.Blocks[i].SourceBlockID })
	if j < 0 {
		return nil, ErrNoSourceBlock
	}
	edited := false
	out := &Doc{DocVersion: doc.DocVersion, Blocks: slices.Clone(doc.Blocks)}
	out.Blocks[i] = Block{Block: extraction.Blocks[j], Origin: OriginSource, Edited: &edited, SourceBlockID: extraction.Blocks[j].ID}
	return out, nil
}
