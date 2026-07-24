ALTER TABLE admin_users
    ADD COLUMN display_name TEXT NOT NULL DEFAULT '',
    ADD COLUMN role TEXT NOT NULL DEFAULT 'admin',
    ADD COLUMN disabled BOOLEAN NOT NULL DEFAULT false;

ALTER TABLE admin_users
    ADD CONSTRAINT admin_users_role_check CHECK (role IN ('admin', 'manager', 'member'));

CREATE TABLE setup_claims (
    token_hash TEXT PRIMARY KEY,
    expires_at TIMESTAMPTZ NOT NULL,
    consumed_at TIMESTAMPTZ
);

CREATE TABLE invitations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    token_hash TEXT NOT NULL UNIQUE,
    role TEXT NOT NULL CHECK (role IN ('admin', 'manager', 'member')),
    workspace_ids JSONB NOT NULL DEFAULT '[]',
    created_by BIGINT NOT NULL REFERENCES admin_users (id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL,
    accepted_by BIGINT REFERENCES admin_users (id),
    accepted_at TIMESTAMPTZ
);

CREATE INDEX invitations_expires_at ON invitations (expires_at);

CREATE TABLE workspace_memberships (
    user_id BIGINT NOT NULL REFERENCES admin_users (id) ON DELETE CASCADE,
    workspace_id TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, workspace_id)
);
