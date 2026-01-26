-- Migration 015: Add selected_skills column to preset_queries table (Postgres)
ALTER TABLE preset_queries ADD COLUMN IF NOT EXISTS selected_skills TEXT DEFAULT '[]';
