package store

import (
	"io/fs"
	"os"
	"regexp"
	"strings"
	"testing"
)

var (
	queryName   = regexp.MustCompile(`(?m)^-- name: (\w+)`)
	ownedTables = regexp.MustCompile(`(?i)\b(documents|imports|annotations|chunks)\b`)
	sqlComment  = regexp.MustCompile(`--[^\n]*`)
)

// TestOwnedQueriesAreScoped fails when a query touches documents or imports
// without naming owner_id, so no unscoped query can exist.
func TestOwnedQueriesAreScoped(t *testing.T) {
	t.Parallel()
	queries := os.DirFS("queries")
	files, err := fs.Glob(queries, "*.sql")
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, file := range files {
		data, err := fs.ReadFile(queries, file)
		if err != nil {
			t.Fatal(err)
		}
		src := string(data)
		starts := queryName.FindAllStringSubmatchIndex(src, -1)
		for i, m := range starts {
			end := len(src)
			if i+1 < len(starts) {
				end = starts[i+1][0]
			}
			name := src[m[2]:m[3]]
			sql := sqlComment.ReplaceAllString(src[m[1]:end], "")
			if !ownedTables.MatchString(sql) {
				continue
			}
			checked++
			if !strings.Contains(sql, "owner_id") {
				t.Errorf("%s: query %s touches documents or imports without owner_id", file, name)
			}
		}
	}
	if checked == 0 {
		t.Fatal("found no queries on documents or imports")
	}
}
