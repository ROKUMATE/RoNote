-- name: FindSourceForURL :one
-- The web source whose canonical URL, or else whose final URL, is url.
SELECT id, canonical_url::text, final_url::text, etag, last_modified, last_fetched_at
FROM sources
WHERE kind = 'web' AND (canonical_url = sqlc.arg(url)::text OR final_url = sqlc.arg(url)::text)
ORDER BY canonical_url = sqlc.arg(url)::text DESC
LIMIT 1;

-- name: UpsertSource :one
INSERT INTO sources (canonical_url, final_url, etag, last_modified, last_fetched_at)
VALUES (sqlc.arg(canonical_url)::text, sqlc.arg(final_url)::text, sqlc.arg(etag), sqlc.arg(last_modified), sqlc.arg(last_fetched_at))
ON CONFLICT (canonical_url) DO UPDATE
SET final_url = EXCLUDED.final_url,
    etag = EXCLUDED.etag,
    last_modified = EXCLUDED.last_modified,
    last_fetched_at = greatest(sources.last_fetched_at, EXCLUDED.last_fetched_at)
RETURNING id, canonical_url::text, final_url::text, etag, last_modified, last_fetched_at;

-- name: TouchSource :exec
UPDATE sources
SET last_fetched_at = greatest(last_fetched_at, sqlc.arg(fetched_at))
WHERE id = sqlc.arg(id);

-- name: NewestSnapshot :one
-- The newest snapshot of a web source, whose body Postgres holds.
SELECT id, source_id, body_sha256, body_gzip, content_type, fetched_at
FROM source_snapshots
WHERE source_id = $1 AND body_gzip IS NOT NULL
ORDER BY fetched_at DESC, id DESC
LIMIT 1;

-- name: SaveSnapshot :one
-- Inserts a snapshot, or returns the one the source already has with the
-- same body. The no-op update makes RETURNING see that row.
INSERT INTO source_snapshots (source_id, body_sha256, body_gzip, content_type, fetched_at)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (source_id, body_sha256) DO UPDATE SET body_sha256 = EXCLUDED.body_sha256
RETURNING id;

-- name: SaveExtraction :one
-- Inserts an extraction, or returns the stored one for the same snapshot and
-- extractor version, with its IR.
INSERT INTO extractions (snapshot_id, extractor_name, extractor_version, ir_version, ir)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (snapshot_id, extractor_name, extractor_version) DO UPDATE SET extractor_version = EXCLUDED.extractor_version
RETURNING id, ir;

-- name: UpsertPDFSource :one
-- The owner's source for the PDF with this SHA-256, created the first time.
-- A PDF's source is never shared (docs/adr/0013-pdf-import.md).
INSERT INTO sources (kind, owner_id, pdf_sha256, last_fetched_at)
VALUES ('pdf', sqlc.arg(owner_id)::uuid, sqlc.arg(pdf_sha256)::text, sqlc.arg(converted_at))
ON CONFLICT (owner_id, pdf_sha256) WHERE kind = 'pdf' DO UPDATE
SET last_fetched_at = greatest(sources.last_fetched_at, EXCLUDED.last_fetched_at)
RETURNING id;

-- name: SavePDFSnapshot :one
-- The snapshot of a PDF source: its blob, not its body. The no-op update
-- makes RETURNING see an existing row.
INSERT INTO source_snapshots (source_id, body_sha256, blob_key, content_type, fetched_at)
VALUES (sqlc.arg(source_id), sqlc.arg(body_sha256), sqlc.arg(blob_key)::text, 'application/pdf', sqlc.arg(fetched_at))
ON CONFLICT (source_id, body_sha256) DO UPDATE SET body_sha256 = EXCLUDED.body_sha256
RETURNING id;
