-- Every query here is scoped by owner_id; TestOwnedQueriesAreScoped checks it.

-- name: ListAnnotations :many
SELECT * FROM annotations
WHERE owner_id = $1 AND document_id = $2
ORDER BY created_at, id;

-- name: LockDocumentContent :one
-- The document's content, locked until the transaction ends, so annotations
-- are checked against the text a save cannot change meanwhile.
SELECT content FROM documents WHERE owner_id = $1 AND id = $2 FOR UPDATE;

-- name: CountAnnotations :one
SELECT count(*) FROM annotations WHERE owner_id = $1 AND document_id = $2;

-- name: CreateAnnotation :one
INSERT INTO annotations (owner_id, document_id, block_id, kind, selector, note)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetAnnotation :one
SELECT * FROM annotations
WHERE owner_id = $1 AND id = $2;

-- name: UpdateAnnotation :one
-- Replaces block, selector and note if the annotation is still at version,
-- and bumps it.
UPDATE annotations
SET block_id = sqlc.arg(block_id), selector = sqlc.arg(selector), note = sqlc.narg(note),
    version = version + 1, updated_at = now()
WHERE owner_id = sqlc.arg(owner_id) AND id = sqlc.arg(id) AND version = sqlc.arg(version)
RETURNING *;

-- name: DeleteAnnotation :execrows
DELETE FROM annotations WHERE owner_id = $1 AND id = $2;
