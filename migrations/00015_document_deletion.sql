-- Documents can be deleted (P18). An import that made a deleted document
-- stays, done and without it: it still counts toward the daily import
-- quota, so deleting can't make room for more imports (ADR 0010).

-- +goose Up
ALTER TABLE imports
    DROP CONSTRAINT imports_document_fkey,
    DROP CONSTRAINT imports_done_has_document,
    ADD CONSTRAINT imports_document_fkey FOREIGN KEY (document_id, owner_id)
        REFERENCES documents (id, owner_id) ON DELETE SET NULL (document_id),
    ADD CONSTRAINT imports_document_when_done CHECK (document_id IS NULL OR status = 'done');

-- +goose Down
DELETE FROM imports WHERE status = 'done' AND document_id IS NULL;
ALTER TABLE imports
    DROP CONSTRAINT imports_document_fkey,
    DROP CONSTRAINT imports_document_when_done,
    ADD CONSTRAINT imports_document_fkey FOREIGN KEY (document_id, owner_id) REFERENCES documents (id, owner_id),
    ADD CONSTRAINT imports_done_has_document CHECK ((status = 'done') = (document_id IS NOT NULL));
