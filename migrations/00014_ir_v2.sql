-- IR version 2 adds page anchors and PDF sources (docs/adr/0013-pdf-import.md).
-- Nothing in version 1 changes meaning, so a stored version 1 extraction
-- becomes version 2 by its number alone. Down refuses once an extraction uses
-- what version 1 lacks, rather than drop it.

-- +goose Up
UPDATE extractions SET ir_version = 2, ir = jsonb_set(ir, '{ir_version}', '2');

-- +goose Down
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM extractions
        WHERE ir -> 'source' ? 'pdf'
           OR jsonb_path_exists(ir, '$.blocks.** ? (exists(@.page))')
           OR jsonb_path_exists(ir, '$.warnings[*] ? (@.code == "figures_omitted")')
    ) THEN
        RAISE EXCEPTION 'extractions use IR version 2: page anchors, PDF sources or figures_omitted';
    END IF;
END $$;
-- +goose StatementEnd
UPDATE extractions SET ir_version = 1, ir = jsonb_set(ir, '{ir_version}', '1');
