-- A user's highlights and notes (docs/adr/0007-annotations-and-anchoring.md).
-- They live outside the document's content and point into one block's
-- plain text by a selector, re-anchored when the text changes.

-- +goose Up
CREATE TABLE annotations (
    id uuid PRIMARY KEY DEFAULT uuidv7(),
    owner_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    document_id uuid NOT NULL,
    -- The block, at any depth of the document's content, whose text the
    -- selector points into.
    block_id text NOT NULL,
    kind text NOT NULL CHECK (kind IN ('highlight')),
    -- {position: {start, end}, quote: {exact, prefix, suffix}}, offsets in
    -- UTF-16 code units of the block's plain text.
    selector jsonb NOT NULL,
    note text CHECK (char_length(note) <= 5000),
    -- Incremented on every change, for optimistic concurrency.
    version integer NOT NULL DEFAULT 1 CHECK (version >= 1),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    -- Only a document of the same owner, and gone with it.
    CONSTRAINT annotations_document_fkey FOREIGN KEY (document_id, owner_id)
        REFERENCES documents (id, owner_id) ON DELETE CASCADE
);

CREATE INDEX annotations_owner_document_idx ON annotations (owner_id, document_id, created_at, id);

-- +goose Down
DROP TABLE annotations;
