-- Documents move from a copy of their extraction's IR to DocContent v1
-- (docs/adr/0006-editor-and-doc-content.md): {doc_version, blocks}, each
-- top-level block marked with its origin. Every existing block is an
-- unedited source block. The title, source, extractor and warnings stay in
-- the extraction.

-- +goose Up
UPDATE documents
SET content = jsonb_build_object(
    'doc_version', 1,
    'blocks', COALESCE(
        (
            SELECT jsonb_agg(
                b || jsonb_build_object('origin', 'source', 'edited', false, 'source_block_id', b -> 'id')
                ORDER BY n
            )
            FROM jsonb_array_elements(content -> 'blocks') WITH ORDINALITY AS t (b, n)
        ),
        '[]'::jsonb
    )
);

COMMENT ON COLUMN documents.content IS 'DocContent v1: the user''s blocks, each marked source or user (ADR 0006).';

-- +goose Down
-- The IR is rebuilt from the extraction's title, source, extractor and
-- warnings and the document's current blocks, so edits and added blocks
-- survive as plain IR blocks.
UPDATE documents d
SET content = (e.ir - 'blocks') || jsonb_build_object(
    'blocks', COALESCE(
        (
            SELECT jsonb_agg(b - 'origin' - 'edited' - 'source_block_id' ORDER BY n)
            FROM jsonb_array_elements(d.content -> 'blocks') WITH ORDINALITY AS t (b, n)
        ),
        '[]'::jsonb
    )
)
FROM extractions e
WHERE e.id = d.extraction_id;

COMMENT ON COLUMN documents.content IS NULL;
