package httpapi

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
)

// TestIRSchemaMatchesGoldens validates every golden extraction against the
// IR schema in api/openapi.yaml, so the spec the web app's types come from
// and the Go IR cannot drift apart.
func TestIRSchemaMatchesGoldens(t *testing.T) {
	t.Parallel()
	loader := openapi3.NewLoader()
	spec, err := loader.LoadFromFile(filepath.Join("..", "..", "api", "openapi.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := spec.Validate(loader.Context); err != nil {
		t.Fatalf("api/openapi.yaml is invalid: %v", err)
	}
	schema := spec.Components.Schemas["IRDocument"].Value

	goldens := os.DirFS(filepath.Join("..", "..", "testdata", "golden"))
	files, err := fs.Glob(goldens, "*/expected.json")
	if err != nil || len(files) == 0 {
		t.Fatalf("no golden files: %v", err)
	}
	var first map[string]any
	for _, file := range files {
		data, err := fs.ReadFile(goldens, file)
		if err != nil {
			t.Fatal(err)
		}
		var doc map[string]any
		if err := json.Unmarshal(data, &doc); err != nil {
			t.Fatal(err)
		}
		if err := schema.VisitJSON(doc); err != nil {
			t.Errorf("%s does not match the IRDocument schema: %v", file, err)
		}
		if first == nil {
			first = doc
		}
	}

	first["unknown_field"] = true
	if err := schema.VisitJSON(first); err == nil {
		t.Error("a document with an unknown field matches the schema; want additionalProperties: false to refuse it")
	}
}
