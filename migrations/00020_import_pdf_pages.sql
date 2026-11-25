-- +goose Up
-- A PDF import keeps the page count pdftext read, refused or not, so a PDF
-- over the cap can say how many pages it has (docs/adr/0013-pdf-import.md);
-- the status event carries it.
ALTER TABLE imports ADD COLUMN pdf_pages integer CHECK (pdf_pages > 0);

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION imports_notify_status() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    PERFORM pg_notify('import_status', json_build_object(
        'import_id', NEW.id,
        'owner_id', NEW.owner_id,
        'status', NEW.status,
        'error_code', NEW.error_code,
        'document_id', NEW.document_id,
        'pdf_pages', NEW.pdf_pages
    )::text);
    RETURN NULL;
END
$$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION imports_notify_status() RETURNS trigger LANGUAGE plpgsql AS $$
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

ALTER TABLE imports DROP COLUMN pdf_pages;
