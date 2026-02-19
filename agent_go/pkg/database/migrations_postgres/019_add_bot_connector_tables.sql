-- Bot connector configuration (one row per platform)
CREATE TABLE IF NOT EXISTS bot_connector_config (
    id TEXT PRIMARY KEY,
    enabled BOOLEAN DEFAULT FALSE,
    bot_mode BOOLEAN DEFAULT FALSE,
    config_json TEXT DEFAULT '{}',
    default_preset_id TEXT,
    auto_confirm BOOLEAN DEFAULT FALSE,
    allowed_channels TEXT DEFAULT '[]',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Bot sessions (one per conversation thread)
CREATE TABLE IF NOT EXISTS bot_sessions (
    id TEXT PRIMARY KEY,
    platform TEXT NOT NULL,
    channel_id TEXT NOT NULL,
    thread_ts TEXT NOT NULL,
    session_id TEXT,
    user_id TEXT NOT NULL,
    user_name TEXT,
    query TEXT NOT NULL,
    status TEXT DEFAULT 'running',
    preset_id TEXT,
    config_json TEXT,
    thread_context TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    completed_at TIMESTAMP,
    UNIQUE(platform, channel_id, thread_ts)
);

-- Bot messages (audit trail of all messages in a bot session)
CREATE TABLE IF NOT EXISTS bot_messages (
    id TEXT PRIMARY KEY,
    bot_session_id TEXT NOT NULL,
    direction TEXT NOT NULL,
    message_type TEXT NOT NULL,
    content TEXT,
    platform_message_id TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (bot_session_id) REFERENCES bot_sessions(id) ON DELETE CASCADE
);

-- Indexes for efficient lookups
CREATE INDEX IF NOT EXISTS idx_bot_sessions_platform_channel ON bot_sessions(platform, channel_id, thread_ts);
CREATE INDEX IF NOT EXISTS idx_bot_sessions_session_id ON bot_sessions(session_id);
CREATE INDEX IF NOT EXISTS idx_bot_sessions_status ON bot_sessions(status);
CREATE INDEX IF NOT EXISTS idx_bot_messages_session ON bot_messages(bot_session_id);
