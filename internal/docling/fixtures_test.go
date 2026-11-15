package docling_test

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode"

	"github.com/rokumate/ronote/internal/docling"
	"github.com/rokumate/ronote/internal/fakedocling"
	"github.com/rokumate/ronote/internal/ir"
)

var update = flag.Bool("update", false, "rewrite the PDF fixtures' IR goldens and testdata/pdf/report.md")

var fixtures = filepath.Join("..", "..", "testdata", "pdf")

// converted is a fixture docling converted: its recording and its IR.
type converted struct {
	name string
	rec  fakedocling.Recording
	doc  *ir.Doc
}

// convertedFixtures maps every recording docling converted, the way the
// worker would, with a fixed conversion time.
func convertedFixtures(t *testing.T) []converted {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(fixtures, "*"+fakedocling.Suffix))
	if err != nil {
		t.Fatal(err)
	}
	var out []converted
	for _, p := range paths {
		var rec fakedocling.Recording
		data, err := os.ReadFile(p) //nolint:gosec // a fixture path the test chose
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(data, &rec); err != nil {
			t.Fatal(err)
		}
		if rec.Result == nil || rec.Lines == nil {
			continue
		}
		var result struct {
			Document struct {
				JSON *docling.Document `json:"json_content"`
			} `json:"document"`
		}
		var lines struct {
			Blocks [][]docling.Line `json:"blocks"`
		}
		if err := json.Unmarshal(rec.Result.Body, &result); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(rec.Lines.Body, &lines); err != nil {
			t.Fatal(err)
		}
		name := strings.TrimSuffix(filepath.Base(p), fakedocling.Suffix)
		boxes := docling.LayerBoxes(result.Document.JSON)
		layer, err := docling.Layer(boxes, lines.Blocks)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		doc, err := docling.Map(result.Document.JSON, docling.PDF{
			Filename: name + ".pdf", SHA256: rec.SHA256, ConvertedAt: time.Date(2026, 11, 15, 0, 0, 0, 0, time.UTC), Docling: docling.ServeVersion,
		}, layer)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		out = append(out, converted{name: name, rec: rec, doc: doc})
	}
	if len(out) < 7 {
		t.Fatalf("%d converted recordings, want the printed pages and the real PDFs", len(out))
	}
	return out
}

// TestFixturesMapToGoldens maps each recorded fixture and compares it with
// its golden; -update rewrites them. Review that diff like code.
func TestFixturesMapToGoldens(t *testing.T) {
	for _, c := range convertedFixtures(t) {
		t.Run(c.name, func(t *testing.T) {
			got, err := json.MarshalIndent(c.doc, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			got = append(got, '\n')
			path := filepath.Join(fixtures, c.name+".ir.json")
			if *update {
				if err := os.WriteFile(path, got, 0o600); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(path) //nolint:gosec // a fixture path the test chose
			if err != nil {
				t.Fatalf("%v; run go test ./internal/docling -update", err)
			}
			if string(got) != string(want) {
				t.Errorf("%s maps to IR that differs from its golden; run go test ./internal/docling -update and review the diff", c.name)
			}
		})
	}
}

// Results of comparing one of a page's code blocks with its PDF.
const (
	exact      = "exact"
	whitespace = "whitespace only"
	other      = "other difference"
	missing    = "missing"
)

// compare finds src among pdf's code blocks, or two or three in a row, as a
// page break splits one: exact, or differing in whitespace only; else
// "other" when a block holds its first line, or missing. merged says src was
// found whole, line for line, inside a block docling merged with its
// neighbours.
func compare(src string, pdf []string) (result string, merged bool) {
	var candidates []string
	for i := range pdf {
		for n := 1; n <= 3 && i+n <= len(pdf); n++ {
			candidates = append(candidates, strings.Join(pdf[i:i+n], ""))
		}
	}
	if slices.Contains(candidates, src) {
		return exact, false
	}
	bare := func(s string) string { return strings.Join(strings.FieldsFunc(s, unicode.IsSpace), "") }
	for _, c := range candidates {
		if bare(c) == bare(src) {
			return whitespace, false
		}
	}
	lines := "\n" + strings.TrimSuffix(src, "\n") + "\n"
	for _, c := range pdf {
		if strings.Contains("\n"+c, lines) {
			return exact, true
		}
	}
	first := strings.TrimSpace(strings.SplitN(strings.TrimSpace(src), "\n", 2)[0])
	for _, c := range pdf {
		if first != "" && strings.Contains(c, first) {
			return other, false
		}
	}
	return missing, false
}

// codeOf returns the code blocks of blocks, nested ones included, in order.
func codeOf(blocks []ir.Block) []string {
	var out []string
	for _, b := range blocks {
		if b.Kind == ir.KindCode {
			out = append(out, b.Text)
		}
		out = append(out, codeOf(b.Children)...)
		for _, item := range b.Items {
			out = append(out, codeOf(item.Children)...)
		}
	}
	return out
}

// TestPDFCodeIsVerbatim compares every code block of each printed golden
// page, as the extractor read it from the HTML, with the code of its PDF,
// read from the text layer (docs/adr/0013-pdf-import.md). Gate: every block
// found, and none differing in more than whitespace. -update writes the
// PDF verbatim report, testdata/pdf/report.md.
func TestPDFCodeIsVerbatim(t *testing.T) {
	var printed []string
	data, err := os.ReadFile(filepath.Join(fixtures, "printed.json")) //nolint:gosec // a fixture path the test chose
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &printed); err != nil {
		t.Fatal(err)
	}
	all := convertedFixtures(t)
	byName := map[string]converted{}
	for _, c := range all {
		byName[c.name] = c
	}

	var report strings.Builder
	report.WriteString("# PDF verbatim report\n\n")
	report.WriteString("Written by `go test ./internal/docling -update` from the recordings in this directory (docs/adr/0013-pdf-import.md). Each printed golden page's code blocks, as the extractor reads them from its HTML, are looked for in the code mapped from its PDF: docling's blocks, read from the text layer by pdftext. Gate: every block found, none with an other difference.\n\n")
	report.WriteString("| PDF | pages | conversion | per page | code blocks | exact | whitespace only | other difference | missing |\n|---|---|---|---|---|---|---|---|---|\n")
	var details strings.Builder
	for _, name := range printed {
		c, ok := byName[name]
		if !ok {
			t.Fatalf("%s is printed but has no converted recording", name)
		}
		var golden ir.Doc
		data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "golden", name, "expected.json")) //nolint:gosec // a fixture path the test chose
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(data, &golden); err != nil {
			t.Fatal(err)
		}
		counts := map[string]int{}
		fmt.Fprintf(&details, "\n## %s\n\n| block | first line | result | note |\n|---|---|---|---|\n", name)
		src, pdf := codeOf(golden.Blocks), codeOf(c.doc.Blocks)
		for i, block := range src {
			result, merged := compare(block, pdf)
			counts[result]++
			first := strings.TrimSpace(strings.SplitN(strings.TrimSpace(block), "\n", 2)[0])
			note := ""
			if merged {
				note = "inside a block docling merged with its neighbours"
			}
			fmt.Fprintf(&details, "| %d | `%s` | %s | %s |\n", i+1, strings.ReplaceAll(first, "|", `\|`), result, note)
			if result == missing || result == other {
				t.Errorf("%s: code block %d (%q) is %s", name, i+1, first, result)
			}
		}
		fmt.Fprintf(&report, "| %s | %d | %.1f s | %.2f s | %d | %d | %d | %d | %d |\n", name, c.rec.Pages, c.rec.Seconds, c.rec.Seconds/float64(c.rec.Pages),
			len(src), counts[exact], counts[whitespace], counts[other], counts[missing])
	}
	report.WriteString("\nThe real PDFs have no HTML to compare with; their code blocks and conversion times:\n\n| PDF | pages | conversion | per page | code blocks |\n|---|---|---|---|---|\n")
	for _, c := range all {
		if slices.Contains(printed, c.name) {
			continue
		}
		fmt.Fprintf(&report, "| %s | %d | %.1f s | %.2f s | %d |\n", c.name, c.rec.Pages, c.rec.Seconds, c.rec.Seconds/float64(c.rec.Pages), len(codeOf(c.doc.Blocks)))
	}
	report.WriteString(details.String())

	path := filepath.Join(fixtures, "report.md")
	if *update {
		if err := os.WriteFile(path, []byte(report.String()), 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path) //nolint:gosec // a fixture path the test chose
	if err != nil || string(want) != report.String() {
		t.Errorf("testdata/pdf/report.md is not this report; run go test ./internal/docling -update")
	}
}
