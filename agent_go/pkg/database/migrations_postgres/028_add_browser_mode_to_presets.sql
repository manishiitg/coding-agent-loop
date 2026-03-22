-- Migration 028: Add browser_mode column to preset_queries table (Postgres)
ALTER TABLE preset_queries ADD COLUMN IF NOT EXISTS browser_mode TEXT DEFAULT '';
