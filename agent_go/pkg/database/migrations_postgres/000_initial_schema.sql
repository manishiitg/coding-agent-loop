-- Migration 000: Initial Database Schema (Postgres)
-- This migration creates the complete initial database schema for Postgres
-- It is the single source of truth for the database structure and matches the Go code expectations

-- Enable UUID extension if available, though we use text IDs for compatibility
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- Preset queries table (stores user-defined and predefined query templates)
CREATE TABLE IF NOT EXISTS preset_queries (
    id TEXT PRIMARY KEY DEFAULT replace(uuid_generate_v4()::text, '-', ''),
    label TEXT NOT NULL,
    query TEXT NOT NULL,
    selected_servers TEXT, -- JSON array
    selected_tools TEXT DEFAULT '[]', -- JSON array of "server:tool" strings
    selected_folder TEXT DEFAULT NULL,
    agent_mode TEXT DEFAULT 'ReAct',
    llm_config JSONB DEFAULT NULL, -- LLM configuration
    use_code_execution_mode BOOLEAN DEFAULT FALSE,
    is_predefined BOOLEAN DEFAULT FALSE,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    created_by TEXT DEFAULT 'user'
);

-- Chat sessions table
CREATE TABLE IF NOT EXISTS chat_sessions (
    id TEXT PRIMARY KEY DEFAULT replace(uuid_generate_v4()::text, '-', ''),
    session_id TEXT UNIQUE NOT NULL,
    title TEXT,
    agent_mode TEXT,
    preset_query_id TEXT,
    config TEXT DEFAULT NULL, -- JSON configuration
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    completed_at TIMESTAMP,
    status TEXT DEFAULT 'active',
    FOREIGN KEY (preset_query_id) REFERENCES preset_queries(id) ON DELETE SET NULL
);

-- Events table
CREATE TABLE IF NOT EXISTS events (
    id TEXT PRIMARY KEY DEFAULT replace(uuid_generate_v4()::text, '-', ''),
    session_id TEXT NOT NULL,
    chat_session_id TEXT REFERENCES chat_sessions(id) ON DELETE CASCADE,
    event_type TEXT NOT NULL,
    timestamp TIMESTAMP NOT NULL,
    event_data JSONB NOT NULL, -- Store as JSONB for better querying capabilities
    FOREIGN KEY (chat_session_id) REFERENCES chat_sessions(id) ON DELETE CASCADE
);

-- Workflows table
CREATE TABLE IF NOT EXISTS workflows (
    id TEXT PRIMARY KEY DEFAULT replace(uuid_generate_v4()::text, '-', ''),
    preset_query_id TEXT NOT NULL,
    workflow_status TEXT DEFAULT 'execution',
    selected_options JSONB DEFAULT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (preset_query_id) REFERENCES preset_queries(id) ON DELETE CASCADE
);

-- Slack configuration table
CREATE TABLE IF NOT EXISTS slack_feedback_config (
    id TEXT PRIMARY KEY DEFAULT 'slack_config',
    enabled BOOLEAN DEFAULT FALSE,
    bot_token TEXT,
    app_token TEXT,
    channel_id TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Slack feedback messages map
CREATE TABLE IF NOT EXISTS slack_feedback_messages (
    id TEXT PRIMARY KEY DEFAULT replace(uuid_generate_v4()::text, '-', ''),
    unique_id TEXT NOT NULL,
    slack_message_ts TEXT NOT NULL,
    slack_channel_id TEXT NOT NULL,
    slack_thread_ts TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(unique_id, slack_message_ts)
);

-- Indexes
CREATE INDEX IF NOT EXISTS idx_chat_sessions_session_id ON chat_sessions(session_id);
CREATE INDEX IF NOT EXISTS idx_chat_sessions_created_at ON chat_sessions(created_at);
CREATE INDEX IF NOT EXISTS idx_chat_sessions_preset_query_id ON chat_sessions(preset_query_id);
CREATE INDEX IF NOT EXISTS idx_chat_sessions_status ON chat_sessions(status);
CREATE INDEX IF NOT EXISTS idx_chat_sessions_preset_created ON chat_sessions(preset_query_id, created_at);

CREATE INDEX IF NOT EXISTS idx_events_session_id ON events(session_id);
CREATE INDEX IF NOT EXISTS idx_events_chat_session_id ON events(chat_session_id);
CREATE INDEX IF NOT EXISTS idx_events_event_type ON events(event_type);
CREATE INDEX IF NOT EXISTS idx_events_timestamp ON events(timestamp);
CREATE INDEX IF NOT EXISTS idx_events_chat_session_timestamp ON events(chat_session_id, timestamp);
CREATE INDEX IF NOT EXISTS idx_events_session_timestamp ON events(session_id, timestamp);
CREATE INDEX IF NOT EXISTS idx_events_type_timestamp ON events(event_type, timestamp);

CREATE INDEX IF NOT EXISTS idx_preset_queries_label ON preset_queries(label);
CREATE INDEX IF NOT EXISTS idx_preset_queries_created_at ON preset_queries(created_at);
CREATE INDEX IF NOT EXISTS idx_preset_queries_is_predefined ON preset_queries(is_predefined);

CREATE INDEX IF NOT EXISTS idx_workflows_preset_query_id ON workflows(preset_query_id);
CREATE INDEX IF NOT EXISTS idx_workflows_status ON workflows(workflow_status);

CREATE INDEX IF NOT EXISTS idx_slack_feedback_messages_unique_id ON slack_feedback_messages(unique_id);
CREATE INDEX IF NOT EXISTS idx_slack_feedback_messages_ts ON slack_feedback_messages(slack_message_ts, slack_thread_ts);
