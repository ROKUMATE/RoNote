package ir

import (
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
)

// fieldsByKind lists the optional fields each kind may set. Validate rejects a
// block that sets any other field.
var fieldsByKind = map[Kind][]string{
	KindHeading:   {"level", "anchor", "content"},
	KindParagraph: {"content"},
	KindList:      {"ordered", "items"},
	KindCode:      {"lang", "text"},
	KindTable:     {"head", "rows"},
	KindCallout:   {"variant", "title", "children"},
	KindImage:     {"src", "alt", "caption"},
	KindQuote:     {"children"},
	KindDivider:   {},
}

var (
	marks    = []Mark{MarkBold, MarkItalic, MarkCode, MarkKbd, MarkStrike, MarkSub, MarkSup}
	variants = []Variant{VariantNote, VariantTip, VariantInfo, VariantWarning, VariantDanger}
	warnings = []WarningCode{WarningLowConfidence}
)

// Validate reports every way doc breaks the IR's rules, joined into one
// error, or nil. Each problem names its location, such as
// "blocks[3].items[0][1]".
func Validate(doc *Doc) error {
	v := validator{ids: map[string]string{}}
	if doc.IRVersion != Version {
		v.fail("ir_version", "is %d, want %d", doc.IRVersion, Version)
	}
	v.url("source.url", doc.Source.URL, false)
	v.url("source.final_url", doc.Source.FinalURL, true)
	v.url("source.canonical_url", doc.Source.CanonicalURL, false)
	if doc.Extractor.Name == "" || doc.Extractor.Version == "" {
		v.fail("extractor", "needs a name and a version")
	}
	for i, w := range doc.Warnings {
		if !slices.Contains(warnings, w.Code) {
			v.fail(fmt.Sprintf("warnings[%d]", i), "unknown code %q", w.Code)
		}
	}
	v.blocks("blocks", doc.Blocks)
	return errors.Join(v.errs...)
}

type validator struct {
	errs []error
	ids  map[string]string // block ID -> path of the block that has it
}

func (v *validator) fail(path, format string, args ...any) {
	v.errs = append(v.errs, fmt.Errorf("%s: %s", path, fmt.Sprintf(format, args...)))
}

// url checks that s is an absolute http(s) URL. An empty s is fine unless
// required.
func (v *validator) url(path, s string, required bool) {
	if s == "" {
		if required {
			v.fail(path, "is empty")
		}
		return
	}
	if !isAbsoluteHTTP(s) {
		v.fail(path, "%q is not an absolute http(s) URL", s)
	}
}

func (v *validator) blocks(path string, blocks []Block) {
	for i := range blocks {
		v.block(fmt.Sprintf("%s[%d]", path, i), &blocks[i])
	}
}

func (v *validator) block(path string, b *Block) {
	switch {
	case b.ID == "":
		v.fail(path, "has no id")
	case v.ids[b.ID] != "":
		v.fail(path, "repeats id %q from %s", b.ID, v.ids[b.ID])
	default:
		v.ids[b.ID] = path
	}

	allowed, known := fieldsByKind[b.Kind]
	if !known {
		v.fail(path, "unknown kind %q", b.Kind)
		return
	}
	for _, field := range setFields(b) {
		if !slices.Contains(allowed, field) {
			v.fail(path, "%s block sets %s", b.Kind, field)
		}
	}

	switch b.Kind {
	case KindHeading:
		if b.Level < 1 || b.Level > 6 {
			v.fail(path, "heading level %d is not 1-6", b.Level)
		}
		v.inlines(path+".content", b.Content, true)
	case KindParagraph:
		v.inlines(path+".content", b.Content, true)
	case KindList:
		if len(b.Items) == 0 {
			v.fail(path, "list has no items")
		}
		for i, item := range b.Items {
			itemPath := fmt.Sprintf("%s.items[%d]", path, i)
			if len(item) == 0 {
				v.fail(itemPath, "list item is empty")
			}
			v.blocks(itemPath, item)
		}
	case KindCode:
		if b.Text == "" {
			v.fail(path, "code text is empty")
		}
	case KindTable:
		if len(b.Head) == 0 && len(b.Rows) == 0 {
			v.fail(path, "table has no rows")
		}
		v.rows(path+".head", b.Head)
		v.rows(path+".rows", b.Rows)
	case KindCallout:
		if !slices.Contains(variants, b.Variant) {
			v.fail(path, "unknown callout variant %q", b.Variant)
		}
		if b.Title == "" && len(b.Children) == 0 {
			v.fail(path, "callout has neither title nor children")
		}
		v.blocks(path+".children", b.Children)
	case KindImage:
		v.url(path+".src", b.Src, true)
		v.inlines(path+".caption", b.Caption, false)
	case KindQuote:
		if len(b.Children) == 0 {
			v.fail(path, "quote has no children")
		}
		v.blocks(path+".children", b.Children)
	case KindDivider:
	}
}

func (v *validator) rows(path string, rows [][]Cell) {
	for r, row := range rows {
		for c, cell := range row {
			v.inlines(fmt.Sprintf("%s[%d][%d]", path, r, c), cell, false)
		}
	}
}

func (v *validator) inlines(path string, runs []Inline, required bool) {
	if required && len(runs) == 0 {
		v.fail(path, "is empty")
	}
	for i, run := range runs {
		runPath := fmt.Sprintf("%s[%d]", path, i)
		if run.Text == "" {
			v.fail(runPath, "text is empty")
		}
		for j, m := range run.Marks {
			if !slices.Contains(marks, m) {
				v.fail(runPath, "unknown mark %q", m)
			} else if slices.Contains(run.Marks[:j], m) {
				v.fail(runPath, "repeats mark %q", m)
			}
		}
		if run.Link != nil {
			v.link(runPath+".link", run.Link)
		}
	}
}

func (v *validator) link(path string, l *Link) {
	switch l.Kind {
	case LinkAnchor:
		if len(l.Href) < 2 || l.Href[0] != '#' {
			v.fail(path, "anchor href %q is not #fragment", l.Href)
		}
	case LinkSameDomain:
		if !isAbsoluteHTTP(l.Href) {
			v.fail(path, "same_domain href %q is not an absolute http(s) URL", l.Href)
		}
	case LinkExternal:
		if !isAbsoluteHTTP(l.Href) && !strings.HasPrefix(l.Href, "mailto:") {
			v.fail(path, "external href %q is not an absolute http(s) or mailto URL", l.Href)
		}
	default:
		v.fail(path, "unknown link kind %q", l.Kind)
	}
}

// setFields names the kind-specific fields of b that are not zero.
func setFields(b *Block) []string {
	set := map[string]bool{
		"level":    b.Level != 0,
		"anchor":   b.Anchor != "",
		"content":  len(b.Content) > 0,
		"ordered":  b.Ordered,
		"items":    len(b.Items) > 0,
		"lang":     b.Lang != "",
		"text":     b.Text != "",
		"head":     len(b.Head) > 0,
		"rows":     len(b.Rows) > 0,
		"variant":  b.Variant != "",
		"title":    b.Title != "",
		"children": len(b.Children) > 0,
		"src":      b.Src != "",
		"alt":      b.Alt != "",
		"caption":  len(b.Caption) > 0,
	}
	var fields []string
	for field, isSet := range set {
		if isSet {
			fields = append(fields, field)
		}
	}
	slices.Sort(fields)
	return fields
}

func isAbsoluteHTTP(s string) bool {
	u, err := url.Parse(s)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}
