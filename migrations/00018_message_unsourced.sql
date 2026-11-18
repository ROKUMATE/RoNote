-- An answer with text and no valid citation is shown as what it is, not as
-- not found (P18.3): its status is unsourced. A message also counts the
-- citation markers the model wrote into its own text (ADR 0012).

-- +goose Up
ALTER TABLE messages
    DROP CONSTRAINT messages_status_check,
    ADD CONSTRAINT messages_status_check CHECK (status IN ('streaming', 'done', 'stopped', 'failed', 'not_found', 'unsourced')),
    ADD COLUMN self_markers integer NOT NULL DEFAULT 0;

-- +goose Down
UPDATE messages SET status = 'done' WHERE status = 'unsourced';
ALTER TABLE messages
    DROP COLUMN self_markers,
    DROP CONSTRAINT messages_status_check,
    ADD CONSTRAINT messages_status_check CHECK (status IN ('streaming', 'done', 'stopped', 'failed', 'not_found'));
