-- Migration 028: Add browser_mode column to preset_queries table
ALTER TABLE preset_queries ADD COLUMN browser_mode TEXT DEFAULT '';
