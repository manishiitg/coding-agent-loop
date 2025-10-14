-- Add selected_options column to workflows table
-- This column stores the selected options for the current workflow phase as JSON

-- NOTE: For fresh databases, the selected_options column is already created in migration 000
-- This ALTER TABLE is only needed for databases that existed before migration 000 included this column
-- Commenting out to prevent "duplicate column name" error for fresh databases
-- ALTER TABLE workflows ADD COLUMN selected_options JSON DEFAULT NULL;

-- Update existing workflows to have NULL selected_options (no migration needed for NULL)
