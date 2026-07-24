-- Additive operation metadata for user-visible, cancellable background work.
ALTER TABLE jobs
    ADD COLUMN stage TEXT NOT NULL DEFAULT 'queued',
    ADD COLUMN message TEXT,
    ADD COLUMN progress_current BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN progress_total BIGINT,
    ADD COLUMN progress_unit TEXT,
    ADD COLUMN progress_rate BIGINT,
    ADD COLUMN eta_seconds BIGINT,
    ADD COLUMN error_code TEXT,
    ADD COLUMN action TEXT,
    ADD COLUMN cancel_requested BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN retry_of UUID REFERENCES jobs (id) ON DELETE SET NULL,
    ADD COLUMN initiated_by BIGINT REFERENCES admin_users (id) ON DELETE SET NULL,
    ADD COLUMN heartbeat_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    ADD COLUMN updated_at TIMESTAMPTZ NOT NULL DEFAULT now();

CREATE INDEX jobs_updated_at ON jobs (updated_at DESC);
CREATE INDEX jobs_retry_of ON jobs (retry_of) WHERE retry_of IS NOT NULL;
CREATE INDEX jobs_initiated_by ON jobs (initiated_by) WHERE initiated_by IS NOT NULL;
