-- 001_initial_schema.up.sql
-- Initial database schema for AgentBridge

-- Users table
CREATE TABLE users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    display_name TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Daemons table (registered daemon instances)
CREATE TABLE daemons (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    daemon_id TEXT NOT NULL UNIQUE,
    status TEXT NOT NULL DEFAULT 'online' CHECK (status IN ('online', 'offline')),
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_daemons_user ON daemons(user_id);
CREATE INDEX idx_daemons_status ON daemons(status);

-- Runtimes table (detected agent CLIs on a daemon)
CREATE TABLE runtimes (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    daemon_id UUID NOT NULL REFERENCES daemons(id) ON DELETE CASCADE,
    agent_type TEXT NOT NULL,
    binary_path TEXT NOT NULL,
    version TEXT NOT NULL DEFAULT 'unknown',
    status TEXT NOT NULL DEFAULT 'available' CHECK (status IN ('available', 'unavailable', 'offline')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_runtimes_daemon ON runtimes(daemon_id);

-- Chat sessions
CREATE TABLE chat_sessions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    runtime_id UUID REFERENCES runtimes(id) ON DELETE SET NULL,
    title TEXT NOT NULL DEFAULT 'New Chat',
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'archived')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_chat_sessions_user ON chat_sessions(user_id, updated_at DESC);

-- Chat messages
CREATE TABLE chat_messages (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    chat_session_id UUID NOT NULL REFERENCES chat_sessions(id) ON DELETE CASCADE,
    seq INTEGER NOT NULL,
    role TEXT NOT NULL CHECK (role IN ('user', 'assistant', 'system')),
    content TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'complete' CHECK (status IN ('pending', 'streaming', 'complete', 'error')),
    elapsed_ms INTEGER,
    failure_reason TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX idx_chat_messages_session_seq ON chat_messages(chat_session_id, seq);
CREATE INDEX idx_chat_messages_session_created ON chat_messages(chat_session_id, created_at);

-- Message buffer for disconnected clients
CREATE TABLE message_buffer (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    payload JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL DEFAULT (now() + INTERVAL '5 minutes')
);

CREATE INDEX idx_message_buffer_user ON message_buffer(user_id, created_at);
CREATE INDEX idx_message_buffer_expires ON message_buffer(expires_at);
