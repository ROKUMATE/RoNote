// Package doccontent is the format of a user's document (ADR 0006): the
// blocks of its extraction's IR, each marked with where it came from, plus
// the blocks the user added. The extraction IR itself stays frozen.
package doccontent

import (
	"fmt"
	"maps"
	"slices"

	"github.com/google/uuid"

	"github.com/rokumate/ronote/internal/ir"
)

// Version is the doc_version this package reads and writes.
const Version = 1

// Origin says where a top-level block came from.
type Origin string

const (
	// OriginSource is a block of the page, possibly edited.
	OriginSource Origin = "source"
	// OriginUser is a block the user added.
	OriginUser Origin = "user"
)

// Doc is a user's document content.
type Doc struct {
	DocVersion int     `json:"doc_version"`
	Blocks     []Block `json:"blocks"`
	// CodeLangs are the languages the user chose for code blocks, by block
	// id, over the page's. An optional field added to version 1, which
	// readers that do not know it ignore (ADR 0006). The code never changes.
	CodeLangs map[string]string `json:"code_langs,omitempty"`
}

// Block is a top-level block: an IR block and its origin. Origin and edits
// are tracked at the top level only; a list, callout, table or tabs block is
// one unit.
type Block struct {
	ir.Block
	Origin Origin `json:"origin"`
	// Edited is set on source blocks only: whether the user changed it.
	Edited *bool `json:"edited,omitempty"`
	// SourceBlockID is set on source blocks only: the id of the extraction's
	// block it came from, which is also its own id.
	SourceBlockID string `json:"source_block_id,omitempty"`
}

// CodeLangs are the languages a user may choose for a code block, in the
// spec's CodeLang order; plaintext means no highlighting.
var CodeLangs = []string{
	"go", "python", "javascript", "typescript", "tsx", "jsx", "bash", "console", "c", "cpp",
	"csharp", "java", "kotlin", "rust", "swift", "ruby", "php", "sql", "json", "yaml",
	"toml", "ini", "xml", "html", "css", "dockerfile", "makefile", "markdown", "diff", "plaintext",
}

// UserKinds are the kinds a user block may use, nested blocks included.
var UserKinds = []ir.Kind{ir.KindParagraph, ir.KindHeading, ir.KindList, ir.KindQuote, ir.KindCallout, ir.KindDivider}

// Code is a stable reason a document cannot be saved. The values are the
// API's error codes.
type Code string

// The codes, one per rule in ADR 0006.
const (
	CodeInvalidContent      Code = "invalid_content"
	CodeBlockKindNotAllowed Code = "block_kind_not_allowed"
	CodeUnknownSourceBlock  Code = "unknown_source_block"
	CodeSourceBlockModified Code = "source_block_modified"
)

// Problem is why a document cannot be saved.
type Problem struct {
	Code Code
	// Err says what and where, such as "blocks[3]: ...".
	Err error
}

func (p *Problem) Error() string { return fmt.Sprintf("%s: %v", p.Code, p.Err) }

func (p *Problem) Unwrap() error { return p.Err }

func problem(code Code, format string, args ...any) *Problem {
	return &Problem{Code: code, Err: fmt.Errorf(format, args...)}
}

// FromIR makes the content of a new document: every block of the
// extraction, as an unedited source block.
func FromIR(doc *ir.Doc) *Doc {
	blocks := make([]Block, len(doc.Blocks))
	for i, b := range doc.Blocks {
		edited := false
		blocks[i] = Block{Block: b, Origin: OriginSource, Edited: &edited, SourceBlockID: b.ID}
	}
	return &Doc{DocVersion: Version, Blocks: blocks}
}

// Validate checks doc's shape: the IR rules for every block, the origin
// fields, the kinds user blocks may use, and the code languages the user
// chose. It returns a *Problem or nil.
func Validate(doc *Doc) error {
	if doc.DocVersion != Version {
		return problem(CodeInvalidContent, "doc_version is %d, want %d", doc.DocVersion, Version)
	}
	irBlocks := make([]ir.Block, len(doc.Blocks))
	for i := range doc.Blocks {
		irBlocks[i] = doc.Blocks[i].Block
	}
	if err := ir.ValidateBlocks(irBlocks); err != nil {
		return &Problem{Code: CodeInvalidContent, Err: err}
	}
	for i := range doc.Blocks {
		if err := validateOrigin(fmt.Sprintf("blocks[%d]", i), &doc.Blocks[i]); err != nil {
			return err
		}
	}
	if err := validateCodeLangs(doc); err != nil {
		return err
	}
	return nil
}

// validateCodeLangs checks every chosen language is one of CodeLangs, for
// a code block of doc at any depth.
func validateCodeLangs(doc *Doc) *Problem {
	code := map[string]bool{}
	var walk func(b *ir.Block)
	walk = func(b *ir.Block) {
		if b.Kind == ir.KindCode {
			code[b.ID] = true
		}
		for _, child := range children(b) {
			walk(child)
		}
	}
	for i := range doc.Blocks {
		walk(&doc.Blocks[i].Block)
	}
	for _, id := range slices.Sorted(maps.Keys(doc.CodeLangs)) {
		switch lang := doc.CodeLangs[id]; {
		case !code[id]:
			return problem(CodeInvalidContent, "code_langs: %q is not a code block of the document", id)
		case !slices.Contains(CodeLangs, lang):
			return problem(CodeInvalidContent, "code_langs[%q]: %q is not a language RoNote offers", id, lang)
		}
	}
	return nil
}

func validateOrigin(path string, b *Block) *Problem {
	switch b.Origin {
	case OriginSource:
		switch {
		case b.Edited == nil:
			return problem(CodeInvalidContent, "%s: source block has no edited flag", path)
		case b.SourceBlockID == "":
			return problem(CodeInvalidContent, "%s: source block has no source_block_id", path)
		case b.SourceBlockID != b.ID:
			return problem(CodeInvalidContent, "%s: source block id %q is not its source_block_id %q", path, b.ID, b.SourceBlockID)
		}
	case OriginUser:
		if b.Edited != nil || b.SourceBlockID != "" {
			return problem(CodeInvalidContent, "%s: user block has source fields", path)
		}
		if id, err := uuid.Parse(b.ID); err != nil || id.Version() != 7 {
			return problem(CodeInvalidContent, "%s: user block id %q is not a UUIDv7", path, b.ID)
		}
		if kind := disallowedKind(&b.Block); kind != "" {
			return problem(CodeBlockKindNotAllowed, "%s: user blocks cannot hold %s blocks", path, kind)
		}
	default:
		return problem(CodeInvalidContent, "%s: unknown origin %q", path, b.Origin)
	}
	return nil
}

// disallowedKind returns the first kind in b, b included, that user blocks
// may not use, or "".
func disallowedKind(b *ir.Block) ir.Kind {
	if !slices.Contains(UserKinds, b.Kind) {
		return b.Kind
	}
	for _, child := range children(b) {
		if kind := disallowedKind(child); kind != "" {
			return kind
		}
	}
	return ""
}

// children returns the blocks directly inside b: its children and those of
// its items.
func children(b *ir.Block) []*ir.Block {
	var out []*ir.Block
	for i := range b.Children {
		out = append(out, &b.Children[i])
	}
	for i := range b.Items {
		for j := range b.Items[i].Children {
			out = append(out, &b.Items[i].Children[j])
		}
	}
	return out
}
