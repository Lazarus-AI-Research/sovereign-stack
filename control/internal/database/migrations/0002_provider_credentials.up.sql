CREATE TABLE provider_credentials (
    id          TEXT PRIMARY KEY,
    provider    TEXT NOT NULL,
    label       TEXT NOT NULL,
    ciphertext  BYTEA NOT NULL,
    nonce       BYTEA NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX provider_credentials_provider ON provider_credentials (provider);
