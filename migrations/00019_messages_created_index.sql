-- Chat's spending across all users is capped per UTC day (ADR 0014): each
-- question sums the day's messages' cost, which this index finds by time.

-- +goose Up
CREATE INDEX messages_created_idx ON messages (created_at);

-- +goose Down
DROP INDEX messages_created_idx;
