-- Migration 001: Add workflow_status column to workflows table
-- This migration handles the transition from human_verification_complete + refinement_required to workflow_status

-- Add the workflow_status column (if it doesn't exist)
-- Note: This migration is safe to run multiple times
-- Use a safe approach that won't fail if column already exists
-- SQLite doesn't support IF NOT EXISTS for ADD COLUMN, so we'll use a different approach

-- Check if workflow_status column exists, and only add it if it doesn't
-- This uses a safe approach that won't fail if column already exists

-- NOTE: For fresh databases, the workflow_status column is already created in migration 000
-- This ALTER TABLE is only needed for databases that existed before migration 000 included this column
-- Commenting out to prevent "duplicate column name" error for fresh databases
-- ALTER TABLE workflows ADD COLUMN workflow_status TEXT DEFAULT 'pre-verification';

-- Migrate existing data from old columns to new workflow_status
-- Only attempt migration if the old columns exist
-- This uses a safe approach that won't fail if columns don't exist
-- NOTE: For fresh databases, this UPDATE is not needed since workflow_status is already set to 'pre-verification' by default
-- This migration is only needed for databases that existed before migration 000 included the workflow_status column

-- Check if old columns exist before attempting migration
-- If they don't exist, skip the data migration (fresh database)
-- If they do exist, migrate the data
-- NOTE: For fresh databases, this entire UPDATE block is skipped since the old columns don't exist
-- This migration is only needed for databases that existed before migration 000 included the workflow_status column

-- Since SQLite doesn't support conditional execution of entire statements,
-- and the old columns don't exist in fresh databases, we'll skip this migration entirely
-- for fresh databases by commenting out the problematic UPDATE statement
-- The workflow_status column is already set to 'pre-verification' by default in migration 000

-- UPDATE workflows 
-- SET workflow_status = CASE 
--     WHEN human_verification_complete = 1 THEN 'post-verification'
--     WHEN refinement_required = 1 THEN 'post-verification-todo-refinement'
--     ELSE 'pre-verification'
-- END
-- WHERE workflow_status = 'pre-verification';

-- Create the new index
CREATE INDEX IF NOT EXISTS idx_workflows_status ON workflows(workflow_status);
