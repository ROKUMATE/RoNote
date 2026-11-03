package ir

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
)

// AssignIDs gives every block, nested ones included, an ID derived from its
// heading path, kind and normalized text, so re-extracting the same page
// yields the same IDs and editing one block changes only its own ID and its
// containers'. Blocks that would share an ID get "-2", "-3", ... in document
// order.
func AssignIDs(blocks []Block) {
	a := idAssigner{seen: map[string]int{}}
	a.top(blocks)
}

type heading struct {
	level int
	text  string
}

type idAssigner struct {
	seen map[string]int
	path []heading // enclosing headings of the current top-level block
}

// top walks top-level blocks, keeping the heading path up to date.
func (a *idAssigner) top(blocks []Block) {
	for i := range blocks {
		b := &blocks[i]
		if b.Kind == KindHeading {
			for len(a.path) > 0 && a.path[len(a.path)-1].level >= b.Level {
				a.path = a.path[:len(a.path)-1]
			}
		}
		a.assign(b)
		if b.Kind == KindHeading {
			a.path = append(a.path, heading{level: b.Level, text: blockText(b)})
		}
	}
}

func (a *idAssigner) assign(b *Block) {
	texts := make([]string, len(a.path))
	for i, h := range a.path {
		texts[i] = h.text
	}
	sum := sha256.Sum256([]byte(strings.Join(texts, "\x1f") + "\x1e" + string(b.Kind) + "\x1e" + blockText(b)))
	id := hex.EncodeToString(sum[:])[:12]
	a.seen[id]++
	if n := a.seen[id]; n > 1 {
		id += "-" + strconv.Itoa(n)
	}
	b.ID = id

	for _, item := range b.Items {
		a.nested(item)
	}
	a.nested(b.Children)
}

func (a *idAssigner) nested(blocks []Block) {
	for i := range blocks {
		a.assign(&blocks[i])
	}
}

// blockText is the text a block's ID is derived from: prose with its
// whitespace collapsed, and code exactly as written.
func blockText(b *Block) string {
	switch b.Kind {
	case KindHeading, KindParagraph:
		return normalize(inlineText(b.Content))
	case KindCode:
		return b.Lang + "\n" + normalize(b.Title) + "\n" + b.Text
	case KindList:
		var parts []string
		for _, item := range b.Items {
			parts = append(parts, blocksText(item))
		}
		return strings.Join(parts, "\n")
	case KindTable:
		return normalize(rowsText(b.Head) + "\n" + rowsText(b.Rows))
	case KindCallout:
		return string(b.Variant) + "\n" + normalize(b.Title) + "\n" + blocksText(b.Children)
	case KindImage:
		return b.Src + "\n" + normalize(b.Alt) + "\n" + normalize(inlineText(b.Caption))
	case KindQuote:
		return blocksText(b.Children)
	default:
		return ""
	}
}

func blocksText(blocks []Block) string {
	parts := make([]string, len(blocks))
	for i := range blocks {
		parts[i] = blockText(&blocks[i])
	}
	return strings.Join(parts, "\n")
}

func rowsText(rows [][]Cell) string {
	var b strings.Builder
	for _, row := range rows {
		for _, cell := range row {
			b.WriteString(inlineText(cell))
			b.WriteByte('\t')
		}
		b.WriteByte('\n')
	}
	return b.String()
}

func inlineText(runs []Inline) string {
	var b strings.Builder
	for _, run := range runs {
		b.WriteString(run.Text)
	}
	return b.String()
}

func normalize(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
