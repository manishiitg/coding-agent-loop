-- Migration 010: Add Slack integration for human feedback (Socket Mode)
-- Stores Slack configuration for sending feedback notifications

-- Slack configuration table (single row - global config)
CREATE TABLE IF NOT EXISTS slack_feedback_config (
    id TEXT PRIMARY KEY DEFAULT 'slack_config',
    enabled BOOLEAN DEFAULT 0, -- Whether Slack notifications are enabled
    bot_token TEXT, -- Slack bot token (xoxb-...)
    app_token TEXT, -- App-level token (xapp-...) for Socket Mode
    channel_id TEXT, -- Slack channel ID (C1234567890)
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Table to map Slack messages to feedback requests
CREATE TABLE IF NOT EXISTS slack_feedback_messages (
    id TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(16)))),
    unique_id TEXT NOT NULL, -- Maps to HumanFeedbackRequest.UniqueID
    slack_message_ts TEXT NOT NULL, -- Slack message timestamp
    slack_channel_id TEXT NOT NULL, -- Slack channel ID
    slack_thread_ts TEXT, -- Thread timestamp (same as message_ts for parent)
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(unique_id, slack_message_ts)
);

CREATE INDEX IF NOT EXISTS idx_slack_feedback_messages_unique_id ON slack_feedback_messages(unique_id);
CREATE INDEX IF NOT EXISTS idx_slack_feedback_messages_ts ON slack_feedback_messages(slack_message_ts, slack_thread_ts);

