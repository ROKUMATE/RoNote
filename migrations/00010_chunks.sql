-- Retrieval (docs/adr/0011-retrieval.md): documents and notes cut into
-- chunks, each with its words for full-text search and its embedding, and
-- a cache of embeddings shared across users. The first use of pgvector.

-- +goose Up
CREATE EXTENSION IF NOT EXISTS vector;

-- index_seq moves with every change search sees; indexed_seq is the one
-- the chunks were made from.
ALTER TABLE documents
    ADD COLUMN index_seq bigint NOT NULL DEFAULT 1,
    ADD COLUMN indexed_seq bigint NOT NULL DEFAULT 0;

CREATE TABLE chunks (
    id uuid PRIMARY KEY DEFAULT uuidv7(),
    owner_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    document_id uuid NOT NULL,
    -- The chunk's blocks in order, top-level ids; the first is where a
    -- result lands.
    block_ids text[] NOT NULL CHECK (cardinality(block_ids) >= 1),
    heading_path text[] NOT NULL,
    kind text NOT NULL CHECK (kind IN ('prose', 'code', 'note')),
    text text NOT NULL,
    lang text NOT NULL DEFAULT '',
    -- Its order in the document.
    position integer NOT NULL,
    tsv tsvector NOT NULL,
    embedding halfvec(1024),
    -- The model of embedding, or '' while there is none.
    model text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT chunks_document_fkey FOREIGN KEY (document_id, owner_id)
        REFERENCES documents (id, owner_id) ON DELETE CASCADE
);

-- Every search reads one user's chunks: owner first.
CREATE INDEX chunks_owner_idx ON chunks (owner_id, document_id);
CREATE INDEX chunks_tsv_idx ON chunks USING gin (tsv);

CREATE TABLE embedding_cache (
    model text NOT NULL,
    text_sha256 bytea NOT NULL CHECK (octet_length(text_sha256) = 32),
    embedding halfvec(1024) NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (model, text_sha256)
);

-- +goose Down
DROP TABLE embedding_cache;
DROP TABLE chunks;
ALTER TABLE documents DROP COLUMN indexed_seq, DROP COLUMN index_seq;
DROP EXTENSION vector;
