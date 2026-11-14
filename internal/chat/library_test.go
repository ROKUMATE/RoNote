package chat

import (
	"maps"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// TestSearchToolSchemaMatchesItsInput keeps the schema the model is shown
// and the input the tool decodes the same.
func TestSearchToolSchemaMatchesItsInput(t *testing.T) {
	t.Parallel()
	var fields []string
	for _, f := range reflect.VisibleFields(reflect.TypeFor[toolInput]()) {
		fields = append(fields, strings.Split(f.Tag.Get("json"), ",")[0])
	}
	slices.Sort(fields)
	if props := slices.Sorted(maps.Keys(searchTool.Properties)); !slices.Equal(props, fields) {
		t.Errorf("the schema has %v, the input %v", props, fields)
	}
	for _, r := range searchTool.Required {
		if !slices.Contains(fields, r) {
			t.Errorf("the schema requires %q, which the input lacks", r)
		}
	}
	if searchTool.Name != toolName {
		t.Errorf("the tool is named %q, calls are answered as %q", searchTool.Name, toolName)
	}
}
