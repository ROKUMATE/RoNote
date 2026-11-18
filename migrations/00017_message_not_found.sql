-- An answer can say the library doesn't answer (P18.3): the model calls
-- not_in_library, and the message ends as not_found (ADR 0012).

-- +goose Up
ALTER TABLE messages
    DROP CONSTRAINT messages_status_check,
    ADD CONSTRAINT messages_status_check CHECK (status IN ('streaming', 'done', 'stopped', 'failed', 'not_found'));

-- +goose Down
UPDATE messages SET status = 'done' WHERE status = 'not_found';
ALTER TABLE messages
    DROP CONSTRAINT messages_status_check,
    ADD CONSTRAINT messages_status_check CHECK (status IN ('streaming', 'done', 'stopped', 'failed'));
