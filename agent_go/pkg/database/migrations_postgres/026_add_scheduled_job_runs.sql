CREATE TABLE IF NOT EXISTS scheduled_job_runs (
    id TEXT PRIMARY KEY,
    job_id TEXT NOT NULL,
    run_folder TEXT DEFAULT NULL,
    session_id TEXT DEFAULT NULL,
    status TEXT NOT NULL DEFAULT 'running',
    error TEXT DEFAULT NULL,
    duration_ms BIGINT DEFAULT NULL,
    group_ids TEXT DEFAULT NULL,
    started_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    completed_at TIMESTAMP DEFAULT NULL,
    FOREIGN KEY (job_id) REFERENCES scheduled_jobs(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_scheduled_job_runs_job_id ON scheduled_job_runs(job_id);
CREATE INDEX IF NOT EXISTS idx_scheduled_job_runs_started_at ON scheduled_job_runs(started_at);
