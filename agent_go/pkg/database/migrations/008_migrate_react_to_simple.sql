-- Migration: Update existing ReAct agent_mode records to simple
-- This migration removes ReAct mode support by converting all 'ReAct' agent_mode values to 'simple'

-- Update chat_sessions table
UPDATE chat_sessions 
SET agent_mode = 'simple' 
WHERE agent_mode = 'ReAct';

-- Update preset_queries table
UPDATE preset_queries 
SET agent_mode = 'simple' 
WHERE agent_mode = 'ReAct';

