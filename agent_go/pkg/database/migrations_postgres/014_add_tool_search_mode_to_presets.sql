-- Migration 014: Add use_tool_search_mode and pre_discovered_tools columns to preset_queries table (Postgres)
-- This allows users to enable tool search mode per preset

ALTER TABLE preset_queries ADD COLUMN IF NOT EXISTS use_tool_search_mode BOOLEAN DEFAULT FALSE;
ALTER TABLE preset_queries ADD COLUMN IF NOT EXISTS pre_discovered_tools TEXT DEFAULT '[]';
