//go:build integration

package search

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/rokumate/ronote/internal/embed"
	"github.com/rokumate/ronote/internal/ir"
	"github.com/rokumate/ronote/internal/store"
)

// captured records the last query a pool sent, with its arguments.
type captured struct {
	mu   sync.Mutex
	sql  string
	args []any
}

func (c *captured) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	c.mu.Lock()
	defer c.mu.Unlock()
	if strings.Contains(data.SQL, "SearchChunks") {
		c.sql, c.args = data.SQL, data.Args
	}
	return ctx
}

func (*captured) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

// planNode is a node of EXPLAIN (FORMAT JSON).
type planNode struct {
	NodeType     string     `json:"Node Type"`
	RelationName string     `json:"Relation Name"`
	IndexName    string     `json:"Index Name"`
	Plans        []planNode `json:"Plans"`
}

func (n planNode) walk(fn func(planNode)) {
	fn(n)
	for _, p := range n.Plans {
		p.walk(fn)
	}
}

// TestVectorSearchReadsOnlyTheOwnersRows explains the meaning half of the
// search over a table where other users hold almost every row: it must
// reach the owner's rows through the owner index, never by reading the
// whole table.
func TestVectorSearchReadsOnlyTheOwnersRows(t *testing.T) {
	testDB.Reset(t)
	ctx := t.Context()
	cfg, err := pgxpool.ParseConfig(testDB.URL)
	if err != nil {
		t.Fatal(err)
	}
	trace := &captured{}
	cfg.ConnConfig.Tracer = trace
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	st := store.New(pool)
	ada, others := newUser(t, st), newUser(t, st)
	indexed(t, st, ada, fixture(t, "real-python-controlflow"))
	crowd := indexed(t, st, others, &ir.Doc{IRVersion: ir.Version, Title: "Crowd", Blocks: []ir.Block{
		{ID: "000000000001", Kind: ir.KindParagraph, Content: []ir.Inline{{Text: "seed"}}},
	}})
	// Twenty thousand of another user's chunks, sharing one vector.
	_, err = pool.Exec(ctx, `
		INSERT INTO chunks (owner_id, document_id, block_ids, heading_path, kind, text, position, tsv, embedding, model)
		SELECT $1, $2, ARRAY['000000000001'], ARRAY[]::text[], 'prose', 'chunk ' || g, g,
			to_tsvector('simple', 'chunk ' || g), c.embedding, c.model
		FROM generate_series(1, 20000) g, (SELECT embedding, model FROM chunks WHERE document_id = $2 LIMIT 1) c`,
		others, crowd)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "ANALYZE chunks"); err != nil {
		t.Fatal(err)
	}

	if _, err := NewSearcher(st, embed.Fake{}, discard).Search(ctx, Request{Owner: ada, Query: "loops", Limit: 10, Mode: MeaningOnly}); err != nil {
		t.Fatal(err)
	}
	trace.mu.Lock()
	query, args := trace.sql, trace.args
	trace.mu.Unlock()
	if query == "" {
		t.Fatal("captured no search query")
	}
	var raw []byte
	if err := pool.QueryRow(ctx, "EXPLAIN (FORMAT JSON) "+query, args...).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var plans []struct {
		Plan planNode `json:"Plan"`
	}
	if err := json.Unmarshal(raw, &plans); err != nil {
		t.Fatal(err)
	}
	ownerIndex := false
	plans[0].Plan.walk(func(n planNode) {
		if n.RelationName == "chunks" && n.NodeType == "Seq Scan" {
			t.Errorf("the search reads every user's chunks (Seq Scan on chunks):\n%s", raw)
		}
		ownerIndex = ownerIndex || n.IndexName == "chunks_owner_idx"
	})
	if !ownerIndex {
		t.Errorf("the search doesn't read through chunks_owner_idx:\n%s", raw)
	}
}
