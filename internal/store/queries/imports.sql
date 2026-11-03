-- Every query here is scoped by owner_id; TestOwnedQueriesAreScoped checks it.

-- name: CreateImport :one
INSERT INTO imports (owner_id, requested_url, canonical_url)
VALUES ($1, $2, $3)
RETURNING id, owner_id, requested_url, canonical_url, status, error_code, document_id, created_at, updated_at;

-- name: GetImport :one
SELECT id, owner_id, requested_url, canonical_url, status, error_code, document_id, created_at, updated_at
FROM imports
WHERE owner_id = $1 AND id = $2;

-- name: SetImportStatus :one
-- Moves an import that has not finished to a working status.
UPDATE imports
SET status = sqlc.arg(status), updated_at = now()
WHERE owner_id = sqlc.arg(owner_id) AND id = sqlc.arg(id) AND status NOT IN ('done', 'failed')
RETURNING id, owner_id, requested_url, canonical_url, status, error_code, document_id, created_at, updated_at;

-- name: FinishImport :one
-- Marks an import done with its document. Finishing it again with the same
-- document changes nothing; a failed import stays failed.
UPDATE imports
SET status = 'done', document_id = sqlc.arg(document_id), updated_at = now()
WHERE owner_id = sqlc.arg(owner_id) AND id = sqlc.arg(id)
  AND (status NOT IN ('done', 'failed') OR (status = 'done' AND document_id = sqlc.arg(document_id)))
RETURNING id, owner_id, requested_url, canonical_url, status, error_code, document_id, created_at, updated_at;

-- name: FailImport :one
-- Marks an import that has not finished as failed with a stable error code.
UPDATE imports
SET status = 'failed', error_code = sqlc.arg(error_code), updated_at = now()
WHERE owner_id = sqlc.arg(owner_id) AND id = sqlc.arg(id) AND status NOT IN ('done', 'failed')
RETURNING id, owner_id, requested_url, canonical_url, status, error_code, document_id, created_at, updated_at;

-- name: LatestImportForDocument :one
SELECT id, owner_id, requested_url, canonical_url, status, error_code, document_id, created_at, updated_at
FROM imports
WHERE owner_id = $1 AND document_id = $2
ORDER BY created_at DESC, id DESC
LIMIT 1;
