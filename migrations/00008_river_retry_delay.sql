-- A retry's delay runs from the failure on the database's clock alone.
-- Migration 00006 moved a retry's app-clock time onto the database's by
-- the difference between attempted_at (the claim, database clock) and the
-- error's "at" (River's executor start, app clock). That difference holds
-- the wait between the claim and the start too, so a retry came that much
-- early. Now the River adapter sends a delay as that far past the year
-- 3000, a time River accepts as being in the future, and the trigger turns
-- it into now() plus the delay (docs/adr/0005-import-pipeline.md).

-- +goose Up
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION river_job_db_clock() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'INSERT' THEN
        -- created_at is the app's now() at the insert: its distance from the
        -- database's now() is the offset between the two clocks.
        NEW.scheduled_at := NEW.scheduled_at + (now() - NEW.created_at);
        NEW.created_at := now();
        IF NEW.state = 'scheduled' THEN
            NEW.state := 'available';
        END IF;
    ELSIF OLD.state = 'running'
        AND NEW.state IN ('retryable', 'available')
        AND coalesce(cardinality(NEW.errors), 0) > coalesce(cardinality(OLD.errors), 0) THEN
        -- A retry delay, sent as that far past the year 3000 (the River
        -- adapter's afterDelay): it runs that long after this failure, by
        -- the database's clock alone.
        IF NEW.scheduled_at >= '3000-01-01'::timestamptz AND NEW.scheduled_at < '4000-01-01'::timestamptz THEN
            NEW.scheduled_at := now() + (NEW.scheduled_at - '3000-01-01'::timestamptz);
        END IF;
        NEW.state := 'available';
    END IF;
    RETURN NEW;
END
$$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION river_job_db_clock() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    attempt_started_at timestamptz;
BEGIN
    IF TG_OP = 'INSERT' THEN
        -- created_at is the app's now() at the insert: its distance from the
        -- database's now() is the offset between the two clocks.
        NEW.scheduled_at := NEW.scheduled_at + (now() - NEW.created_at);
        NEW.created_at := now();
        IF NEW.state = 'scheduled' THEN
            NEW.state := 'available';
        END IF;
    ELSIF OLD.state = 'running'
        AND NEW.state IN ('retryable', 'available')
        AND coalesce(cardinality(NEW.errors), 0) > coalesce(cardinality(OLD.errors), 0) THEN
        -- A failed attempt. Its error's "at" is when the attempt started by
        -- the app's clock, and attempted_at the same moment by the
        -- database's.
        attempt_started_at := (NEW.errors[array_upper(NEW.errors, 1)] ->> 'at')::timestamptz;
        NEW.scheduled_at := NEW.scheduled_at + (OLD.attempted_at - attempt_started_at);
        NEW.state := 'available';
    END IF;
    RETURN NEW;
END
$$;
-- +goose StatementEnd
