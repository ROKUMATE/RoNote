//go:build integration

package doccontent_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/rokumate/ronote/internal/doccontent"
	"github.com/rokumate/ronote/internal/pgtest"
)

const (
	beforeDocContent = 4 // documents hold their extraction's IR
	withDocContent   = 5 // 00005_doc_content.sql
)

// TestDocContentMigration converts stored IR documents to DocContent and
// back. Up makes every block an unedited source block; down gives the IR
// back, and keeps edits and added blocks.
func TestDocContentMigration(t *testing.T) {
	db := pgtest.New(t)
	ctx := t.Context()
	migrate := func(version int64) {
		t.Helper()
		if err := db.MigrateTo(ctx, version); err != nil {
			t.Fatalf("migrate to %d: %v", version, err)
		}
	}
	golden, err := os.ReadFile(filepath.Join("..", "..", "testdata", "golden", "starlight-tabs", "expected.json"))
	if err != nil {
		t.Fatal(err)
	}

	migrate(beforeDocContent)
	plain, edited := insertDocuments(ctx, t, db.Pool, golden)

	migrate(withDocContent)
	var extraction struct {
		Blocks []json.RawMessage `json:"blocks"`
	}
	if err := json.Unmarshal(golden, &extraction); err != nil {
		t.Fatal(err)
	}
	wantUp := map[string]any{"doc_version": float64(1), "blocks": sourceBlocks(t, extraction.Blocks)}
	for _, id := range []uuid.UUID{plain, edited} {
		if got := content(ctx, t, db.Pool, id); !reflect.DeepEqual(got, wantUp) {
			t.Errorf("document after up = %v, want every block an unedited source block", got)
		}
	}
	// The user edits the second document: an edited first block and a note.
	note := uuid.Must(uuid.NewV7()).String()
	_, err = db.Pool.Exec(ctx, `
		UPDATE documents SET content = jsonb_set(
			jsonb_set(content, '{blocks,0,edited}', 'true'),
			'{blocks}',
			(jsonb_set(content, '{blocks,0,edited}', 'true') -> 'blocks') || jsonb_build_array(jsonb_build_object(
				'id', $2::text, 'kind', 'paragraph', 'content', jsonb_build_array(jsonb_build_object('text', 'My note')), 'origin', 'user'))
		) WHERE id = $1`, edited, note)
	if err != nil {
		t.Fatal(err)
	}
	var doc doccontent.Doc
	if err := json.Unmarshal(raw(ctx, t, db.Pool, edited), &doc); err != nil {
		t.Fatal(err)
	}
	if err := doccontent.Validate(&doc); err != nil {
		t.Errorf("edited document is not valid DocContent: %v", err)
	}

	migrate(beforeDocContent)
	var wantIR map[string]any
	if err := json.Unmarshal(golden, &wantIR); err != nil {
		t.Fatal(err)
	}
	if got := content(ctx, t, db.Pool, plain); !reflect.DeepEqual(got, wantIR) {
		t.Errorf("document after down = %v, want its extraction's IR", got)
	}
	wantIR["blocks"] = append(wantIR["blocks"].([]any), map[string]any{
		"id": note, "kind": "paragraph", "content": []any{map[string]any{"text": "My note"}},
	})
	if got := content(ctx, t, db.Pool, edited); !reflect.DeepEqual(got, wantIR) {
		t.Errorf("edited document after down = %v, want the IR with the note kept", got)
	}

	migrate(withDocContent)
	if got := content(ctx, t, db.Pool, plain); !reflect.DeepEqual(got, wantUp) {
		t.Errorf("document after up again = %v", got)
	}
}

// insertDocuments stores one extraction of irJSON and two users'
// documents made from it, the way P08 stored them.
func insertDocuments(ctx context.Context, t *testing.T, pool *pgxpool.Pool, irJSON []byte) (uuid.UUID, uuid.UUID) {
	t.Helper()
	var extraction uuid.UUID
	err := pool.QueryRow(ctx, `
		WITH s AS (
			INSERT INTO sources (canonical_url, final_url, last_fetched_at)
			VALUES ('https://docs.example.com/tabs', 'https://docs.example.com/tabs', now()) RETURNING id
		), snap AS (
			INSERT INTO source_snapshots (source_id, body_sha256, body_gzip, content_type, fetched_at)
			SELECT id, $1, '\x00', 'text/html', now() FROM s RETURNING id
		)
		INSERT INTO extractions (snapshot_id, extractor_name, extractor_version, ir_version, ir)
		SELECT id, 'starlight', '1', 1, $2 FROM snap RETURNING id`,
		strings.Repeat("a", 64), irJSON).Scan(&extraction)
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]uuid.UUID, 2)
	for i, email := range []string{"ada@example.com", "grace@example.com"} {
		err := pool.QueryRow(ctx, `
			WITH u AS (INSERT INTO users (email, display_name) VALUES ($1, '') RETURNING id)
			INSERT INTO documents (owner_id, source_id, extraction_id, title, content)
			SELECT u.id, e.source_id, e.id, 'Tabs', e.ir
			FROM u, (SELECT x.id, s.source_id, x.ir FROM extractions x JOIN source_snapshots s ON s.id = x.snapshot_id WHERE x.id = $2) e
			RETURNING id`, email, extraction).Scan(&ids[i])
		if err != nil {
			t.Fatal(err)
		}
	}
	return ids[0], ids[1]
}

func raw(ctx context.Context, t *testing.T, pool *pgxpool.Pool, id uuid.UUID) []byte {
	t.Helper()
	var content []byte
	if err := pool.QueryRow(ctx, `SELECT content FROM documents WHERE id = $1`, id).Scan(&content); err != nil {
		t.Fatal(err)
	}
	return content
}

func content(ctx context.Context, t *testing.T, pool *pgxpool.Pool, id uuid.UUID) map[string]any {
	t.Helper()
	var v map[string]any
	if err := json.Unmarshal(raw(ctx, t, pool, id), &v); err != nil {
		t.Fatal(err)
	}
	return v
}

// sourceBlocks is blocks as unedited source blocks, decoded generically.
func sourceBlocks(t *testing.T, blocks []json.RawMessage) []any {
	t.Helper()
	out := make([]any, len(blocks))
	for i, b := range blocks {
		var block map[string]any
		if err := json.Unmarshal(b, &block); err != nil {
			t.Fatal(err)
		}
		block["origin"], block["edited"], block["source_block_id"] = "source", false, block["id"]
		out[i] = block
	}
	return out
}
