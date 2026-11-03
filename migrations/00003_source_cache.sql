-- +goose Up
-- The cache shared by every user: one source per canonical URL, a snapshot
-- per distinct body, and an extraction per snapshot and extractor version.
CREATE TABLE sources (
    id uuid PRIMARY KEY DEFAULT uuidv7(),
    kind text NOT NULL DEFAULT 'web' CHECK (kind = 'web'),
    canonical_url text NOT NULL UNIQUE CHECK (length(canonical_url) <= 2048),
    final_url text NOT NULL CHECK (length(final_url) <= 2048),
    -- Validators from the last fetch, for conditional requests; '' when the
    -- server sent none.
    etag text NOT NULL DEFAULT '',
    last_modified text NOT NULL DEFAULT '',
    -- When the newest snapshot was last fetched or confirmed by a 304.
    last_fetched_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX sources_final_url_idx ON sources (final_url);

CREATE TABLE source_snapshots (
    id uuid PRIMARY KEY DEFAULT uuidv7(),
    source_id uuid NOT NULL REFERENCES sources (id) ON DELETE CASCADE,
    -- SHA-256 of the body as the server sent it.
    body_sha256 text NOT NULL CHECK (body_sha256 ~ '^[0-9a-f]{64}$'),
    -- The page decoded to UTF-8, gzip-compressed. The fetcher caps pages at
    -- 5 MiB, so the compressed body never comes close to this limit.
    body_gzip bytea NOT NULL CHECK (octet_length(body_gzip) <= 5242880),
    content_type text NOT NULL,
    fetched_at timestamptz NOT NULL,
    UNIQUE (source_id, body_sha256)
);

CREATE INDEX source_snapshots_newest_idx ON source_snapshots (source_id, fetched_at DESC);

CREATE TABLE extractions (
    id uuid PRIMARY KEY DEFAULT uuidv7(),
    snapshot_id uuid NOT NULL REFERENCES source_snapshots (id) ON DELETE CASCADE,
    extractor_name text NOT NULL,
    extractor_version text NOT NULL,
    ir_version integer NOT NULL,
    ir jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (snapshot_id, extractor_name, extractor_version)
);

-- +goose Down
DROP TABLE extractions;
DROP TABLE source_snapshots;
DROP TABLE sources;
