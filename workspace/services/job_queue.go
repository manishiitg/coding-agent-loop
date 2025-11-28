package services

import (
	"database/sql"
	"fmt"
	"log"
	"path/filepath"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// JobQueue manages persistent job storage using SQLite
type JobQueue struct {
	db *sql.DB
}

// NewJobQueue creates a new job queue with SQLite backend
func NewJobQueue(dataDir string) (*JobQueue, error) {
	// Ensure data directory exists
	dbPath := filepath.Join(dataDir, "job_queue.db")

	// Open SQLite database
	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL")
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %v", err)
	}

	jq := &JobQueue{db: db}

	// Initialize database schema
	if err := jq.initSchema(); err != nil {
		return nil, fmt.Errorf("failed to initialize schema: %v", err)
	}

	return jq, nil
}

// initSchema creates the jobs table
func (jq *JobQueue) initSchema() error {
	createTableSQL := `
	CREATE TABLE IF NOT EXISTS jobs (
		id TEXT PRIMARY KEY,
		file_path TEXT NOT NULL,
		content TEXT,
		action TEXT NOT NULL,
		status TEXT NOT NULL DEFAULT 'pending',
		priority INTEGER DEFAULT 0,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		process_after DATETIME DEFAULT CURRENT_TIMESTAMP,
		error TEXT,
		retries INTEGER DEFAULT 0,
		worker_id TEXT
	);

	CREATE INDEX IF NOT EXISTS idx_jobs_status ON jobs(status);
	CREATE INDEX IF NOT EXISTS idx_jobs_priority ON jobs(priority DESC);
	CREATE INDEX IF NOT EXISTS idx_jobs_created_at ON jobs(created_at);
	CREATE INDEX IF NOT EXISTS idx_jobs_process_after ON jobs(process_after);
	`

	_, err := jq.db.Exec(createTableSQL)
	return err
}

// AddJob adds a new job to the queue
func (jq *JobQueue) AddJob(job ProcessingJob) error {
	now := time.Now()
	job.CreatedAt = now
	job.UpdatedAt = now
	job.Status = "pending"

	query := `
	INSERT INTO jobs (id, file_path, content, action, status, priority, created_at, updated_at, process_after, retries)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err := jq.db.Exec(query, job.ID, job.FilePath, job.Content, job.Action, job.Status, job.Priority, job.CreatedAt, job.UpdatedAt, job.ProcessAfter, job.Retries)
	if err != nil {
		return fmt.Errorf("failed to add job: %v", err)
	}

	log.Printf("Added job %s for %s (action: %s, priority: %d, process after: %v)", job.ID, job.FilePath, job.Action, job.Priority, job.ProcessAfter)
	return nil
}

// GetNextJob retrieves the next pending job that's ready to be processed
func (jq *JobQueue) GetNextJob() (*ProcessingJob, error) {
	query := `
	SELECT id, file_path, content, action, status, priority, created_at, updated_at, process_after, error, retries, worker_id
	FROM jobs 
	WHERE status = 'pending' AND process_after <= CURRENT_TIMESTAMP
	ORDER BY priority DESC, created_at ASC 
	LIMIT 1
	`

	row := jq.db.QueryRow(query)

	job := &ProcessingJob{}
	var errorStr, workerIDStr sql.NullString

	err := row.Scan(&job.ID, &job.FilePath, &job.Content, &job.Action, &job.Status, &job.Priority, &job.CreatedAt, &job.UpdatedAt, &job.ProcessAfter, &errorStr, &job.Retries, &workerIDStr)

	if err == sql.ErrNoRows {
		return nil, nil // No pending jobs ready to be processed
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get next job: %v", err)
	}

	// Handle NULL values
	if errorStr.Valid {
		job.Error = errorStr.String
	}
	if workerIDStr.Valid {
		job.WorkerID = workerIDStr.String
	}

	// If this is a failed job being retried, reset its status to pending
	if job.Status == "failed" {
		// Update the job status in the database to pending
		_, err := jq.db.Exec("UPDATE jobs SET status = 'pending' WHERE id = ?", job.ID)
		if err != nil {
			log.Printf("Failed to reset job status to pending: %v", err)
		}
		job.Status = "pending"
		log.Printf("Retrying failed job %s (attempt %d/3)", job.ID, job.Retries+1)
	}

	return job, nil
}

// ClaimJob marks a job as being processed by a worker
func (jq *JobQueue) ClaimJob(jobID, workerID string) error {
	query := `
	UPDATE jobs 
	SET status = 'processing', worker_id = ?, updated_at = CURRENT_TIMESTAMP
	WHERE id = ? AND status = 'pending'
	`

	result, err := jq.db.Exec(query, workerID, jobID)
	if err != nil {
		return fmt.Errorf("failed to claim job: %v", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %v", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("job %s not found or already claimed", jobID)
	}

	log.Printf("Worker %s claimed job %s", workerID, jobID)
	return nil
}

// CompleteJob marks a job as completed
func (jq *JobQueue) CompleteJob(jobID string) error {
	query := `
	UPDATE jobs 
	SET status = 'completed', updated_at = CURRENT_TIMESTAMP
	WHERE id = ?
	`

	_, err := jq.db.Exec(query, jobID)
	if err != nil {
		return fmt.Errorf("failed to complete job: %v", err)
	}

	log.Printf("Completed job %s", jobID)
	return nil
}

// FailJob marks a job as failed and increments retry count
func (jq *JobQueue) FailJob(jobID, errorMsg string) error {
	// First, get the current retry count
	var retries int
	err := jq.db.QueryRow("SELECT retries FROM jobs WHERE id = ?", jobID).Scan(&retries)
	if err != nil {
		return fmt.Errorf("failed to get retry count: %v", err)
	}

	newRetries := retries + 1
	var status string

	if newRetries >= 3 {
		status = "failed" // Permanent failure after 3 retries
		log.Printf("Job %s permanently failed after %d retries: %s", jobID, newRetries, errorMsg)
	} else {
		status = "failed" // Will be retried by GetNextJob
		log.Printf("Job %s failed (attempt %d/3), will retry: %s", jobID, newRetries, errorMsg)
	}

	query := `
	UPDATE jobs 
	SET status = ?, error = ?, retries = ?, updated_at = CURRENT_TIMESTAMP
	WHERE id = ?
	`

	_, err = jq.db.Exec(query, status, errorMsg, newRetries, jobID)
	if err != nil {
		return fmt.Errorf("failed to mark job as failed: %v", err)
	}

	return nil
}

// RetryJob resets a failed job back to pending (if retries < max)
func (jq *JobQueue) RetryJob(jobID string, maxRetries int) error {
	query := `
	UPDATE jobs 
	SET status = 'pending', worker_id = NULL, updated_at = CURRENT_TIMESTAMP
	WHERE id = ? AND retries < ?
	`

	result, err := jq.db.Exec(query, jobID, maxRetries)
	if err != nil {
		return fmt.Errorf("failed to retry job: %v", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %v", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("job %s not found or exceeded max retries", jobID)
	}

	log.Printf("Retrying job %s", jobID)
	return nil
}

// GetJobStats returns statistics about the job queue
func (jq *JobQueue) GetJobStats() (map[string]int, error) {
	query := `
	SELECT status, COUNT(*) 
	FROM jobs 
	GROUP BY status
	`

	rows, err := jq.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to get job stats: %v", err)
	}
	defer rows.Close()

	stats := make(map[string]int)
	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			return nil, fmt.Errorf("failed to scan stats row: %v", err)
		}
		stats[status] = count
	}

	return stats, nil
}

// CleanupOldJobs removes completed jobs older than specified duration
func (jq *JobQueue) CleanupOldJobs(olderThan time.Duration) error {
	cutoff := time.Now().Add(-olderThan)

	query := `
	DELETE FROM jobs 
	WHERE status = 'completed' AND updated_at < ?
	`

	result, err := jq.db.Exec(query, cutoff)
	if err != nil {
		return fmt.Errorf("failed to cleanup old jobs: %v", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %v", err)
	}

	if rowsAffected > 0 {
		log.Printf("Cleaned up %d old completed jobs", rowsAffected)
	}

	return nil
}

// ResetStuckJobs resets jobs that have been processing for more than the specified duration
func (jq *JobQueue) ResetStuckJobs(timeoutDuration time.Duration) error {
	cutoff := time.Now().Add(-timeoutDuration)

	query := `
	UPDATE jobs 
	SET status = 'pending', worker_id = NULL, updated_at = CURRENT_TIMESTAMP
	WHERE status = 'processing' AND updated_at < ?
	`

	result, err := jq.db.Exec(query, cutoff)
	if err != nil {
		return fmt.Errorf("failed to reset stuck jobs: %v", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %v", err)
	}

	if rowsAffected > 0 {
		log.Printf("Reset %d stuck jobs that were processing for more than %v", rowsAffected, timeoutDuration)
	}

	return nil
}

// GetStuckJobsCount returns the count of jobs stuck in processing state
func (jq *JobQueue) GetStuckJobsCount(timeoutDuration time.Duration) (int, error) {
	cutoff := time.Now().Add(-timeoutDuration)

	query := `
	SELECT COUNT(*) 
	FROM jobs 
	WHERE status = 'processing' AND updated_at < ?
	`

	var count int
	err := jq.db.QueryRow(query, cutoff).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count stuck jobs: %v", err)
	}

	return count, nil
}

// CancelPendingJobsForFile cancels all pending jobs for a specific file path
func (jq *JobQueue) CancelPendingJobsForFile(filePath string) error {
	query := `
	UPDATE jobs 
	SET status = 'cancelled', updated_at = CURRENT_TIMESTAMP
	WHERE file_path = ? AND status IN ('pending', 'processing')
	`

	result, err := jq.db.Exec(query, filePath)
	if err != nil {
		return fmt.Errorf("failed to cancel pending jobs for file: %v", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %v", err)
	}

	if rowsAffected > 0 {
		log.Printf("Cancelled %d pending jobs for %s", rowsAffected, filePath)
	}

	return nil
}

// GetPendingJobsForFile returns all pending jobs for a specific file path
func (jq *JobQueue) GetPendingJobsForFile(filePath string) ([]ProcessingJob, error) {
	query := `
	SELECT id, file_path, content, action, status, priority, created_at, updated_at, process_after, error, retries, worker_id
	FROM jobs 
	WHERE file_path = ? AND status IN ('pending', 'processing')
	ORDER BY created_at ASC
	`

	rows, err := jq.db.Query(query, filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to query pending jobs for file: %v", err)
	}
	defer rows.Close()

	var jobs []ProcessingJob
	for rows.Next() {
		job := ProcessingJob{}
		var errorStr, workerIDStr sql.NullString

		err := rows.Scan(&job.ID, &job.FilePath, &job.Content, &job.Action, &job.Status, &job.Priority, &job.CreatedAt, &job.UpdatedAt, &job.ProcessAfter, &errorStr, &job.Retries, &workerIDStr)
		if err != nil {
			return nil, fmt.Errorf("failed to scan job: %v", err)
		}

		// Handle NULL values
		if errorStr.Valid {
			job.Error = errorStr.String
		}
		if workerIDStr.Valid {
			job.WorkerID = workerIDStr.String
		}

		jobs = append(jobs, job)
	}

	return jobs, nil
}

// Close closes the database connection
func (jq *JobQueue) Close() error {
	return jq.db.Close()
}
