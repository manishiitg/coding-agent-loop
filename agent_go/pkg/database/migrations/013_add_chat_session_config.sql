-- Migration 013: Add Config Column to Chat Sessions
-- This migration adds a JSON config column to store chat session configuration
-- (MCP servers, code execution mode, workspace settings, etc.)

ALTER TABLE chat_sessions ADD COLUMN config TEXT DEFAULT NULL;
