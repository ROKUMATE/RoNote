-- +goose Up
-- Each user's own copy of an imported page. Every query on these tables is
-- scoped by owner_id.
CREATE TABLE documents (
    id uuid PRIMARY KEY DEFAULT uuidv7(),
    owner_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    source_id uuid NOT NULL REFERENCES sources (id),
    -- The extraction the document was created from; content starts as its IR.
    extraction_id uuid NOT NULL REFERENCES extractions (id),
    title text NOT NULL,
    -- The user's own IR copy, edited from P10 on.
    content jsonb NOT NULL,
    -- Incremented on every edit, for optimistic concurrency.
    version integer NOT NULL DEFAULT 1 CHECK (version >= 1),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    -- One live document per owner and source.
    CONSTRAINT documents_owner_source_key UNIQUE (owner_id, source_id),
    -- Lets imports reference a document together with its owner.
    CONSTRAINT documents_id_owner_key UNIQUE (id, owner_id)
);

CREATE INDEX documents_owner_created_idx ON documents (owner_id, created_at DESC, id DESC);

CREATE TABLE imports (
    id uuid PRIMARY KEY DEFAULT uuidv7(),
    owner_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    requested_url text NOT NULL CHECK (length(requested_url) <= 2048),
    -- The requested URL, canonicalized.
    canonical_url text NOT NULL CHECK (length(canonical_url) <= 2048),
    status text NOT NULL DEFAULT 'queued' CHECK (status IN ('queued', 'fetching', 'extracting', 'done', 'failed')),
    error_code text,
    document_id uuid,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    -- An import can only point at a document of the same owner.
    CONSTRAINT imports_document_fkey FOREIGN KEY (document_id, owner_id) REFERENCES documents (id, owner_id),
    CONSTRAINT imports_done_has_document CHECK ((status = 'done') = (document_id IS NOT NULL)),
    CONSTRAINT imports_failed_has_error CHECK ((status = 'failed') = (error_code IS NOT NULL))
);

CREATE INDEX imports_owner_canonical_idx ON imports (owner_id, canonical_url);
CREATE INDEX imports_owner_document_idx ON imports (owner_id, document_id);

-- Every new import and every status change notifies the import_status
-- channel inside the transaction that makes it, so listeners hear about it
-- exactly when it commits.
-- +goose StatementBegin
CREATE FUNCTION imports_notify_status() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    PERFORM pg_notify('import_status', json_build_object(
        'import_id', NEW.id,
        'owner_id', NEW.owner_id,
        'status', NEW.status,
        'error_code', NEW.error_code,
        'document_id', NEW.document_id
    )::text);
    RETURN NULL;
END
$$;
-- +goose StatementEnd

CREATE TRIGGER imports_notify_insert AFTER INSERT ON imports
FOR EACH ROW EXECUTE FUNCTION imports_notify_status();

CREATE TRIGGER imports_notify_status AFTER UPDATE OF status ON imports
FOR EACH ROW WHEN (OLD.status IS DISTINCT FROM NEW.status) EXECUTE FUNCTION imports_notify_status();

-- +goose Down
DROP TABLE imports;
DROP FUNCTION imports_notify_status();
DROP TABLE documents;
