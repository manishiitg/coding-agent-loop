CREATE TABLE IF NOT EXISTS scheduled_jobs (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    description TEXT DEFAULT '',
    entity_type TEXT NOT NULL,
    preset_query_id TEXT NOT NULL,
    trigger_payload TEXT DEFAULT NULL,
    group_ids TEXT DEFAULT NULL,
    cron_expression TEXT NOT NULL,
    timezone TEXT DEFAULT 'UTC',
    enabled BOOLEAN DEFAULT TRUE,
    last_run_at TIMESTAMP DEFAULT NULL,
    next_run_at TIMESTAMP DEFAULT NULL,
    last_session_id TEXT DEFAULT NULL,
    last_status TEXT DEFAULT NULL,
    last_error TEXT DEFAULT NULL,
    last_duration_ms BIGINT DEFAULT NULL,
    run_count INTEGER DEFAULT 0,
    consecutive_failures INTEGER DEFAULT 0,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (preset_query_id) REFERENCES preset_queries(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_scheduled_jobs_next_run_at ON scheduled_jobs(next_run_at);
CREATE INDEX IF NOT EXISTS idx_scheduled_jobs_enabled ON scheduled_jobs(enabled);
CREATE INDEX IF NOT EXISTS idx_scheduled_jobs_entity_type ON scheduled_jobs(entity_type);
