-- Migration 009: Add use_code_execution_mode column to preset_queries table
-- This allows users to enable/disable MCP code execution mode per preset
-- When enabled, the agent uses generated Go code to access MCP tools instead of exposing them directly

ALTER TABLE preset_queries ADD COLUMN use_code_execution_mode INTEGER DEFAULT 0;

