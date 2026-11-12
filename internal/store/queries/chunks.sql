-- Every query on chunks is scoped by owner_id; TestOwnedQueriesAreScoped checks it.

-- name: DeleteDocumentChunks :exec
DELETE FROM chunks
WHERE owner_id = $1 AND document_id = $2;

-- name: InsertChunk :exec
-- Headings and prose are read as English, code as it is; idents is a
-- tsvector literal of the identifier tokens, weighted A.
INSERT INTO chunks (owner_id, document_id, block_ids, heading_path, kind, text, lang, position, tsv, embedding, model)
VALUES (
    sqlc.arg(owner_id), sqlc.arg(document_id), sqlc.arg(block_ids)::text[], sqlc.arg(heading_path)::text[],
    sqlc.arg(kind), sqlc.arg(text), sqlc.arg(lang), sqlc.arg(position),
    setweight(to_tsvector('english', sqlc.arg(headings)::text), 'A')
        || setweight(to_tsvector('english', sqlc.arg(prose)::text), 'B')
        || setweight(to_tsvector('simple', sqlc.arg(code)::text), 'C')
        || sqlc.arg(idents)::text::tsvector,
    sqlc.narg(embedding)::halfvec, sqlc.arg(model)
);

-- name: CountDocumentChunks :one
SELECT count(*)::int
FROM chunks
WHERE owner_id = $1 AND document_id = $2;

-- name: GetCachedEmbeddings :many
SELECT text_sha256, embedding::halfvec AS embedding
FROM embedding_cache
WHERE model = $1 AND text_sha256 = ANY(sqlc.arg(hashes)::bytea[]);

-- name: PutCachedEmbedding :exec
INSERT INTO embedding_cache (model, text_sha256, embedding)
VALUES ($1, $2, sqlc.arg(embedding)::halfvec)
ON CONFLICT DO NOTHING;
