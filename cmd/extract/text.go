package main

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/rokumate/ronote/internal/ir"
)

// writeText prints doc as Markdown-like text for reading in a terminal. Code
// blocks are printed exactly, between fences.
func writeText(w io.Writer, doc *ir.Doc) error {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\nsource: %s\nextractor: %s %s\n", doc.Title, doc.Source.FinalURL, doc.Extractor.Name, doc.Extractor.Version)
	for _, block := range doc.Blocks {
		b.WriteString("\n" + blockText(block) + "\n")
	}
	if len(doc.Warnings) > 0 {
		b.WriteString("\nwarnings:\n")
		for _, warn := range doc.Warnings {
			fmt.Fprintf(&b, "- %s: %s\n", warn.Code, warn.Message)
		}
	}
	_, err := io.WriteString(w, b.String())
	return err
}

// blockText renders one block as lines without a trailing newline.
func blockText(b ir.Block) string {
	switch b.Kind {
	case ir.KindHeading:
		return strings.Repeat("#", b.Level) + " " + inline(b.Content)
	case ir.KindParagraph:
		return inline(b.Content)
	case ir.KindCode:
		fence := "```" + b.Lang
		if b.Title != "" {
			fence += " title=" + strconv.Quote(b.Title)
		}
		return fence + "\n" + strings.TrimSuffix(b.Text, "\n") + "\n```"
	case ir.KindList:
		var items []string
		for i, item := range b.Items {
			marker := "- "
			if b.Ordered {
				marker = fmt.Sprintf("%d. ", i+1)
			}
			items = append(items, indent(blocksText(item.Children), marker, strings.Repeat(" ", len(marker))))
		}
		return strings.Join(items, "\n")
	case ir.KindTabs:
		var tabs []string
		for _, item := range b.Items {
			tabs = append(tabs, "=== "+item.Label+"\n"+indent(blocksText(item.Children), "    ", "    "))
		}
		return strings.Join(tabs, "\n")
	case ir.KindTable:
		var lines []string
		for i, row := range append(append([][]ir.Cell{}, b.Head...), b.Rows...) {
			cells := make([]string, len(row))
			for j, c := range row {
				cells[j] = inline(c)
			}
			lines = append(lines, "| "+strings.Join(cells, " | ")+" |")
			if i == len(b.Head)-1 {
				lines = append(lines, "|"+strings.Repeat(" --- |", len(row)))
			}
		}
		return strings.Join(lines, "\n")
	case ir.KindCallout:
		title := strings.ToUpper(string(b.Variant))
		if b.Title != "" {
			title += ": " + b.Title
		}
		body := "[" + title + "]"
		if len(b.Children) > 0 {
			body += "\n" + blocksText(b.Children)
		}
		return indent(body, "> ", "> ")
	case ir.KindImage:
		s := fmt.Sprintf("![%s](%s)", b.Alt, b.Src)
		if len(b.Caption) > 0 {
			s += "\ncaption: " + inline(b.Caption)
		}
		return s
	case ir.KindQuote:
		return indent(blocksText(b.Children), "> ", "> ")
	default:
		return "---"
	}
}

func blocksText(blocks []ir.Block) string {
	parts := make([]string, len(blocks))
	for i, b := range blocks {
		parts[i] = blockText(b)
	}
	return strings.Join(parts, "\n")
}

// indent puts first before the first line of s and rest before the others.
func indent(s, first, rest string) string {
	lines := strings.Split(s, "\n")
	for i := range lines {
		if i == 0 {
			lines[i] = first + lines[i]
		} else {
			lines[i] = rest + lines[i]
		}
	}
	return strings.Join(lines, "\n")
}

// inline renders runs with Markdown-like marks and [text](href) links.
func inline(runs []ir.Inline) string {
	var b strings.Builder
	for _, r := range runs {
		text := r.Text
		for _, m := range r.Marks {
			switch m {
			case ir.MarkBold:
				text = "**" + text + "**"
			case ir.MarkItalic:
				text = "_" + text + "_"
			case ir.MarkCode:
				text = "`" + text + "`"
			case ir.MarkKbd:
				text = "[" + text + "]"
			case ir.MarkStrike:
				text = "~~" + text + "~~"
			case ir.MarkSub, ir.MarkSup:
			}
		}
		if r.Link != nil {
			text = fmt.Sprintf("[%s](%s)", text, r.Link.Href)
		}
		b.WriteString(text)
	}
	return b.String()
}
