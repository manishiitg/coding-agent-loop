-- Migration 014: Add use_tool_search_mode and pre_discovered_tools columns to preset_queries table
-- This allows users to enable tool search mode per preset
-- When enabled, agents discover tools on-demand via search_tools instead of loading all tools upfront
-- pre_discovered_tools contains a JSON array of tool names that are always available without searching

ALTER TABLE preset_queries ADD COLUMN use_tool_search_mode INTEGER DEFAULT 0;
ALTER TABLE preset_queries ADD COLUMN pre_discovered_tools TEXT DEFAULT '[]';
