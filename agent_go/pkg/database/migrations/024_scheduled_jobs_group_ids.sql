-- Add group_ids column to scheduled_jobs for workflow group selection
-- NULL means run all groups; non-null JSON array specifies which group IDs to run
ALTER TABLE scheduled_jobs ADD COLUMN group_ids TEXT DEFAULT NULL;
