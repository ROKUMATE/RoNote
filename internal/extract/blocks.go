package extract

import (
	"slices"
	"strconv"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"github.com/rokumate/ronote/internal/ir"
)

// markOf maps inline formatting elements to marks.
var markOf = map[atom.Atom]ir.Mark{
	atom.B:      ir.MarkBold,
	atom.Strong: ir.MarkBold,
	atom.I:      ir.MarkItalic,
	atom.Em:     ir.MarkItalic,
	atom.Var:    ir.MarkItalic,
	atom.Cite:   ir.MarkItalic,
	atom.Dfn:    ir.MarkItalic,
	atom.Code:   ir.MarkCode,
	atom.Tt:     ir.MarkCode,
	atom.Samp:   ir.MarkCode,
	atom.Kbd:    ir.MarkKbd,
	atom.S:      ir.MarkStrike,
	atom.Del:    ir.MarkStrike,
	atom.Strike: ir.MarkStrike,
	atom.Sub:    ir.MarkSub,
	atom.Sup:    ir.MarkSup,
}

// markOrder is the order marks are listed in on a run.
var markOrder = []ir.Mark{ir.MarkBold, ir.MarkItalic, ir.MarkCode, ir.MarkKbd, ir.MarkStrike, ir.MarkSub, ir.MarkSup}

// blockTags start a new block: text before and after them is never joined.
var blockTags = map[atom.Atom]bool{
	atom.Address: true, atom.Article: true, atom.Blockquote: true, atom.Center: true,
	atom.Dd: true, atom.Details: true, atom.Div: true, atom.Dl: true, atom.Dt: true,
	atom.Fieldset: true, atom.Figcaption: true, atom.Figure: true, atom.Footer: true,
	atom.H1: true, atom.H2: true, atom.H3: true, atom.H4: true, atom.H5: true, atom.H6: true,
	atom.Header: true, atom.Hr: true, atom.Li: true, atom.Main: true, atom.Ol: true,
	atom.P: true, atom.Pre: true, atom.Section: true, atom.Summary: true, atom.Table: true,
	atom.Tbody: true, atom.Td: true, atom.Tfoot: true, atom.Th: true, atom.Thead: true,
	atom.Tr: true, atom.Ul: true,
}

// blocks converts the children of n.
func (e *extractor) blocks(n *html.Node) []ir.Block {
	w := walker{e: e}
	w.children(n)
	w.flush()
	return w.out
}

// inline converts the content of n to inline runs only, as headings, table
// cells and captions hold. Blocks inside n become runs separated by spaces.
func (e *extractor) inline(n *html.Node) []ir.Inline {
	w := walker{e: e, inlineOnly: true}
	w.children(n)
	return w.in.take()
}

// walker turns a stretch of the DOM into blocks. Inline content gathers in
// in until a block element ends the current paragraph.
type walker struct {
	e          *extractor
	out        []ir.Block
	in         inliner
	inlineOnly bool
}

func (w *walker) children(n *html.Node) {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		w.node(c)
	}
}

func (w *walker) node(n *html.Node) {
	switch n.Type {
	case html.TextNode:
		w.in.text(n.Data)
	case html.ElementNode:
		if !w.e.skip(n) {
			w.element(n)
		}
	}
}

// flush ends the current paragraph.
func (w *walker) flush() {
	if runs := w.in.take(); len(runs) > 0 {
		w.out = append(w.out, ir.Block{Kind: ir.KindParagraph, Content: runs})
	}
}

func (w *walker) add(b *ir.Block) {
	if b != nil {
		w.out = append(w.out, *b)
	}
}

func (w *walker) element(n *html.Node) {
	if w.inlineOnly {
		w.inlineElement(n)
		return
	}
	switch tag := n.DataAtom; {
	case tag == atom.Br:
		w.in.lineBreak()
	case tag == atom.Img:
		w.flush()
		w.add(w.e.image(n, nil))
	case tag == atom.A:
		w.withLink(n, func() { w.children(n) })
	case markOf[tag] != "":
		w.in.marks = append(w.in.marks, markOf[tag])
		w.children(n)
		w.in.marks = w.in.marks[:len(w.in.marks)-1]
	case !blockTags[tag]:
		w.children(n)
	default:
		w.flush()
		w.block(n)
		w.flush()
	}
}

// inlineElement handles n when only inline runs are wanted.
func (w *walker) inlineElement(n *html.Node) {
	switch tag := n.DataAtom; {
	case tag == atom.Br:
		w.in.lineBreak()
	case tag == atom.Img:
	case tag == atom.Pre:
		w.in.marks = append(w.in.marks, ir.MarkCode)
		w.in.raw(preText(n))
		w.in.marks = w.in.marks[:len(w.in.marks)-1]
	case tag == atom.A:
		w.withLink(n, func() { w.children(n) })
	case markOf[tag] != "":
		w.in.marks = append(w.in.marks, markOf[tag])
		w.children(n)
		w.in.marks = w.in.marks[:len(w.in.marks)-1]
	case blockTags[tag]:
		w.in.text(" ")
		w.children(n)
		w.in.text(" ")
	default:
		w.children(n)
	}
}

func (w *walker) withLink(n *html.Node, f func()) {
	outer := w.in.link
	if hasAttr(n, "href") {
		w.in.link = w.e.links.link(attr(n, "href"))
	}
	f()
	w.in.link = outer
}

// block converts one block-level element.
func (w *walker) block(n *html.Node) {
	e := w.e
	switch n.DataAtom {
	case atom.H1, atom.H2, atom.H3, atom.H4, atom.H5, atom.H6:
		w.add(e.heading(n))
	case atom.P:
		w.children(n)
	case atom.Ul, atom.Ol:
		w.add(e.list(n))
	case atom.Dl:
		w.add(e.definitionList(n))
	case atom.Pre:
		if text := preText(n); text != "" {
			w.out = append(w.out, ir.Block{Kind: ir.KindCode, Lang: codeLang(n), Text: text})
		}
	case atom.Table:
		w.add(e.table(n))
	case atom.Hr:
		w.out = append(w.out, ir.Block{Kind: ir.KindDivider})
	case atom.Figure:
		w.out = append(w.out, e.figure(n)...)
	case atom.Details:
		w.out = append(w.out, e.details(n)...)
	case atom.Blockquote:
		if b := e.callout(n); b != nil {
			w.add(b)
		} else if children := e.blocks(n); len(children) > 0 {
			w.out = append(w.out, ir.Block{Kind: ir.KindQuote, Children: children})
		}
	default:
		if b := e.callout(n); b != nil {
			w.add(b)
		} else {
			w.children(n)
		}
	}
}

func (e *extractor) heading(n *html.Node) *ir.Block {
	content := e.inline(n)
	if len(content) == 0 {
		return nil
	}
	level, _ := strconv.Atoi(n.Data[1:])
	return &ir.Block{Kind: ir.KindHeading, Level: level, Anchor: e.anchor(n, content), Content: content}
}

// anchor is the heading's id, else the id of the <section> it opens, else
// the id or name of an <a> inside it, else a slug of its text. Slugs are made
// unique within the page.
func (e *extractor) anchor(n *html.Node, content []ir.Inline) string {
	if id := strings.TrimSpace(attr(n, "id")); id != "" {
		e.anchors[id] = true
		return id
	}
	if id := strings.TrimSpace(attr(n.Parent, "id")); id != "" && n.Parent.DataAtom == atom.Section && firstContent(n.Parent) == n {
		e.anchors[id] = true
		return id
	}
	if a := findFirst(n, func(c *html.Node) bool { return c.DataAtom == atom.A && (attr(c, "id") != "" || attr(c, "name") != "") }); a != nil {
		id := attr(a, "id")
		if id == "" {
			id = attr(a, "name")
		}
		e.anchors[id] = true
		return id
	}
	slug := slugify(runsText(content))
	anchor := slug
	if e.anchors[anchor] {
		// Resume where the last duplicate of this slug stopped, so many equal
		// headings stay linear.
		n := max(e.nextSuffix[slug], 2)
		for e.anchors[slug+"-"+strconv.Itoa(n)] {
			n++
		}
		anchor = slug + "-" + strconv.Itoa(n)
		e.nextSuffix[slug] = n + 1
	}
	e.anchors[anchor] = true
	return anchor
}

// firstContent is n's first child element that has text, skipping empty
// link targets such as Sphinx's <span id="..."></span>.
func firstContent(n *html.Node) *html.Node {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && collapse(textContent(c)) != "" {
			return c
		}
	}
	return nil
}

func (e *extractor) list(n *html.Node) *ir.Block {
	b := ir.Block{Kind: ir.KindList, Ordered: n.DataAtom == atom.Ol}
	var stray walker // content directly inside the list, outside any <li>
	stray.e = e
	flushStray := func() {
		stray.flush()
		if len(stray.out) > 0 {
			b.Items = append(b.Items, stray.out)
			stray.out = nil
		}
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && c.DataAtom == atom.Li && !e.skip(c) {
			flushStray()
			if item := e.blocks(c); len(item) > 0 {
				b.Items = append(b.Items, item)
			}
			continue
		}
		stray.node(c)
	}
	flushStray()
	if len(b.Items) == 0 {
		return nil
	}
	return &b
}

// definitionList turns <dl> into a list: each <dt> starts an item as a bold
// paragraph, and its <dd> blocks follow in the same item.
func (e *extractor) definitionList(n *html.Node) *ir.Block {
	b := ir.Block{Kind: ir.KindList}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type != html.ElementNode || e.skip(c) {
			continue
		}
		switch c.DataAtom {
		case atom.Dt:
			term := e.inline(c)
			if len(term) == 0 {
				continue
			}
			for i := range term {
				term[i].Marks = withMark(term[i].Marks, ir.MarkBold)
			}
			b.Items = append(b.Items, []ir.Block{{Kind: ir.KindParagraph, Content: term}})
		case atom.Dd:
			desc := e.blocks(c)
			if len(desc) == 0 {
				continue
			}
			if len(b.Items) == 0 {
				b.Items = append(b.Items, nil)
			}
			b.Items[len(b.Items)-1] = append(b.Items[len(b.Items)-1], desc...)
		default:
			if more := e.definitionList(c); more != nil {
				b.Items = append(b.Items, more.Items...)
			}
		}
	}
	if len(b.Items) == 0 {
		return nil
	}
	return &b
}

// table keeps rows from <thead> as the head and all others as rows. Without
// a <thead>, a first row of only <th> cells is the head.
func (e *extractor) table(n *html.Node) *ir.Block {
	var head, rows [][]ir.Cell
	var firstRowAllTh bool
	var visit func(n *html.Node, inHead bool)
	visit = func(n *html.Node, inHead bool) {
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type != html.ElementNode || e.skip(c) {
				continue
			}
			switch c.DataAtom {
			case atom.Thead:
				visit(c, true)
			case atom.Tbody, atom.Tfoot:
				visit(c, false)
			case atom.Tr:
				row, allTh := e.row(c)
				if len(row) == 0 {
					continue
				}
				if len(head) == 0 && len(rows) == 0 {
					firstRowAllTh = allTh
				}
				if inHead {
					head = append(head, row)
				} else {
					rows = append(rows, row)
				}
			}
		}
	}
	visit(n, false)
	if len(head) == 0 && firstRowAllTh && len(rows) > 1 {
		head, rows = rows[:1], rows[1:]
	}
	if len(head) == 0 && len(rows) == 0 {
		return nil
	}
	return &ir.Block{Kind: ir.KindTable, Head: head, Rows: rows}
}

func (e *extractor) row(tr *html.Node) (cells []ir.Cell, allTh bool) {
	allTh = true
	for c := tr.FirstChild; c != nil; c = c.NextSibling {
		if c.Type != html.ElementNode || (c.DataAtom != atom.Td && c.DataAtom != atom.Th) || e.skip(c) {
			continue
		}
		allTh = allTh && c.DataAtom == atom.Th
		cells = append(cells, ir.Cell(e.inline(c)))
	}
	return cells, allTh
}

// image converts an <img>; images whose source is not http(s) are dropped.
func (e *extractor) image(n *html.Node, caption []ir.Inline) *ir.Block {
	src := attr(n, "src")
	if strings.TrimSpace(src) == "" {
		src = attr(n, "data-src")
	}
	u, ok := e.links.resolve(src)
	if !ok {
		return nil
	}
	return &ir.Block{Kind: ir.KindImage, Src: u.String(), Alt: collapse(attr(n, "alt")), Caption: caption}
}

// figure turns an image with its <figcaption> into one image block. Other
// content inside the figure, or a figure without an image, converts as usual.
func (e *extractor) figure(n *html.Node) []ir.Block {
	img := e.findKept(n, func(c *html.Node) bool { return c.DataAtom == atom.Img })
	caption := e.findKept(n, func(c *html.Node) bool { return c.DataAtom == atom.Figcaption })
	if img == nil {
		return e.blocks(n)
	}
	var captionRuns []ir.Inline
	if caption != nil {
		captionRuns = e.inline(caption)
	}
	w := walker{e: e}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		switch {
		case c == caption:
		case c == img || (c.Type == html.ElementNode && findFirst(c, func(x *html.Node) bool { return x == img }) != nil):
			w.flush()
			w.add(e.image(img, captionRuns))
		default:
			w.node(c)
		}
	}
	w.flush()
	return w.out
}

// details converts <summary> to a bold paragraph followed by the rest.
func (e *extractor) details(n *html.Node) []ir.Block {
	w := walker{e: e}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && c.DataAtom == atom.Summary && !e.skip(c) {
			w.flush()
			if summary := e.inline(c); len(summary) > 0 {
				for i := range summary {
					summary[i].Marks = withMark(summary[i].Marks, ir.MarkBold)
				}
				w.out = append(w.out, ir.Block{Kind: ir.KindParagraph, Content: summary})
			}
			continue
		}
		w.node(c)
	}
	w.flush()
	return w.out
}

// calloutMarkers are class tokens that make an element a callout.
var calloutMarkers = []string{"admonition", "callout", "alert", "markdown-alert"}

// calloutVariants maps words found in class tokens to variants.
var calloutVariants = map[string]ir.Variant{
	"note": ir.VariantNote,
	"tip":  ir.VariantTip, "hint": ir.VariantTip, "success": ir.VariantTip,
	"info": ir.VariantInfo, "information": ir.VariantInfo, "important": ir.VariantInfo,
	"warning": ir.VariantWarning, "warn": ir.VariantWarning, "caution": ir.VariantWarning, "attention": ir.VariantWarning,
	"danger": ir.VariantDanger, "error": ir.VariantDanger, "critical": ir.VariantDanger,
}

// callout converts n if it is a callout: a class token from calloutMarkers,
// or role=note or role=alert. The variant comes from words in the class
// tokens (default note); a direct child whose class mentions "title" or
// "heading" becomes the title.
func (e *extractor) callout(n *html.Node) *ir.Block {
	classes := strings.Fields(strings.ToLower(attr(n, "class")))
	role := strings.ToLower(attr(n, "role"))
	isCallout := role == "note" || role == "alert"
	for _, c := range classes {
		isCallout = isCallout || slices.Contains(calloutMarkers, c)
	}
	if !isCallout {
		return nil
	}

	b := ir.Block{Kind: ir.KindCallout, Variant: ir.VariantNote}
findVariant:
	for _, c := range classes {
		for word := range strings.FieldsFuncSeq(c, func(r rune) bool { return r == '-' || r == '_' }) {
			if v, ok := calloutVariants[word]; ok {
				b.Variant = v
				break findVariant
			}
		}
	}

	w := walker{e: e}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if b.Title == "" && c.Type == html.ElementNode && !e.skip(c) && isTitleClass(attr(c, "class")) {
			b.Title = collapse(textContent(c))
			continue
		}
		w.node(c)
	}
	w.flush()
	b.Children = w.out
	if b.Title == "" && len(b.Children) == 0 {
		return nil
	}
	return &b
}

func isTitleClass(class string) bool {
	class = strings.ToLower(class)
	return strings.Contains(class, "title") || strings.Contains(class, "heading")
}

// withMark adds m to marks, keeping markOrder.
func withMark(marks []ir.Mark, m ir.Mark) []ir.Mark {
	if slices.Contains(marks, m) {
		return marks
	}
	var out []ir.Mark
	for _, candidate := range markOrder {
		if candidate == m || slices.Contains(marks, candidate) {
			out = append(out, candidate)
		}
	}
	return out
}

// slugify turns heading text into an anchor: lowercase letters and digits
// joined by hyphens.
func slugify(text string) string {
	var b strings.Builder
	hyphen := false
	for _, r := range strings.ToLower(text) {
		if isWordRune(r) {
			if hyphen && b.Len() > 0 {
				b.WriteByte('-')
			}
			b.WriteRune(r)
			hyphen = false
		} else {
			hyphen = true
		}
	}
	if b.Len() == 0 {
		return "section"
	}
	return b.String()
}

func runsText(runs []ir.Inline) string {
	var b strings.Builder
	for _, r := range runs {
		b.WriteString(r.Text)
	}
	return b.String()
}
