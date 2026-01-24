-- Migration 015: Add selected_skills column to preset_queries table
-- This allows users to configure which skills are enabled for workflow presets
-- selected_skills contains a JSON array of skill folder names

ALTER TABLE preset_queries ADD COLUMN selected_skills TEXT DEFAULT '[]';
