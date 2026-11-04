-- Every query here is scoped by owner_id; TestOwnedQueriesAreScoped checks it.

-- name: FindOwnerDocumentForURL :one
-- The owner's document for url: a document whose source has url as its
-- canonical or final URL, or one an earlier import of url produced.
SELECT d.id
FROM documents d
WHERE d.owner_id = sqlc.arg(owner_id)
  AND (
    d.source_id IN (SELECT s.id FROM sources s WHERE s.canonical_url = sqlc.arg(url) OR s.final_url = sqlc.arg(url))
    OR d.id IN (
      SELECT i.document_id FROM imports i
      WHERE i.owner_id = sqlc.arg(owner_id) AND i.canonical_url = sqlc.arg(url) AND i.document_id IS NOT NULL
    )
  )
ORDER BY d.created_at, d.id
LIMIT 1;

-- name: CreateDocumentIfAbsent :one
-- Creates the owner's document for a source, or returns the one they
-- already have. created is false when it already existed.
INSERT INTO documents (owner_id, source_id, extraction_id, title, content)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT ON CONSTRAINT documents_owner_source_key DO UPDATE SET owner_id = EXCLUDED.owner_id
RETURNING id, (xmax = 0)::boolean AS created;

-- name: GetDocument :one
-- The document, with the warnings of the extraction it was made from.
SELECT d.id, d.title, s.canonical_url AS source_url, d.version, d.created_at, d.updated_at, d.content,
    COALESCE(e.ir -> 'warnings', '[]'::jsonb)::jsonb AS warnings
FROM documents d
JOIN sources s ON s.id = d.source_id
JOIN extractions e ON e.id = d.extraction_id
WHERE d.owner_id = $1 AND d.id = $2;

-- name: ListDocuments :many
-- The owner's documents, newest first, after the (created_at, id) cursor.
SELECT d.id, d.title, s.canonical_url AS source_url, d.version, d.created_at, d.updated_at
FROM documents d
JOIN sources s ON s.id = d.source_id
WHERE d.owner_id = sqlc.arg(owner_id)
  AND (d.created_at, d.id) < (sqlc.arg(before_created_at)::timestamptz, sqlc.arg(before_id)::uuid)
ORDER BY d.created_at DESC, d.id DESC
LIMIT sqlc.arg(max_rows);

-- name: DocumentExtraction :one
-- The IR of the extraction the owner's document was made from.
SELECT e.ir
FROM documents d
JOIN extractions e ON e.id = d.extraction_id
WHERE d.owner_id = $1 AND d.id = $2;

-- name: SaveDocument :one
-- Replaces the content if the document is still at version, and bumps it.
UPDATE documents
SET content = sqlc.arg(content), version = version + 1, updated_at = now()
WHERE owner_id = sqlc.arg(owner_id) AND id = sqlc.arg(id) AND version = sqlc.arg(version)
RETURNING version;

-- name: DocumentVersion :one
SELECT version FROM documents WHERE owner_id = $1 AND id = $2;

-- name: ResolveDocuments :many
-- For each canonical URL, the owner's document for it, matched as
-- FindOwnerDocumentForURL matches one URL. URLs without one are left out.
SELECT u.url::text AS url, d.id AS document_id
FROM unnest(sqlc.arg(urls)::text[]) AS u (url)
JOIN LATERAL (
    SELECT d.id
    FROM documents d
    WHERE d.owner_id = sqlc.arg(owner_id)
      AND (
        d.source_id IN (SELECT s.id FROM sources s WHERE s.canonical_url = u.url OR s.final_url = u.url)
        OR d.id IN (
          SELECT i.document_id FROM imports i
          WHERE i.owner_id = sqlc.arg(owner_id) AND i.canonical_url = u.url AND i.document_id IS NOT NULL
        )
      )
    ORDER BY d.created_at, d.id
    LIMIT 1
) d ON true;
