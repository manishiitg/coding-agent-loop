-- Migration 029: Drop preset_query_id foreign key from chat_sessions
-- The preset_queries table is no longer used (manifest-based workflows).
-- Keep the column as a plain string identifier for filtering sessions by workflow.
ALTER TABLE chat_sessions DROP CONSTRAINT IF EXISTS chat_sessions_preset_query_id_fkey;
