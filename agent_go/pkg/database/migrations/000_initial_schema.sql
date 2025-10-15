-- Migration 000: Initial Database Schema
-- This migration creates the complete initial database schema
-- This is the single source of truth for the database structure

-- Chat sessions table (simple)
CREATE TABLE IF NOT EXISTS chat_sessions (
    id TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(16)))),
    session_id TEXT UNIQUE NOT NULL, -- Maps to events.SessionID
    title TEXT, -- Auto-generated title
    agent_mode TEXT, -- simple, react, orchestrator
    preset_query_id TEXT, -- Optional link to preset query
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    completed_at DATETIME,
    status TEXT DEFAULT 'active', -- active, completed, error
    FOREIGN KEY (preset_query_id) REFERENCES preset_queries(id) ON DELETE SET NULL
);

-- Events table (stores all typed events as JSON)
CREATE TABLE IF NOT EXISTS events (
    id TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(16)))),
    session_id TEXT NOT NULL,
    chat_session_id TEXT REFERENCES chat_sessions(id) ON DELETE CASCADE,
    
    -- Event identification
    event_type TEXT NOT NULL, -- Maps to events.EventType
    timestamp DATETIME NOT NULL,
    
    -- Store the complete typed event as JSON
    event_data TEXT NOT NULL, -- JSON string
    
    -- Foreign key constraint
    FOREIGN KEY (chat_session_id) REFERENCES chat_sessions(id) ON DELETE CASCADE
);

-- Preset queries table (stores user-defined and predefined query templates)
-- This table stores both custom user presets and predefined system presets
-- Custom presets are created by users and stored in the database
-- Predefined presets are built-in and can be customized with server selections
CREATE TABLE IF NOT EXISTS preset_queries (
    id TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(16)))),
    label TEXT NOT NULL, -- Display name for the preset
    query TEXT NOT NULL, -- The actual query text
    selected_servers TEXT, -- JSON array of server names (e.g., ["aws", "github"])
    selected_folder TEXT DEFAULT NULL, -- Single folder path for orchestrator/workflow modes
    agent_mode TEXT DEFAULT 'ReAct', -- Agent mode: simple, ReAct, orchestrator, workflow
    is_predefined INTEGER DEFAULT 0, -- Whether this is a built-in preset (SQLite uses INTEGER for boolean)
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    created_by TEXT DEFAULT 'user' -- For future multi-user support
);

-- Workflows table (stores workflow state for todo-list-based execution)
-- Links to preset_queries for reusable workflow templates
-- Objectives are now handled via preset queries and file context is managed separately
CREATE TABLE IF NOT EXISTS workflows (
    id TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(16)))),
    preset_query_id TEXT NOT NULL, -- Links to preset_queries.id for workflow templates
    workflow_status TEXT DEFAULT 'pre-verification', -- Current workflow status: 'pre-verification', 'post-verification', 'post-verification-todo-refinement'
    selected_options JSON DEFAULT NULL, -- Selected options for the current workflow phase (JSON object)
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (preset_query_id) REFERENCES preset_queries(id) ON DELETE CASCADE
);

-- Create indexes for better performance
CREATE INDEX IF NOT EXISTS idx_chat_sessions_session_id ON chat_sessions(session_id);
CREATE INDEX IF NOT EXISTS idx_chat_sessions_created_at ON chat_sessions(created_at);
CREATE INDEX IF NOT EXISTS idx_chat_sessions_preset_query_id ON chat_sessions(preset_query_id);
CREATE INDEX IF NOT EXISTS idx_events_session_id ON events(session_id);
CREATE INDEX IF NOT EXISTS idx_events_chat_session_id ON events(chat_session_id);
CREATE INDEX IF NOT EXISTS idx_events_event_type ON events(event_type);
CREATE INDEX IF NOT EXISTS idx_events_timestamp ON events(timestamp);
CREATE INDEX IF NOT EXISTS idx_preset_queries_label ON preset_queries(label);
CREATE INDEX IF NOT EXISTS idx_preset_queries_created_at ON preset_queries(created_at);
CREATE INDEX IF NOT EXISTS idx_preset_queries_is_predefined ON preset_queries(is_predefined);

-- Workflow-specific indexes
CREATE INDEX IF NOT EXISTS idx_workflows_preset_query_id ON workflows(preset_query_id);
CREATE INDEX IF NOT EXISTS idx_workflows_status ON workflows(workflow_status);
