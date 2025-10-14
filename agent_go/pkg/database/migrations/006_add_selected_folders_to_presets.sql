-- Migration 005: Add selected_folder column to preset_queries table
-- This column stores the selected folder for the preset as a single folder path

-- Add selected_folder column to preset_queries table
ALTER TABLE preset_queries ADD COLUMN selected_folder TEXT DEFAULT NULL;

-- Update existing presets to have NULL selected_folder (no migration needed for NULL)
-- The column will be populated when users create/update presets with folder selection
