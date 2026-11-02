package extract_test

import (
	"bytes"
	"testing"

	"github.com/rokumate/ronote/internal/extract"
	"github.com/rokumate/ronote/internal/ir"
)

// FuzzExtract feeds arbitrary bytes to the extractor: it must never panic or
// fail, and its output must always pass Validate. The seed corpus is every
// golden input plus testdata/fuzz/FuzzExtract.
func FuzzExtract(f *testing.F) {
	for _, name := range fixtures(f) {
		f.Add(readFixture(f, name, "input.html"))
	}
	const page = "https://docs.example.com/guide/page"
	f.Fuzz(func(t *testing.T, input []byte) {
		doc, err := extract.Extract(bytes.NewReader(input), ir.Source{URL: page, FinalURL: page})
		if err != nil {
			t.Fatalf("Extract() error = %v", err)
		}
		if err := ir.Validate(doc); err != nil {
			t.Fatalf("Validate() = %v", err)
		}
	})
}
