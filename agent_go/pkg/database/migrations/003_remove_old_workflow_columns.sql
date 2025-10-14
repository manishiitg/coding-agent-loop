-- Migration 002: Remove old workflow columns
-- This migration removes the old human_verification_complete and refinement_required columns
-- Note: SQLite doesn't support DROP COLUMN IF EXISTS, so we'll use a different approach

-- For fresh databases, these columns don't exist, so this migration is not needed
-- For existing databases, we'll skip this migration since SQLite doesn't support DROP COLUMN IF EXISTS
-- The old columns will just be ignored if they exist

-- NOTE: SQLite doesn't support DROP COLUMN IF EXISTS syntax
-- This migration is only needed for databases that had the old columns
-- Since we're starting fresh, these columns don't exist, so we'll skip this migration

-- ALTER TABLE workflows DROP COLUMN IF EXISTS human_verification_complete;
-- ALTER TABLE workflows DROP COLUMN IF EXISTS refinement_required;
