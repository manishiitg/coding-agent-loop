-- Migration 016: Add enable_browser_access column to preset_queries
-- This column enables browser automation tool (agent_browser) for workflow presets

ALTER TABLE preset_queries ADD COLUMN enable_browser_access INTEGER DEFAULT 0;
