package search

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"
	"unicode/utf8"
)

// TestTheLongestQueryFitsTheQueryEmbedder: with truncate off, a query over
// the query embedder's context fails instead of being cut, so the longest
// query the API takes must fit, in any script. The model's tokenizer is
// byte-level, so a token is at most a UTF-8 byte, and a character at most
// utf8.UTFMax of them.
func TestTheLongestQueryFitsTheQueryEmbedder(t *testing.T) {
	t.Parallel()
	if need := MaxQuery*utf8.UTFMax + queryPrefixTokens; need > QueryContext {
		t.Errorf("MaxQuery %d needs up to %d tokens with the prefix, over QueryContext %d", MaxQuery, need, QueryContext)
	}
}

// TestTheQueryLimitIsOneNumber: the spec and the search palette say
// MaxQuery.
func TestTheQueryLimitIsOneNumber(t *testing.T) {
	t.Parallel()
	want := strconv.Itoa(MaxQuery)
	for path, pattern := range map[string]string{
		"api/openapi.yaml":                      `- name: q\s+in: query\s+required: true\s+schema:\s+type: string\s+minLength: 1\s+maxLength: (\d+)`,
		"apps/web/src/search/SearchPalette.tsx": `maxLength=\{(\d+)\}`,
	} {
		data, err := os.ReadFile(filepath.Join("..", "..", path)) //nolint:gosec // the repo's own files
		if err != nil {
			t.Fatal(err)
		}
		m := regexp.MustCompile(pattern).FindSubmatch(data)
		if m == nil || string(m[1]) != want {
			t.Errorf("%s: %s gives %q, want MaxQuery, %s", path, pattern, m, want)
		}
	}
}
