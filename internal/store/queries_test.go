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
	ownedTables = regexp.MustCompile(`(?i)\b(documents|imports|annotations|chunks|conversations|messages)\b`)
	sqlComment  = regexp.MustCompile(`--[^\n]*`)
)

// unscoped are the queries on owned tables that may read across owners,
// each with why. Each returns only a total, never a row of anyone's.
var unscoped = map[string]string{
	"ChatSpentToday": "chat's daily budget is for all users together (docs/adr/0014-production-topology.md): it sums cost only",
	"ChunkToSearch":  "the restore drill searches one owner's restored library for a word of theirs (docs/adr/0015-operations.md); only the number of hits leaves it",
}

// TestOwnedQueriesAreScoped fails when a query touches documents or imports
// without naming owner_id, so no unscoped query can exist but those
// unscoped names.
func TestOwnedQueriesAreScoped(t *testing.T) {
	t.Parallel()
	for name, why := range unscoped {
		if why == "" {
			t.Errorf("unscoped query %s has no reason", name)
		}
	}
	seen := map[string]bool{}
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
			if _, ok := unscoped[name]; ok {
				seen[name] = true
				continue
			}
			if !strings.Contains(sql, "owner_id") {
				t.Errorf("%s: query %s touches documents or imports without owner_id", file, name)
			}
		}
	}
	if checked == 0 {
		t.Fatal("found no queries on documents or imports")
	}
	for name := range unscoped {
		if !seen[name] {
			t.Errorf("unscoped query %s no longer exists; remove it", name)
		}
	}
}
