-- Migration 016: Add enable_browser_access column to preset_queries table (Postgres)
ALTER TABLE preset_queries ADD COLUMN IF NOT EXISTS enable_browser_access BOOLEAN DEFAULT FALSE;
