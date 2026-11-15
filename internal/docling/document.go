// Package docling turns a PDF into IR with docling-serve
// (docs/adr/0013-pdf-import.md): Document is the part of docling's JSON
// RoNote reads, Map maps it to IR, and code text comes from the text layer
// through pdftext, since docling joins a code block's lines.
package docling

import (
	"fmt"
	"strconv"
	"strings"
)

// Document is the subset of a DoclingDocument that Map reads.
type Document struct {
	Body     Node            `json:"body"`
	Groups   []Node          `json:"groups"`
	Texts    []Item          `json:"texts"`
	Pictures []Item          `json:"pictures"`
	Tables   []Table         `json:"tables"`
	Pages    map[string]Page `json:"pages"`
}

// Ref points at another node, as "#/texts/3".
type Ref struct {
	Ref string `json:"$ref"`
}

// Node is what every item and group has: where it is and what it holds.
type Node struct {
	SelfRef      string `json:"self_ref"`
	Children     []Ref  `json:"children"`
	Label        string `json:"label"`
	ContentLayer string `json:"content_layer"`
}

// Item is a text, a picture or a table's common part.
type Item struct {
	Node
	Text         string `json:"text"`
	Level        int    `json:"level"`
	CodeLanguage string `json:"code_language"`
	Enumerated   bool   `json:"enumerated"`
	Prov         []Prov `json:"prov"`
	Captions     []Ref  `json:"captions"`
}

// Prov is where on which page an item was found.
type Prov struct {
	PageNo int  `json:"page_no"`
	BBox   BBox `json:"bbox"`
}

// BBox is a box in PDF points.
type BBox struct {
	L           float64 `json:"l"`
	T           float64 `json:"t"`
	R           float64 `json:"r"`
	B           float64 `json:"b"`
	CoordOrigin string  `json:"coord_origin"`
}

// Table is a table item and its cells.
type Table struct {
	Item
	Data struct {
		NumRows    int    `json:"num_rows"`
		NumCols    int    `json:"num_cols"`
		TableCells []Cell `json:"table_cells"`
	} `json:"data"`
}

// Cell is one table cell, at its grid position.
type Cell struct {
	Text         string `json:"text"`
	StartRow     int    `json:"start_row_offset_idx"`
	StartCol     int    `json:"start_col_offset_idx"`
	ColumnHeader bool   `json:"column_header"`
}

// Page is one page of the PDF.
type Page struct {
	PageNo int `json:"page_no"`
	Size   struct {
		Height float64 `json:"height"`
	} `json:"size"`
}

// resolve finds the node ref points at: an item, a group, a picture or a
// table, by kind.
func (d *Document) resolve(ref string) (kind string, index int, err error) {
	parts := strings.Split(strings.TrimPrefix(ref, "#/"), "/")
	if len(parts) != 2 {
		return "", 0, fmt.Errorf("docling: bad reference %q", ref)
	}
	i, err := strconv.Atoi(parts[1])
	if err != nil || i < 0 {
		return "", 0, fmt.Errorf("docling: bad reference %q", ref)
	}
	n := map[string]int{"texts": len(d.Texts), "groups": len(d.Groups), "pictures": len(d.Pictures), "tables": len(d.Tables)}
	size, known := n[parts[0]]
	if !known || i >= size {
		return "", 0, fmt.Errorf("docling: reference %q points at nothing", ref)
	}
	return parts[0], i, nil
}
