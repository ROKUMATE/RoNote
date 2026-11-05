-- River claims a job when its scheduled_at is at or before the database's
-- now(), but writes scheduled_at from the app's clock: its pgx driver fills
-- a new job's created_at and scheduled_at with time.Now(), and a retry's
-- time is time.Now() plus a backoff. When the clocks differ, a new job
-- waits out the difference or a retry runs early. This trigger moves those
-- times onto the database's clock and puts the job straight into
-- available, so the claim query is the one comparison that decides when a
-- job runs, on one clock (docs/adr/0005-import-pipeline.md).

-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION river_job_db_clock() RETURNS trigger
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

CREATE TRIGGER river_job_db_clock
BEFORE INSERT OR UPDATE ON river_job
FOR EACH ROW EXECUTE FUNCTION river_job_db_clock();

-- +goose Down
DROP TRIGGER river_job_db_clock ON river_job;
DROP FUNCTION river_job_db_clock();
