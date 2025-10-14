-- Migration 003: Remove objective column from workflows table
-- This migration removes the objective column since objectives are now handled via preset queries
-- and file context is managed separately during execution

-- Note: SQLite doesn't support DROP COLUMN in older versions, so this migration is skipped
-- This is not critical - the old column will just be ignored

-- No SQL to execute for SQLite compatibility
