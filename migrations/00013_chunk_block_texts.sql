-- Each chunk keeps its top-level blocks' texts beside their ids, so chat can
-- send a chunk to the model a block at a time and map a citation's block
-- range back to blocks (docs/adr/0012-chat.md). Chunks made before have no
-- such texts: they go, and the worker's start-up backfill makes them again,
-- with their embeddings from the cache.

-- +goose Up
DELETE FROM chunks;
UPDATE documents SET indexed_seq = 0;
ALTER TABLE chunks ADD COLUMN block_texts text[] NOT NULL
    CHECK (cardinality(block_texts) = cardinality(block_ids));

-- +goose Down
ALTER TABLE chunks DROP COLUMN block_texts;
