DROP INDEX IF EXISTS jobs_retry_of;
DROP INDEX IF EXISTS jobs_initiated_by;
DROP INDEX IF EXISTS jobs_updated_at;

ALTER TABLE jobs
    DROP COLUMN IF EXISTS updated_at,
    DROP COLUMN IF EXISTS retry_of,
    DROP COLUMN IF EXISTS initiated_by,
    DROP COLUMN IF EXISTS heartbeat_at,
    DROP COLUMN IF EXISTS cancel_requested,
    DROP COLUMN IF EXISTS action,
    DROP COLUMN IF EXISTS error_code,
    DROP COLUMN IF EXISTS progress_unit,
    DROP COLUMN IF EXISTS eta_seconds,
    DROP COLUMN IF EXISTS progress_rate,
    DROP COLUMN IF EXISTS progress_total,
    DROP COLUMN IF EXISTS progress_current,
    DROP COLUMN IF EXISTS message,
    DROP COLUMN IF EXISTS stage;
