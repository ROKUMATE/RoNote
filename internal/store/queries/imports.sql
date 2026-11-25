-- Every query here is scoped by owner_id; TestOwnedQueriesAreScoped checks it.

-- name: CreateImport :one
INSERT INTO imports (owner_id, requested_url, canonical_url)
VALUES (sqlc.arg(owner_id), sqlc.arg(requested_url)::text, sqlc.arg(canonical_url)::text)
RETURNING *;

-- name: CreatePDFImport :one
INSERT INTO imports (owner_id, kind, filename, pdf_sha256)
VALUES (sqlc.arg(owner_id), 'pdf', sqlc.arg(filename)::text, sqlc.arg(pdf_sha256)::text)
RETURNING *;

-- name: GetImport :one
SELECT *
FROM imports
WHERE owner_id = $1 AND id = $2;

-- name: SetImportStatus :one
-- Moves an import that has not finished to a working status.
UPDATE imports
SET status = sqlc.arg(status), updated_at = now()
WHERE owner_id = sqlc.arg(owner_id) AND id = sqlc.arg(id) AND status NOT IN ('done', 'failed')
RETURNING *;

-- name: FinishImport :one
-- Marks an import done with its document. Finishing it again with the same
-- document changes nothing; a failed import stays failed.
UPDATE imports
SET status = 'done', document_id = sqlc.arg(document_id), updated_at = now()
WHERE owner_id = sqlc.arg(owner_id) AND id = sqlc.arg(id)
  AND (status NOT IN ('done', 'failed') OR (status = 'done' AND document_id = sqlc.arg(document_id)))
RETURNING *;

-- name: SetImportPDFPages :exec
-- Records the page count pdftext read in a PDF import's file.
UPDATE imports
SET pdf_pages = sqlc.arg(pdf_pages), updated_at = now()
WHERE owner_id = sqlc.arg(owner_id) AND id = sqlc.arg(id);

-- name: FailImport :one
-- Marks an import that has not finished as failed with a stable error code.
UPDATE imports
SET status = 'failed', error_code = sqlc.arg(error_code), updated_at = now()
WHERE owner_id = sqlc.arg(owner_id) AND id = sqlc.arg(id) AND status NOT IN ('done', 'failed')
RETURNING *;

-- name: LatestImportForDocument :one
SELECT *
FROM imports
WHERE owner_id = $1 AND document_id = $2
ORDER BY created_at DESC, id DESC
LIMIT 1;

-- name: LockOwner :exec
-- Holds the owner's quota lock until the transaction ends, so two imports
-- can't both pass a check that only one fits under.
SELECT pg_advisory_xact_lock(hashtextextended(sqlc.arg(owner_id)::uuid::text, 0));

-- name: QuotaUsage :one
-- What counts against the owner's quotas: imports started within the window
-- and how many seconds until the oldest of them leaves it; and documents,
-- with the imports still under way, which may each make one.
SELECT
    count(*) FILTER (WHERE created_at > now() - make_interval(secs => sqlc.arg(window_seconds)::double precision))::int AS recent_imports,
    coalesce(extract(epoch FROM min(created_at) FILTER (WHERE created_at > now() - make_interval(secs => sqlc.arg(window_seconds)::double precision))
        + make_interval(secs => sqlc.arg(window_seconds)::double precision) - now()), 0)::double precision AS seconds_until_oldest_leaves,
    ((SELECT count(*) FROM documents d WHERE d.owner_id = sqlc.arg(owner_id))
        + count(*) FILTER (WHERE status NOT IN ('done', 'failed')))::int AS documents
FROM imports
WHERE owner_id = sqlc.arg(owner_id);

-- name: BlobKeysInUse :many
-- The keys among keys that a PDF snapshot holds, or that an unfinished PDF
-- import of its owner will read.
SELECT k::text AS key
FROM unnest(sqlc.arg(keys)::text[]) AS k
WHERE EXISTS (SELECT 1 FROM source_snapshots s WHERE s.blob_key = k)
   OR EXISTS (
        SELECT 1 FROM imports i
        WHERE i.kind = 'pdf' AND i.status NOT IN ('done', 'failed')
          AND 'pdf/' || i.owner_id::text || '/' || i.pdf_sha256 = k
   );
