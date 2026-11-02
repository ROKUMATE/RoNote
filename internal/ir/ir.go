// Package ir is the document model every importer produces: a closed set of
// blocks holding inline text runs, with no raw HTML anywhere.
// docs/adr/0004-ir-and-links.md records the rules.
package ir

import "time"

// Version is the IR version this package reads and writes.
const Version = 1

// Kind names a block type. The set is closed: Validate rejects other kinds.
type Kind string

// Block kinds.
const (
	KindHeading   Kind = "heading"
	KindParagraph Kind = "paragraph"
	KindList      Kind = "list"
	KindCode      Kind = "code"
	KindTable     Kind = "table"
	KindCallout   Kind = "callout"
	KindImage     Kind = "image"
	KindQuote     Kind = "quote"
	KindDivider   Kind = "divider"
)

// Mark is inline formatting on a text run.
type Mark string

// Inline marks.
const (
	MarkBold   Mark = "bold"
	MarkItalic Mark = "italic"
	MarkCode   Mark = "code"
	MarkKbd    Mark = "kbd"
	MarkStrike Mark = "strike"
	MarkSub    Mark = "sub"
	MarkSup    Mark = "sup"
)

// LinkKind says where a link points relative to the imported page.
type LinkKind string

// Link kinds.
const (
	// LinkAnchor points at an anchor on the same page; its href is "#fragment".
	LinkAnchor LinkKind = "anchor"
	// LinkSameDomain points at another page on the page's host, ignoring a
	// leading "www.". The reader offers to import it.
	LinkSameDomain LinkKind = "same_domain"
	// LinkExternal is any other http(s) or mailto link.
	LinkExternal LinkKind = "external"
)

// Variant is the flavour of a callout.
type Variant string

// Callout variants.
const (
	VariantNote    Variant = "note"
	VariantTip     Variant = "tip"
	VariantInfo    Variant = "info"
	VariantWarning Variant = "warning"
	VariantDanger  Variant = "danger"
)

// WarningCode identifies a problem an extractor noticed but could live with.
type WarningCode string

// WarningLowConfidence means the extractor found no main content element and
// fell back to the whole body.
const WarningLowConfidence WarningCode = "low_confidence"

// Doc is one imported page.
type Doc struct {
	IRVersion int       `json:"ir_version"`
	Title     string    `json:"title"`
	Source    Source    `json:"source"`
	Extractor Extractor `json:"extractor"`
	Blocks    []Block   `json:"blocks"`
	Warnings  []Warning `json:"warnings,omitempty"`
}

// Source records where and when the page was fetched.
type Source struct {
	URL          string    `json:"url"`
	FinalURL     string    `json:"final_url"`
	CanonicalURL string    `json:"canonical_url,omitempty"`
	FetchedAt    time.Time `json:"fetched_at"`
	BodySHA256   string    `json:"body_sha256"`
	ETag         string    `json:"etag,omitempty"`
	LastModified string    `json:"last_modified,omitempty"`
}

// Extractor names the code that produced the blocks.
type Extractor struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// Warning is a problem the extractor noticed but could live with.
type Warning struct {
	Code    WarningCode `json:"code"`
	Message string      `json:"message"`
}

// Block is one block of content. Kind decides which fields may be set; see
// Validate. Children of lists, callouts and quotes are blocks themselves.
type Block struct {
	ID   string `json:"id"`
	Kind Kind   `json:"kind"`

	// heading
	Level  int    `json:"level,omitempty"`
	Anchor string `json:"anchor,omitempty"`
	// heading, paragraph
	Content []Inline `json:"content,omitempty"`
	// list
	Ordered bool      `json:"ordered,omitempty"`
	Items   [][]Block `json:"items,omitempty"`
	// code
	Lang string `json:"lang,omitempty"`
	Text string `json:"text,omitempty"`
	// table
	Head [][]Cell `json:"head,omitempty"`
	Rows [][]Cell `json:"rows,omitempty"`
	// callout
	Variant Variant `json:"variant,omitempty"`
	Title   string  `json:"title,omitempty"`
	// callout, quote
	Children []Block `json:"children,omitempty"`
	// image
	Src     string   `json:"src,omitempty"`
	Alt     string   `json:"alt,omitempty"`
	Caption []Inline `json:"caption,omitempty"`
}

// Cell is a table cell. Cells hold inline content only.
type Cell []Inline

// Inline is a run of text with the same marks and link.
type Inline struct {
	Text  string `json:"text"`
	Marks []Mark `json:"marks,omitempty"`
	Link  *Link  `json:"link,omitempty"`
}

// Link is the target of an inline run.
type Link struct {
	Href string   `json:"href"`
	Kind LinkKind `json:"kind"`
}
