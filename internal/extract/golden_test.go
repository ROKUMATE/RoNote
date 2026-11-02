package extract_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rokumate/ronote/internal/extract"
	"github.com/rokumate/ronote/internal/ir"
)

var update = flag.Bool("update", false, "rewrite testdata/golden/*/expected.json from the current extractor")

const goldenDir = "../../testdata/golden"

// fixtures lists the golden fixture directories.
func fixtures(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(goldenDir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	if len(names) == 0 {
		t.Fatal("no golden fixtures")
	}
	return names
}

// goldenFS reads fixtures; paths stay inside goldenDir.
var goldenFS = os.DirFS(goldenDir)

func readFixture(t *testing.T, name, file string) []byte {
	t.Helper()
	data, err := fs.ReadFile(goldenFS, path.Join(name, file))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// extractFixture runs the extractor on a fixture's input.html with the
// source described by its meta.json.
func extractFixture(t *testing.T, name string) *ir.Doc {
	t.Helper()
	input := readFixture(t, name, "input.html")
	var meta struct {
		URL          string    `json:"url"`
		FinalURL     string    `json:"final_url"`
		FetchedAt    time.Time `json:"fetched_at"`
		ETag         string    `json:"etag"`
		LastModified string    `json:"last_modified"`
	}
	if err := json.Unmarshal(readFixture(t, name, "meta.json"), &meta); err != nil {
		t.Fatalf("meta.json: %v", err)
	}
	sum := sha256.Sum256(input)
	doc, err := extract.Extract(bytes.NewReader(input), ir.Source{
		URL:          meta.URL,
		FinalURL:     meta.FinalURL,
		FetchedAt:    meta.FetchedAt,
		BodySHA256:   hex.EncodeToString(sum[:]),
		ETag:         meta.ETag,
		LastModified: meta.LastModified,
	})
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	if err := ir.Validate(doc); err != nil {
		t.Fatalf("Validate() = %v", err)
	}
	return doc
}

// TestGolden compares the extractor's output for every fixture with its
// expected.json. Run go test ./internal/extract -update to accept changes.
func TestGolden(t *testing.T) {
	for _, name := range fixtures(t) {
		t.Run(name, func(t *testing.T) {
			var buf bytes.Buffer
			enc := json.NewEncoder(&buf)
			enc.SetEscapeHTML(false)
			enc.SetIndent("", "  ")
			if err := enc.Encode(extractFixture(t, name)); err != nil {
				t.Fatal(err)
			}
			got := buf.Bytes()

			path := filepath.Join(goldenDir, name, "expected.json")
			if *update {
				if err := os.WriteFile(path, got, 0o600); err != nil {
					t.Fatal(err)
				}
				return
			}
			want := readFixture(t, name, "expected.json")
			if !bytes.Equal(got, want) {
				t.Errorf("output differs from expected.json%s\nRun go test ./internal/extract -update if the change is intended.", firstDiff(got, want))
			}
		})
	}
}

// firstDiff describes the first line where got and want differ.
func firstDiff(got, want []byte) string {
	g, w := strings.Split(string(got), "\n"), strings.Split(string(want), "\n")
	for i := 0; i < len(g) || i < len(w); i++ {
		var gl, wl string
		if i < len(g) {
			gl = g[i]
		}
		if i < len(w) {
			wl = w[i]
		}
		if gl != wl {
			return "\nline " + strconv.Itoa(i+1) + ":\n got: " + gl + "\nwant: " + wl
		}
	}
	return ""
}
