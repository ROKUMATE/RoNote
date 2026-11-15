//go:build integration

package ir_test

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	"github.com/rokumate/ronote/internal/ir"
	"github.com/rokumate/ronote/internal/pgtest"
)

const (
	irV1 = 13 // 00013_chunk_block_texts.sql, the last schema of IR version 1
	irV2 = 14 // 00014_ir_v2.sql
)

// TestIRVersionMigration upgrades stored version 1 extractions to version 2,
// which then validate, and back; down refuses once an extraction has a page.
func TestIRVersionMigration(t *testing.T) {
	db := pgtest.New(t)
	ctx := t.Context()
	migrate := func(version int64) error {
		t.Helper()
		return db.MigrateTo(ctx, version)
	}
	versions := func(id uuid.UUID) (column, inIR int, doc ir.Doc) {
		t.Helper()
		var raw []byte
		if err := db.Pool.QueryRow(ctx, `SELECT ir_version, ir FROM extractions WHERE id = $1`, id).Scan(&column, &raw); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(raw, &doc); err != nil {
			t.Fatal(err)
		}
		return column, doc.IRVersion, doc
	}

	if err := migrate(irV1); err != nil {
		t.Fatal(err)
	}
	v1 := ir.Doc{
		IRVersion: 1, Title: "Page",
		Source:    ir.Source{FinalURL: "https://example.com/page", BodySHA256: "ab"},
		Extractor: ir.Extractor{Name: "generic", Version: "2"},
		Blocks:    []ir.Block{{Kind: ir.KindParagraph, Content: []ir.Inline{{Text: "Hello"}}}},
	}
	ir.AssignIDs(v1.Blocks)
	id := insertExtraction(t, db, &v1)

	if err := migrate(irV2); err != nil {
		t.Fatal(err)
	}
	column, inIR, doc := versions(id)
	if column != 2 || inIR != 2 {
		t.Fatalf("after up: ir_version column %d, in the IR %d; want 2 and 2", column, inIR)
	}
	if err := ir.Validate(&doc); err != nil {
		t.Errorf("upgraded extraction is not valid IR: %v", err)
	}

	if err := migrate(irV1); err != nil {
		t.Fatal(err)
	}
	if column, inIR, _ := versions(id); column != 1 || inIR != 1 {
		t.Fatalf("after down: ir_version column %d, in the IR %d; want 1 and 1", column, inIR)
	}

	if err := migrate(irV2); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool.Exec(ctx, `UPDATE extractions SET ir = jsonb_set(ir, '{blocks,0,page}', '1') WHERE id = $1`, id); err != nil {
		t.Fatal(err)
	}
	if err := migrate(irV1); err == nil {
		t.Error("down succeeded with a block that has a page")
	}
}

func insertExtraction(t *testing.T, db *pgtest.DB, doc *ir.Doc) uuid.UUID {
	t.Helper()
	ctx := t.Context()
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	var id uuid.UUID
	err = db.Pool.QueryRow(ctx, `
		WITH s AS (
			INSERT INTO sources (canonical_url, final_url, last_fetched_at) VALUES ($1, $1, now()) RETURNING id
		), snap AS (
			INSERT INTO source_snapshots (source_id, body_sha256, body_gzip, content_type, fetched_at)
			SELECT id, repeat('a', 64), '\x00', 'text/html', now() FROM s RETURNING id
		)
		INSERT INTO extractions (snapshot_id, extractor_name, extractor_version, ir_version, ir)
		SELECT id, 'generic', '2', $2, $3 FROM snap RETURNING id`,
		doc.Source.FinalURL, doc.IRVersion, raw).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	return id
}
