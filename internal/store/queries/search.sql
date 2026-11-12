-- Every query on chunks is scoped by owner_id; TestOwnedQueriesAreScoped checks it.

-- name: SearchChunks :many
-- Hybrid search over one user's chunks (docs/adr/0011-retrieval.md): the
-- top 50 by words and the top 50 by meaning, each read through the owner's
-- rows alone, merged by reciprocal rank fusion. A null query vector leaves
-- the meaning half out; use_words false leaves the words half out.
WITH q AS (
    SELECT websearch_to_tsquery('english', sqlc.arg(q)::text)
        || websearch_to_tsquery('simple', sqlc.arg(q)::text)
        || CASE WHEN sqlc.arg(ident_query)::text = '' THEN ''::tsquery ELSE sqlc.arg(ident_query)::text::tsquery END AS query
),
words AS (
    SELECT c.id, ts_rank_cd(c.tsv, q.query) AS rank
    FROM chunks c, q
    WHERE sqlc.arg(use_words)::boolean
      AND c.owner_id = sqlc.arg(owner_id)
      AND (sqlc.narg(document_id)::uuid IS NULL OR c.document_id = sqlc.narg(document_id)::uuid)
      AND c.tsv @@ q.query
    ORDER BY rank DESC, c.id
    LIMIT 50
),
meaning AS (
    SELECT c.id, c.embedding <=> sqlc.narg(query_vector)::halfvec AS distance
    FROM chunks c
    WHERE sqlc.narg(query_vector)::halfvec IS NOT NULL
      AND c.owner_id = sqlc.arg(owner_id)
      AND (sqlc.narg(document_id)::uuid IS NULL OR c.document_id = sqlc.narg(document_id)::uuid)
      AND c.model = sqlc.arg(model)
      AND c.embedding IS NOT NULL
    ORDER BY distance, c.id
    LIMIT 50
),
fused AS (
    SELECT coalesce(w.id, m.id) AS id, w.r AS words_rank, m.r AS meaning_rank, m.distance,
        coalesce(1.0 / (sqlc.arg(k)::int + w.r), 0) + coalesce(1.0 / (sqlc.arg(k)::int + m.r), 0) AS score
    FROM (SELECT id, row_number() OVER (ORDER BY rank DESC, id) AS r FROM words) w
    FULL JOIN (SELECT id, distance, row_number() OVER (ORDER BY distance, id) AS r FROM meaning) m ON w.id = m.id
)
SELECT c.document_id, d.title, c.block_ids[1]::text AS block_id, c.heading_path, c.kind,
    -- 0 and -1 mean the half didn't find the chunk.
    f.score::double precision AS score, coalesce(f.words_rank, 0)::int AS words_rank,
    coalesce(f.meaning_rank, 0)::int AS meaning_rank, coalesce(f.distance, -1)::double precision AS distance,
    CASE WHEN f.words_rank IS NOT NULL
        THEN ts_headline(CASE WHEN c.kind = 'code' THEN 'simple' ELSE 'english' END::regconfig, c.text, q.query,
            'StartSel=' || chr(57344) || ', StopSel=' || chr(57345) || ', MaxWords=30, MinWords=12, ShortWord=2, MaxFragments=2, FragmentDelimiter=" … "')
        ELSE left(c.text, 240)
    END::text AS snippet
FROM fused f
JOIN chunks c ON c.id = f.id AND c.owner_id = sqlc.arg(owner_id)
JOIN documents d ON d.id = c.document_id AND d.owner_id = c.owner_id
CROSS JOIN q
ORDER BY f.score DESC, c.id
LIMIT sqlc.arg(max_results);
