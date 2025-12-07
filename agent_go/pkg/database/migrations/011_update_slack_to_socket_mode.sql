-- Migration 011: Update Slack config to Socket Mode (remove signing_secret, add app_token)
-- This migration updates the existing slack_feedback_config table to use Socket Mode

-- Add app_token column if it doesn't exist
ALTER TABLE slack_feedback_config ADD COLUMN app_token TEXT;

-- Note: We keep signing_secret column for backward compatibility but it's no longer used
-- The code will ignore it and only use app_token for Socket Mode

