-- name: FindSourceForURL :one
-- The source whose canonical URL, or else whose final URL, is url.
SELECT id, canonical_url, final_url, etag, last_modified, last_fetched_at
FROM sources
WHERE canonical_url = sqlc.arg(url) OR final_url = sqlc.arg(url)
ORDER BY canonical_url = sqlc.arg(url) DESC
LIMIT 1;

-- name: UpsertSource :one
INSERT INTO sources (canonical_url, final_url, etag, last_modified, last_fetched_at)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (canonical_url) DO UPDATE
SET final_url = EXCLUDED.final_url,
    etag = EXCLUDED.etag,
    last_modified = EXCLUDED.last_modified,
    last_fetched_at = greatest(sources.last_fetched_at, EXCLUDED.last_fetched_at)
RETURNING id, canonical_url, final_url, etag, last_modified, last_fetched_at;

-- name: TouchSource :exec
UPDATE sources
SET last_fetched_at = greatest(last_fetched_at, sqlc.arg(fetched_at))
WHERE id = sqlc.arg(id);

-- name: NewestSnapshot :one
SELECT id, source_id, body_sha256, body_gzip, content_type, fetched_at
FROM source_snapshots
WHERE source_id = $1
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
