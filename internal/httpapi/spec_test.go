package httpapi

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"

	"github.com/rokumate/ronote/internal/doccontent"
	"github.com/rokumate/ronote/internal/ir"
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
	docSchema := spec.Components.Schemas["DocContent"].Value
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
		var extraction ir.Doc
		if err := json.Unmarshal(data, &extraction); err != nil {
			t.Fatal(err)
		}
		if err := docSchema.VisitJSON(asJSON(t, doccontent.FromIR(&extraction))); err != nil {
			t.Errorf("%s as a new document does not match the DocContent schema: %v", file, err)
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

// TestDocBlockIsIRBlockPlusOrigin keeps DocBlock's properties those of
// IRBlock plus the three that say where a block came from.
func TestDocBlockIsIRBlockPlusOrigin(t *testing.T) {
	t.Parallel()
	spec, err := loadContract()
	if err != nil {
		t.Fatal(err)
	}
	irBlock := spec.Components.Schemas["IRBlock"].Value
	docBlock := spec.Components.Schemas["DocBlock"].Value
	for name, prop := range irBlock.Properties {
		got, ok := docBlock.Properties[name]
		if !ok {
			t.Errorf("DocBlock lacks IRBlock's %s", name)
			continue
		}
		a, _ := json.Marshal(prop)
		b, _ := json.Marshal(got)
		if string(a) != string(b) {
			t.Errorf("DocBlock's %s = %s, IRBlock's is %s", name, b, a)
		}
	}
	for name := range docBlock.Properties {
		if _, ok := irBlock.Properties[name]; !ok && name != "origin" && name != "edited" && name != "source_block_id" {
			t.Errorf("DocBlock has %s, which IRBlock has not", name)
		}
	}
}

// asJSON is v as decoded JSON, the form schema validation takes.
func asJSON(t *testing.T, v any) any {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var out any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// TestUserKindsMatchTheSpec keeps the kinds the server lets user blocks use
// the spec's UserBlockKind, which the editor's block menu offers.
func TestUserKindsMatchTheSpec(t *testing.T) {
	t.Parallel()
	spec, err := loadContract()
	if err != nil {
		t.Fatal(err)
	}
	var want []string
	for _, v := range spec.Components.Schemas["UserBlockKind"].Value.Enum {
		want = append(want, v.(string))
	}
	var got []string
	for _, k := range doccontent.UserKinds {
		got = append(got, string(k))
	}
	if !slices.Equal(got, want) {
		t.Errorf("doccontent.UserKinds = %v, the spec's UserBlockKind = %v", got, want)
	}
}

// TestCodeLangsMatchTheSpec keeps the languages the server accepts in
// code_langs the spec's CodeLang, which the editor's language picker offers.
func TestCodeLangsMatchTheSpec(t *testing.T) {
	t.Parallel()
	spec, err := loadContract()
	if err != nil {
		t.Fatal(err)
	}
	var want []string
	for _, v := range spec.Components.Schemas["CodeLang"].Value.Enum {
		want = append(want, v.(string))
	}
	if !slices.Equal(doccontent.CodeLangs, want) {
		t.Errorf("doccontent.CodeLangs = %v, the spec's CodeLang = %v", doccontent.CodeLangs, want)
	}
}
