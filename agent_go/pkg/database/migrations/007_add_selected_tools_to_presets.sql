-- Migration 007: Add selected_tools column to preset_queries table
-- This allows users to select specific tools within selected servers
-- Format: JSON array of "server:tool" strings (e.g., ["aws:aws_cli_query", "github:create_issue"])

ALTER TABLE preset_queries ADD COLUMN selected_tools TEXT DEFAULT '[]';
