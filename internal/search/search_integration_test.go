//go:build integration

package search

import (
	"context"
	"testing"

	"github.com/rokumate/ronote/internal/embed"
	"github.com/rokumate/ronote/internal/pgtest"
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
