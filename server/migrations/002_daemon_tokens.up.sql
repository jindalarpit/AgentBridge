-- 002_daemon_tokens.up.sql
-- Add daemon_tokens table for long-lived daemon authentication tokens

CREATE TABLE daemon_tokens (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id TEXT NOT NULL,
    name TEXT NOT NULL CHECK (char_length(name) <= 100),
    token_hash TEXT NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_used_at TIMESTAMPTZ
);

CREATE UNIQUE INDEX idx_daemon_tokens_hash ON daemon_tokens(token_hash);
CREATE INDEX idx_daemon_tokens_user_id ON daemon_tokens(user_id);
