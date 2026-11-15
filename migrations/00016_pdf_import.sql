-- PDF imports (docs/adr/0013-pdf-import.md). A PDF's source belongs to one
-- owner and is keyed by the file's SHA-256 there, never shared: a cache hit
-- across users would tell one that another uploaded the file. Its bytes live
-- in blob storage, so its snapshot keeps a blob key instead of a body. An
-- import names a URL or a PDF, and a PDF import has a converting step.

-- +goose Up
ALTER TABLE sources
    DROP CONSTRAINT sources_kind_check,
    ADD COLUMN owner_id uuid REFERENCES users (id) ON DELETE CASCADE,
    ADD COLUMN pdf_sha256 text CHECK (pdf_sha256 ~ '^[0-9a-f]{64}$'),
    ALTER COLUMN canonical_url DROP NOT NULL,
    ALTER COLUMN final_url DROP NOT NULL,
    ADD CONSTRAINT sources_kind_check CHECK (
        (kind = 'web' AND owner_id IS NULL AND pdf_sha256 IS NULL AND canonical_url IS NOT NULL AND final_url IS NOT NULL)
        OR (kind = 'pdf' AND owner_id IS NOT NULL AND pdf_sha256 IS NOT NULL AND canonical_url IS NULL AND final_url IS NULL)
    );

CREATE UNIQUE INDEX sources_owner_pdf_key ON sources (owner_id, pdf_sha256) WHERE kind = 'pdf';

ALTER TABLE source_snapshots
    ALTER COLUMN body_gzip DROP NOT NULL,
    ADD COLUMN blob_key text CHECK (length(blob_key) <= 512),
    ADD CONSTRAINT source_snapshots_body_or_blob CHECK ((body_gzip IS NULL) <> (blob_key IS NULL));

ALTER TABLE imports
    ADD COLUMN kind text NOT NULL DEFAULT 'web' CHECK (kind IN ('web', 'pdf')),
    ADD COLUMN filename text CHECK (length(filename) BETWEEN 1 AND 255),
    ADD COLUMN pdf_sha256 text CHECK (pdf_sha256 ~ '^[0-9a-f]{64}$'),
    ALTER COLUMN requested_url DROP NOT NULL,
    ALTER COLUMN canonical_url DROP NOT NULL,
    DROP CONSTRAINT imports_status_check,
    ADD CONSTRAINT imports_status_check CHECK (status IN ('queued', 'fetching', 'converting', 'extracting', 'done', 'failed')),
    ADD CONSTRAINT imports_kind_fields CHECK (
        (kind = 'web' AND requested_url IS NOT NULL AND canonical_url IS NOT NULL AND filename IS NULL AND pdf_sha256 IS NULL)
        OR (kind = 'pdf' AND requested_url IS NULL AND canonical_url IS NULL AND filename IS NOT NULL AND pdf_sha256 IS NOT NULL)
    );

-- +goose Down
DELETE FROM imports WHERE kind = 'pdf';
DELETE FROM documents WHERE source_id IN (SELECT id FROM sources WHERE kind = 'pdf');
DELETE FROM sources WHERE kind = 'pdf';
ALTER TABLE imports
    DROP CONSTRAINT imports_kind_fields,
    DROP CONSTRAINT imports_status_check,
    ADD CONSTRAINT imports_status_check CHECK (status IN ('queued', 'fetching', 'extracting', 'done', 'failed')),
    ALTER COLUMN canonical_url SET NOT NULL,
    ALTER COLUMN requested_url SET NOT NULL,
    DROP COLUMN pdf_sha256,
    DROP COLUMN filename,
    DROP COLUMN kind;
ALTER TABLE source_snapshots
    DROP CONSTRAINT source_snapshots_body_or_blob,
    DROP COLUMN blob_key,
    ALTER COLUMN body_gzip SET NOT NULL;
DROP INDEX sources_owner_pdf_key;
ALTER TABLE sources
    DROP CONSTRAINT sources_kind_check,
    ALTER COLUMN final_url SET NOT NULL,
    ALTER COLUMN canonical_url SET NOT NULL,
    DROP COLUMN pdf_sha256,
    DROP COLUMN owner_id,
    ADD CONSTRAINT sources_kind_check CHECK (kind = 'web');
