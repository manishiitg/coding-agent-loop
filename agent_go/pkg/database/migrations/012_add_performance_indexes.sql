-- Migration 012: Add composite indexes for better query performance
-- This migration adds composite indexes to optimize common queries:
-- 1. ListChatSessions: JOIN + GROUP BY + MAX(timestamp) + COUNT
-- 2. GetEventsBySession: WHERE session_id ORDER BY timestamp
-- 3. GetEvents: Filtering by event_type + ordering

-- Composite index for chat_sessions (optimize filtering by preset_query_id + ordering by created_at)
CREATE INDEX IF NOT EXISTS idx_chat_sessions_preset_created ON chat_sessions(preset_query_id, created_at);

-- Index for chat_sessions status (if filtering by status becomes common)
CREATE INDEX IF NOT EXISTS idx_chat_sessions_status ON chat_sessions(status);

-- Composite indexes for events (critical for performance)
-- Optimizes: JOIN + GROUP BY + MAX(timestamp) + COUNT in ListChatSessions
-- This is the most important index for improving ListChatSessions performance
CREATE INDEX IF NOT EXISTS idx_events_chat_session_timestamp ON events(chat_session_id, timestamp);

-- Optimizes: GetEventsBySession WHERE session_id ORDER BY timestamp ASC
CREATE INDEX IF NOT EXISTS idx_events_session_timestamp ON events(session_id, timestamp);

-- Optimizes: GetEvents with event_type filter + ORDER BY timestamp DESC
CREATE INDEX IF NOT EXISTS idx_events_type_timestamp ON events(event_type, timestamp);
