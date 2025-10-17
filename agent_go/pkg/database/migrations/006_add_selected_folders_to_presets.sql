-- Migration 006: Add selected_folder column to preset_queries table
-- This column stores the selected folder for the preset as a single folder path

-- Add selected_folder column to preset_queries table (only if it doesn't exist)
-- SQLite doesn't support IF NOT EXISTS for ALTER TABLE ADD COLUMN, so we use a different approach
-- We'll try to add the column and ignore the error if it already exists

-- Note: This migration is designed to be idempotent
-- If the column already exists, the ALTER TABLE will fail but that's expected
-- The migration system will handle this gracefully

ALTER TABLE preset_queries ADD COLUMN selected_folder TEXT DEFAULT NULL;

-- Update existing presets to have NULL selected_folder (no migration needed for NULL)
-- The column will be populated when users create/update presets with folder selection
