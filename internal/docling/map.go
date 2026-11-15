package docling

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/rokumate/ronote/internal/ir"
)

// MapVersion is the mapping's version, part of the extractor version an
// extraction records. Bump it when Map's output changes.
const MapVersion = "1"

// ErrNoText means the PDF has pages but docling found no text on them: a
// scan, which needs OCR (scanned_pdf_unsupported).
var ErrNoText = errors.New("docling: the PDF has no text layer")

// PDF is what Map records about the file it maps.
type PDF struct {
	Filename    string
	SHA256      string
	ConvertedAt time.Time
	// Docling is the docling-serve version that converted it.
	Docling string
}

// ligatures expands the Unicode ligatures a text layer may hold: the one
// change Map makes to the PDF's text (docs/adr/0013-pdf-import.md).
var ligatures = strings.NewReplacer("ﬀ", "ff", "ﬁ", "fi", "ﬂ", "fl", "ﬃ", "ffi", "ﬄ", "ffl")

// Box is an item's box on one page, as pdftext takes it: PDF points, origin
// bottom left. Ref names the item it belongs to.
type Box struct {
	Ref  string  `json:"-"`
	Page int     `json:"page"`
	L    float64 `json:"l"`
	T    float64 `json:"t"`
	R    float64 `json:"r"`
	B    float64 `json:"b"`
}

// layered are the labels of the items whose text layer Map reads: code,
// for its lines, and prose and pictures, which may be code docling's
// layout model took for something else.
var layered = []string{"code", "text", "paragraph", "picture"}

// LayerBoxes returns the boxes of d's code, prose and pictures, in reading
// order: one per page an item is on.
func LayerBoxes(d *Document) []Box {
	var boxes []Box
	add := func(it *Item) {
		if !slices.Contains(layered, it.Label) || it.ContentLayer == "furniture" {
			return
		}
		for _, p := range it.Prov {
			b := Box{Ref: it.SelfRef, Page: p.PageNo, L: p.BBox.L, T: p.BBox.T, R: p.BBox.R, B: p.BBox.B}
			if p.BBox.CoordOrigin == "TOPLEFT" {
				height := d.Pages[strconv.Itoa(p.PageNo)].Size.Height
				b.T, b.B = height-p.BBox.T, height-p.BBox.B
			}
			boxes = append(boxes, b)
		}
	}
	for i := range d.Texts {
		add(&d.Texts[i])
	}
	for i := range d.Pictures {
		add(&d.Pictures[i])
	}
	return boxes
}

// Layer gathers the lines pdftext read from each box by the item they
// belong to. blocks holds a list of lines per box, in the boxes' order.
func Layer(boxes []Box, blocks [][]Line) (map[string][]Line, error) {
	if len(blocks) != len(boxes) {
		return nil, fmt.Errorf("docling: %d boxes but lines for %d", len(boxes), len(blocks))
	}
	lines := map[string][]Line{}
	for i, block := range blocks {
		lines[boxes[i].Ref] = append(lines[boxes[i].Ref], block...)
	}
	return lines, nil
}

// allMono reports whether lines hold text, all of it set in monospace.
func allMono(lines []Line) bool {
	text := false
	for _, l := range lines {
		if strings.TrimSpace(l.Text) == "" {
			continue
		}
		if !l.Mono {
			return false
		}
		text = true
	}
	return text
}

// Map turns d into IR. layer holds the text layer's lines of code, prose
// and pictures, by reference:
//   - a code block's text is its monospace lines; its prose lines, such as
//     a label docling's box took in, are paragraphs. A code block with no
//     lines keeps docling's text, whose lines docling joined with spaces;
//   - prose, or a picture, whose lines are all monospace is code docling's
//     layout model took for something else, and is code;
//   - headers and footers are dropped, and other pictures too, keeping their
//     captions and counting them in a figures_omitted warning.
//
// It returns ErrNoText for a PDF with no text.
func Map(d *Document, pdf PDF, layer map[string][]Line) (*ir.Doc, error) {
	m := mapper{d: d, layer: layer, seen: map[string]bool{}}
	for _, child := range d.Body.Children {
		if err := m.node(child.Ref, &m.blocks); err != nil {
			return nil, err
		}
	}
	if !m.text {
		return nil, ErrNoText
	}
	title := m.title
	if title == "" {
		title = strings.TrimSuffix(pdf.Filename, ".pdf")
		title = strings.TrimSuffix(title, ".PDF")
	}
	doc := &ir.Doc{
		IRVersion: ir.Version,
		Title:     title,
		Source:    ir.Source{FetchedAt: pdf.ConvertedAt.UTC(), BodySHA256: pdf.SHA256, PDF: &ir.PDF{Filename: pdf.Filename, Pages: len(d.Pages)}},
		Extractor: ir.Extractor{Name: "docling", Version: pdf.Docling + "+map" + MapVersion},
		Blocks:    m.blocks,
	}
	if m.figures > 0 {
		doc.Warnings = []ir.Warning{{
			Code: ir.WarningFiguresOmitted, Count: m.figures,
			Message: fmt.Sprintf("%d figures from the PDF are not shown.", m.figures),
		}}
	}
	ir.AssignIDs(doc.Blocks)
	if err := ir.Validate(doc); err != nil {
		return nil, fmt.Errorf("docling: mapped IR is not valid: %w", err)
	}
	return doc, nil
}

type mapper struct {
	d       *Document
	layer   map[string][]Line
	blocks  []ir.Block
	title   string
	figures int
	text    bool // whether any text was found
	// seen holds the references mapped: each is mapped once, so a caption
	// listed twice shows once and a group can't hold itself.
	seen map[string]bool
}

// visit reports whether ref is new, and marks it seen.
func (m *mapper) visit(ref string) bool {
	if m.seen[ref] {
		return false
	}
	m.seen[ref] = true
	return true
}

// node maps the node ref points at onto out.
func (m *mapper) node(ref string, out *[]ir.Block) error {
	kind, i, err := m.d.resolve(ref)
	if err != nil || !m.visit(ref) {
		return err
	}
	switch kind {
	case "texts":
		m.textItem(&m.d.Texts[i], out)
	case "groups":
		return m.group(&m.d.Groups[i], out)
	case "pictures":
		p := &m.d.Pictures[i]
		if p.ContentLayer == "furniture" {
			return nil
		}
		if lines := m.layer[p.SelfRef]; allMono(lines) {
			m.text = true
			m.codeRuns(lines, firstPage(p.Prov), "", out)
			return nil
		}
		m.figures++
		return m.captions(p.Captions, out)
	case "tables":
		return m.table(&m.d.Tables[i], out)
	}
	return nil
}

func (m *mapper) textItem(t *Item, out *[]ir.Block) {
	if t.ContentLayer == "furniture" {
		return
	}
	text := ligatures.Replace(t.Text)
	if strings.TrimSpace(text) == "" {
		return
	}
	m.text = true
	page := firstPage(t.Prov)
	switch t.Label {
	case "page_header", "page_footer":
		return
	case "title":
		if m.title == "" {
			m.title = strings.TrimSpace(text)
		}
		*out = append(*out, ir.Block{Kind: ir.KindHeading, Page: page, Level: 1, Content: runs(text)})
	case "section_header":
		*out = append(*out, ir.Block{Kind: ir.KindHeading, Page: page, Level: min(max(t.Level, 1), 6), Content: runs(text)})
	case "code":
		if lines := m.layer[t.SelfRef]; len(lines) > 0 {
			m.codeRuns(lines, page, language(t.CodeLanguage), out)
			return
		}
		*out = append(*out, ir.Block{Kind: ir.KindCode, Page: page, Lang: language(t.CodeLanguage), Text: text})
	case "list_item":
		// A list item outside a list group is a list of one.
		*out = append(*out, ir.Block{Kind: ir.KindList, Page: page, Items: []ir.Item{{Children: []ir.Block{{Kind: ir.KindParagraph, Page: page, Content: runs(text)}}}}})
	case "text", "paragraph":
		if lines := m.layer[t.SelfRef]; allMono(lines) {
			m.codeRuns(lines, page, "", out)
			return
		}
		*out = append(*out, ir.Block{Kind: ir.KindParagraph, Page: page, Content: runs(text)})
	default:
		*out = append(*out, ir.Block{Kind: ir.KindParagraph, Page: page, Content: runs(text)})
	}
}

// codeRuns maps an item's lines: each run of monospace lines is a code
// block, without blank lines at its ends, and each run of prose lines a
// paragraph, its lines joined by a space.
func (m *mapper) codeRuns(lines []Line, page int, lang string, out *[]ir.Block) {
	for len(lines) > 0 {
		mono := lines[0].Mono
		n := 1
		for n < len(lines) && (lines[n].Mono == mono || strings.TrimSpace(lines[n].Text) == "") {
			n++
		}
		run := lines[:n]
		lines = lines[n:]
		texts := make([]string, 0, len(run))
		for _, l := range run {
			texts = append(texts, ligatures.Replace(l.Text))
		}
		if mono {
			for len(texts) > 0 && strings.TrimSpace(texts[len(texts)-1]) == "" {
				texts = texts[:len(texts)-1]
			}
			for len(texts) > 0 && strings.TrimSpace(texts[0]) == "" {
				texts = texts[1:]
			}
			if len(texts) > 0 {
				*out = append(*out, ir.Block{Kind: ir.KindCode, Page: page, Lang: lang, Text: strings.Join(texts, "\n") + "\n"})
			}
			continue
		}
		var prose []string
		for _, t := range texts {
			if t = strings.TrimSpace(t); t != "" {
				prose = append(prose, t)
			}
		}
		if len(prose) > 0 {
			*out = append(*out, ir.Block{Kind: ir.KindParagraph, Page: page, Content: runs(strings.Join(prose, " "))})
		}
	}
}

// group maps a list group to a list, an inline group to one paragraph, and
// any other group to its children.
func (m *mapper) group(g *Node, out *[]ir.Block) error {
	if g.ContentLayer == "furniture" {
		return nil
	}
	switch g.Label {
	case "list", "ordered_list":
		return m.list(g, out)
	case "inline":
		var parts []string
		page := 0
		for _, child := range g.Children {
			kind, i, err := m.d.resolve(child.Ref)
			if err != nil {
				return err
			}
			if kind != "texts" || !m.visit(child.Ref) {
				continue
			}
			if t := strings.TrimSpace(ligatures.Replace(m.d.Texts[i].Text)); t != "" {
				parts = append(parts, t)
				if page == 0 {
					page = firstPage(m.d.Texts[i].Prov)
				}
			}
		}
		if len(parts) > 0 {
			m.text = true
			*out = append(*out, ir.Block{Kind: ir.KindParagraph, Page: page, Content: runs(strings.Join(parts, " "))})
		}
		return nil
	default:
		for _, child := range g.Children {
			if err := m.node(child.Ref, out); err != nil {
				return err
			}
		}
		return nil
	}
}

// list maps a list group: each list item is an item holding its text and
// whatever the item contains, such as a nested list.
func (m *mapper) list(g *Node, out *[]ir.Block) error {
	list := ir.Block{Kind: ir.KindList, Ordered: g.Label == "ordered_list"}
	for _, child := range g.Children {
		kind, i, err := m.d.resolve(child.Ref)
		if err != nil {
			return err
		}
		if kind != "texts" || m.d.Texts[i].Label != "list_item" {
			// Anything else in a list, such as a nested list with no item
			// before it, goes in an item of its own.
			var inner []ir.Block
			if err := m.node(child.Ref, &inner); err != nil {
				return err
			}
			if len(inner) > 0 {
				list.Items = append(list.Items, ir.Item{Children: inner})
			}
			continue
		}
		if !m.visit(child.Ref) {
			continue
		}
		t := &m.d.Texts[i]
		list.Ordered = list.Ordered || t.Enumerated
		var children []ir.Block
		if text := ligatures.Replace(t.Text); strings.TrimSpace(text) != "" {
			m.text = true
			children = append(children, ir.Block{Kind: ir.KindParagraph, Page: firstPage(t.Prov), Content: runs(text)})
		}
		for _, nested := range t.Children {
			if err := m.node(nested.Ref, &children); err != nil {
				return err
			}
		}
		if len(children) > 0 {
			list.Items = append(list.Items, ir.Item{Children: children})
		}
	}
	if len(list.Items) == 0 {
		return nil
	}
	list.Page = list.Items[0].Children[0].Page
	*out = append(*out, list)
	return nil
}

// table maps a table's grid, with its leading all-header rows as the head,
// after its captions.
func (m *mapper) table(t *Table, out *[]ir.Block) error {
	if t.ContentLayer == "furniture" {
		return nil
	}
	if err := m.captions(t.Captions, out); err != nil {
		return err
	}
	rows, cols := t.Data.NumRows, t.Data.NumCols
	if rows == 0 || cols == 0 {
		return nil
	}
	grid := make([][]ir.Cell, rows)
	for r := range grid {
		grid[r] = make([]ir.Cell, cols)
	}
	for _, c := range t.Data.TableCells {
		if c.StartRow < 0 || c.StartRow >= rows || c.StartCol < 0 || c.StartCol >= cols {
			continue
		}
		if text := ligatures.Replace(c.Text); strings.TrimSpace(text) != "" {
			m.text = true
			grid[c.StartRow][c.StartCol] = runs(text)
		}
	}
	// A row is a header row when it has cells and all are column headers.
	header := func(r int) bool {
		cells := 0
		for _, c := range t.Data.TableCells {
			if c.StartRow == r {
				if !c.ColumnHeader {
					return false
				}
				cells++
			}
		}
		return cells > 0
	}
	head := 0
	for head < rows && header(head) {
		head++
	}
	*out = append(*out, ir.Block{Kind: ir.KindTable, Page: firstPage(t.Prov), Head: grid[:head], Rows: grid[head:]})
	return nil
}

func (m *mapper) captions(refs []Ref, out *[]ir.Block) error {
	for _, ref := range refs {
		kind, i, err := m.d.resolve(ref.Ref)
		if err != nil {
			return err
		}
		if kind == "texts" && m.visit(ref.Ref) {
			c := m.d.Texts[i]
			c.Label = "caption"
			m.textItem(&c, out)
		}
	}
	return nil
}

// runs is text as one run.
func runs(text string) []ir.Inline {
	return []ir.Inline{{Text: text}}
}

func firstPage(prov []Prov) int {
	if len(prov) == 0 {
		return 1
	}
	return prov[0].PageNo
}

// language is docling's code language in RoNote's spelling, or "" when
// docling doesn't know it.
func language(l string) string {
	l = strings.ToLower(strings.TrimSpace(l))
	if slices.Contains([]string{"", "unknown", "none", "text"}, l) {
		return ""
	}
	return l
}
