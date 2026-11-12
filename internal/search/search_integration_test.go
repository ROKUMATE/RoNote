//go:build integration

package search

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"

	"github.com/rokumate/ronote/internal/embed"
	"github.com/rokumate/ronote/internal/ir"
	"github.com/rokumate/ronote/internal/pgtest"
	"github.com/rokumate/ronote/internal/store"
)

var testDB *pgtest.DB

func TestMain(m *testing.M) {
	pgtest.Main(m, &testDB)
}

// counting is the fake embedder, counting the texts sent to it.
type counting struct {
	embed.Fake
	sent []string
}

func (c *counting) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	c.sent = append(c.sent, texts...)
	return c.Fake.Embed(ctx, texts)
}

// fixture reads a golden fixture's extraction.
func fixture(t *testing.T, name string) *ir.Doc {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "golden", name, "expected.json")) //nolint:gosec // a fixture name the test chose
	if err != nil {
		t.Fatal(err)
	}
	var doc ir.Doc
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	return &doc
}

// newUser makes a user.
func newUser(t *testing.T, st *store.Store) uuid.UUID {
	t.Helper()
	name := uuid.NewString()
	u, err := st.CreateUser(t.Context(), name+"@example.com", name)
	if err != nil {
		t.Fatal(err)
	}
	return u.ID
}

// importDoc stores doc as owner's document, as an import would.
func importDoc(t *testing.T, st *store.Store, owner uuid.UUID, doc *ir.Doc) uuid.UUID {
	t.Helper()
	id, err := st.AddDocument(t.Context(), owner, "https://docs.example.com/"+uuid.NewString(), doc)
	if err != nil {
		t.Fatal(err)
	}
	return id
}
