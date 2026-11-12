// Package search cuts documents into chunks, embeds them, and finds them
// again by words and by meaning (docs/adr/0011-retrieval.md).
package search

import (
	"strings"
	"unicode/utf8"

	"github.com/rokumate/ronote/internal/doccontent"
	"github.com/rokumate/ronote/internal/ir"
)

// Chunk sizes, in characters: about 4 a token, so 350 tokens is 1400. An
// embedding input is cut at maxEmbedChars, which keeps code, denser at about
// 3 characters a token, under 2048 tokens.
const (
	maxChunkChars = 1400
	maxEmbedChars = 6000
)

// Kind is what a chunk holds.
type Kind string

// Chunk kinds.
const (
	KindProse Kind = "prose"
	KindCode  Kind = "code"
	KindNote  Kind = "note"
)

// Note is a highlight with a note, as chunking reads it.
type Note struct {
	BlockID string
	Quote   string
	Text    string
}

// Chunk is a piece of a document for search.
type Chunk struct {
	// BlockIDs are its top-level blocks in order; a note's is the top-level
	// block its highlight is in.
	BlockIDs    []string
	HeadingPath []string
	Kind        Kind
	// Lang is a code chunk's language: the user's choice, else the page's.
	Lang string
	// Text is what the chunk holds, whole.
	Text string
	// Embed is what is embedded: the breadcrumb, then the text with code
	// labelled by language, cut at maxEmbedChars.
	Embed string
	// Headings, Prose and Code are the text full-text search reads, each
	// in its own configuration, and Idents the identifier tokens of its
	// code and inline code.
	Headings, Prose, Code string
	Idents                []string
}

// part is one top-level block, rendered.
type part struct {
	id         string
	prose      string // its text outside code
	code       string // its code, blocks apart
	embed      string // as the embedding reads it
	identsFrom string // code and inline code
	isCode     bool
	lang       string
	// size is the length of its text in a chunk's Text.
	size int
}

// text is the part as a chunk's Text holds it.
func (p part) text() string {
	return strings.TrimSpace(p.prose + "\n\n" + p.code)
}

// Chunks cuts content into chunks: consecutive top-level blocks under the
// same headings, up to about 350 tokens, a code block never split, and one
// chunk per note. title starts every breadcrumb.
func Chunks(title string, content *doccontent.Doc, notes []Note) []Chunk {
	c := chunker{title: title, langs: content.CodeLangs, topOf: map[string]int{}}
	for i := range content.Blocks {
		b := &content.Blocks[i].Block
		walk(b, func(inner *ir.Block) { c.topOf[inner.ID] = i })
		heading := b.Kind == ir.KindHeading
		if heading {
			c.flush()
			c.heading(b)
		}
		c.pathAt = append(c.pathAt, c.headingPath())
		if !heading {
			c.add(c.render(b))
		}
	}
	c.flush()
	for _, n := range notes {
		c.note(content, n)
	}
	return c.out
}

type chunker struct {
	title string
	langs map[string]string
	path  []headingAt
	// topOf maps every block id to its top-level block's index; pathAt
	// holds the heading path in force at each top-level index.
	topOf  map[string]int
	pathAt [][]string
	open   []part
	out    []Chunk
}

type headingAt struct {
	level int
	text  string
}

// heading starts a section: it replaces the headings at its level and below.
func (c *chunker) heading(b *ir.Block) {
	for len(c.path) > 0 && c.path[len(c.path)-1].level >= b.Level {
		c.path = c.path[:len(c.path)-1]
	}
	text := inlineText(b.Content)
	// A page's own title repeated as its first heading is the breadcrumb's
	// start already.
	if len(c.path) > 0 || !strings.EqualFold(strings.TrimSpace(text), strings.TrimSpace(c.title)) {
		c.path = append(c.path, headingAt{level: b.Level, text: strings.TrimSpace(text)})
	}
}

func (c *chunker) headingPath() []string {
	path := make([]string, len(c.path))
	for i, h := range c.path {
		path[i] = h.text
	}
	return path
}

// add puts p in the open chunk, starting a new one when p wouldn't fit. A
// block larger than a chunk is a chunk of its own.
func (c *chunker) add(p part) {
	if p.size == 0 {
		return
	}
	// Parts join with a blank line.
	open := 0
	for _, q := range c.open {
		open += q.size + 2
	}
	if len(c.open) > 0 && open+p.size > maxChunkChars {
		c.flush()
	}
	c.open = append(c.open, p)
	if p.size > maxChunkChars {
		c.flush()
	}
}

// flush closes the open chunk.
func (c *chunker) flush() {
	if len(c.open) == 0 {
		return
	}
	ch := Chunk{HeadingPath: c.headingPath(), Kind: KindCode}
	var text, prose, code, embed, idents []string
	for _, p := range c.open {
		ch.BlockIDs = append(ch.BlockIDs, p.id)
		if !p.isCode {
			ch.Kind = KindProse
		}
		text = appendNonEmpty(text, p.text())
		prose = appendNonEmpty(prose, p.prose)
		code = appendNonEmpty(code, p.code)
		embed = appendNonEmpty(embed, p.embed)
		idents = appendNonEmpty(idents, p.identsFrom)
	}
	if ch.Kind == KindCode && len(c.open) == 1 {
		ch.Lang = c.open[0].lang
	}
	ch.Text = strings.Join(text, "\n\n")
	ch.Headings = strings.Join(ch.HeadingPath, "\n")
	ch.Prose = strings.Join(prose, "\n\n")
	ch.Code = strings.Join(code, "\n\n")
	ch.Embed = cut(c.breadcrumb(ch.HeadingPath)+"\n\n"+strings.Join(embed, "\n\n"), maxEmbedChars)
	ch.Idents = Idents(strings.Join(idents, "\n"))
	c.out = append(c.out, ch)
	c.open = nil
}

func (c *chunker) breadcrumb(path []string) string {
	return strings.Join(append([]string{c.title}, path...), " > ")
}

// note makes a note's chunk, under the headings of its highlight's block.
func (c *chunker) note(content *doccontent.Doc, n Note) {
	top, ok := c.topOf[n.BlockID]
	if !ok || strings.TrimSpace(n.Text) == "" {
		return
	}
	path := c.pathAt[top]
	c.out = append(c.out, Chunk{
		BlockIDs:    []string{content.Blocks[top].ID},
		HeadingPath: path,
		Kind:        KindNote,
		Text:        n.Text,
		Embed:       cut(c.breadcrumb(path)+"\n\nNote on \""+n.Quote+"\": "+n.Text, maxEmbedChars),
		Headings:    strings.Join(path, "\n"),
		Prose:       n.Quote + "\n\n" + n.Text,
	})
}

// render reads a top-level block as text.
func (c *chunker) render(b *ir.Block) part {
	p := part{id: b.ID}
	var prose, code, embed, idents strings.Builder
	var visit func(b *ir.Block)
	visit = func(b *ir.Block) {
		switch b.Kind {
		case ir.KindCode:
			lang := c.lang(b)
			code.WriteString(b.Text + "\n\n")
			label := "Code"
			if lang != "" {
				label += " (" + lang + ")"
			}
			if b.Title != "" {
				label += ", " + b.Title
			}
			embed.WriteString(label + ":\n" + b.Text + "\n\n")
			idents.WriteString(b.Text + "\n")
		case ir.KindHeading, ir.KindParagraph:
			text := inlineText(b.Content)
			prose.WriteString(text + "\n")
			embed.WriteString(text + "\n")
			idents.WriteString(inlineCode(b.Content))
		case ir.KindTable:
			for _, row := range append(b.Head, b.Rows...) {
				cells := make([]string, len(row))
				for i, cell := range row {
					cells[i] = inlineText(cell)
					idents.WriteString(inlineCode(cell))
				}
				line := strings.Join(cells, " | ") + "\n"
				prose.WriteString(line)
				embed.WriteString(line)
			}
		case ir.KindImage:
			text := strings.TrimSpace(b.Alt + " " + inlineText(b.Caption))
			if text != "" {
				prose.WriteString(text + "\n")
				embed.WriteString(text + "\n")
			}
		case ir.KindCallout:
			if b.Title != "" {
				prose.WriteString(b.Title + "\n")
				embed.WriteString(b.Title + "\n")
			}
		case ir.KindList, ir.KindQuote, ir.KindTabs, ir.KindDivider:
		}
		for _, item := range b.Items {
			if item.Label != "" {
				prose.WriteString(item.Label + "\n")
				embed.WriteString(item.Label + "\n")
			}
			for i := range item.Children {
				visit(&item.Children[i])
			}
		}
		for i := range b.Children {
			visit(&b.Children[i])
		}
	}
	visit(b)
	p.prose = strings.TrimSpace(prose.String())
	p.code = strings.TrimSpace(code.String())
	p.embed = strings.TrimSpace(embed.String())
	p.identsFrom = idents.String()
	p.isCode = b.Kind == ir.KindCode
	if p.isCode {
		p.lang = c.lang(b)
	}
	p.size = utf8.RuneCountInString(p.text())
	return p
}

// lang is a code block's language: the user's choice over the page's.
func (c *chunker) lang(b *ir.Block) string {
	if l, ok := c.langs[b.ID]; ok && l != "plaintext" {
		return l
	}
	return b.Lang
}

// walk calls fn on b and every block inside it.
func walk(b *ir.Block, fn func(*ir.Block)) {
	fn(b)
	for i := range b.Children {
		walk(&b.Children[i], fn)
	}
	for _, item := range b.Items {
		for i := range item.Children {
			walk(&item.Children[i], fn)
		}
	}
}

func inlineText(runs []ir.Inline) string {
	var sb strings.Builder
	for _, r := range runs {
		sb.WriteString(r.Text)
	}
	return sb.String()
}

// inlineCode is the text of runs marked code or kbd, a line each.
func inlineCode(runs []ir.Inline) string {
	var sb strings.Builder
	for _, r := range runs {
		for _, m := range r.Marks {
			if m == ir.MarkCode || m == ir.MarkKbd {
				sb.WriteString(r.Text + "\n")
				break
			}
		}
	}
	return sb.String()
}

func appendNonEmpty(list []string, s string) []string {
	if s = strings.TrimSpace(s); s != "" {
		return append(list, s)
	}
	return list
}

// cut shortens s to at most n characters, at a character boundary.
func cut(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}
