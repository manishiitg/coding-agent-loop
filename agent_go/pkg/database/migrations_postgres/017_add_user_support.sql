-- Migration 017: Add Multi-User Support (PostgreSQL)
-- This migration adds session isolation by user_id
-- Users are managed via AUTH_USERS env var (hardcoded), not a database table

-- Add user_id to chat_sessions (nullable for backwards compatibility with single-user mode)
-- In single-user mode, this will be populated with DEFAULT_USER_ID
-- In multi-user mode, this will be populated with the hardcoded user's ID
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'chat_sessions' AND column_name = 'user_id') THEN
        ALTER TABLE chat_sessions ADD COLUMN user_id TEXT;
    END IF;
END $$;

-- Add user_id to preset_queries (nullable for backwards compatibility)
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'preset_queries' AND column_name = 'user_id') THEN
        ALTER TABLE preset_queries ADD COLUMN user_id TEXT;
    END IF;
END $$;

-- Session shares table for sharing sessions via link
CREATE TABLE IF NOT EXISTS session_shares (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id TEXT NOT NULL,
    share_token TEXT UNIQUE NOT NULL,
    created_by TEXT NOT NULL,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    expires_at TIMESTAMPTZ,
    access_level TEXT DEFAULT 'read',
    FOREIGN KEY (session_id) REFERENCES chat_sessions(session_id) ON DELETE CASCADE
);

-- Create indexes for user lookups
CREATE INDEX IF NOT EXISTS idx_chat_sessions_user_id ON chat_sessions(user_id);
CREATE INDEX IF NOT EXISTS idx_preset_queries_user_id ON preset_queries(user_id);
CREATE INDEX IF NOT EXISTS idx_session_shares_token ON session_shares(share_token);
CREATE INDEX IF NOT EXISTS idx_session_shares_session_id ON session_shares(session_id);
