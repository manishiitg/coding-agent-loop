package services

import (
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ProcessingJob represents a file processing job
type ProcessingJob struct {
	ID           string    `json:"id"`
	FilePath     string    `json:"file_path"`
	Content      string    `json:"content"`
	Action       string    `json:"action"`   // "create", "update", "delete"
	Status       string    `json:"status"`   // "pending", "processing", "completed", "failed"
	Priority     int       `json:"priority"` // Higher number = higher priority
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	ProcessAfter time.Time `json:"process_after"` // When this job can be processed
	Error        string    `json:"error,omitempty"`
	Retries      int       `json:"retries"`
	WorkerID     string    `json:"worker_id,omitempty"`
}

// FileProcessor handles background processing of files for embeddings
type FileProcessor struct {
	qdrantClient     *QdrantClient
	embeddingService *EmbeddingService
	chunker          *Chunker
	jobQueue         *JobQueue
	workerCount      int
	running          bool
	dataDir          string
}

// NewFileProcessor creates a new file processor
func NewFileProcessor(qdrantClient *QdrantClient, embeddingService *EmbeddingService, chunker *Chunker, dataDir string) (*FileProcessor, error) {
	// Initialize SQLite job queue
	jobQueue, err := NewJobQueue(dataDir)
	if err != nil {
		return nil, fmt.Errorf("failed to create job queue: %v", err)
	}

	return &FileProcessor{
		qdrantClient:     qdrantClient,
		embeddingService: embeddingService,
		chunker:          chunker,
		jobQueue:         jobQueue,
		workerCount:      3, // Default to 3 workers
		running:          false,
		dataDir:          dataDir,
	}, nil
}

// Start starts the background processing workers
func (fp *FileProcessor) Start() {
	if fp.running {
		return
	}

	fp.running = true
	log.Printf("Starting file processor with %d workers", fp.workerCount)

	// Start worker goroutines
	for i := 0; i < fp.workerCount; i++ {
		go fp.worker(i)
	}

	// Start periodic cleanup goroutine for stuck jobs
	go fp.cleanupStuckJobs()
}

// Stop stops the background processing workers
func (fp *FileProcessor) Stop() {
	if !fp.running {
		return
	}

	fp.running = false

	// Close SQLite connection
	if fp.jobQueue != nil {
		fp.jobQueue.Close()
	}

	log.Println("File processor stopped")
}

// QueueJob queues a file processing job with deduplication and 5-minute delay
func (fp *FileProcessor) QueueJob(filePath, content, action string) error {
	if !fp.running {
		log.Printf("File processor not running, skipping job for %s", filePath)
		return fmt.Errorf("file processor not running")
	}

	// Cancel any existing pending jobs for this file to prevent duplicates
	if err := fp.jobQueue.CancelPendingJobsForFile(filePath); err != nil {
		log.Printf("Warning: Failed to cancel pending jobs for %s: %v", filePath, err)
		// Continue anyway - better to have duplicates than fail completely
	}

	// Set ProcessAfter to 5 minutes from now to allow for deduplication
	processAfter := time.Now().Add(5 * time.Minute)

	job := ProcessingJob{
		ID:           uuid.New().String(),
		FilePath:     filePath,
		Content:      content,
		Action:       action,
		Priority:     fp.getJobPriority(action),
		ProcessAfter: processAfter,
		Retries:      0,
	}

	err := fp.jobQueue.AddJob(job)
	if err != nil {
		log.Printf("Failed to queue %s job for %s: %v", action, filePath, err)
		return err
	}

	log.Printf("Queued %s job for %s (deduplicated, process after: %v)", action, filePath, processAfter)
	return nil
}

// QueueJobWithPriority queues a file processing job with specific priority, deduplication and 5-minute delay
func (fp *FileProcessor) QueueJobWithPriority(filePath, content, action string, priority int) error {
	if !fp.running {
		log.Printf("File processor not running, skipping job for %s", filePath)
		return fmt.Errorf("file processor not running")
	}

	// Cancel any existing pending jobs for this file to prevent duplicates
	if err := fp.jobQueue.CancelPendingJobsForFile(filePath); err != nil {
		log.Printf("Warning: Failed to cancel pending jobs for %s: %v", filePath, err)
		// Continue anyway - better to have duplicates than fail completely
	}

	// Set ProcessAfter to 5 minutes from now to allow for deduplication
	processAfter := time.Now().Add(5 * time.Minute)

	job := ProcessingJob{
		ID:           uuid.New().String(),
		FilePath:     filePath,
		Content:      content,
		Action:       action,
		Priority:     priority,
		ProcessAfter: processAfter,
		Retries:      0,
	}

	err := fp.jobQueue.AddJob(job)
	if err != nil {
		log.Printf("Failed to queue %s job for %s: %v", action, filePath, err)
		return err
	}

	log.Printf("Queued %s job (priority %d) for %s (deduplicated, process after: %v)", action, priority, filePath, processAfter)
	return nil
}

// worker processes jobs from the SQLite queue
func (fp *FileProcessor) worker(workerID int) {
	workerIDStr := fmt.Sprintf("worker-%d", workerID)
	log.Printf("Worker %s started", workerIDStr)

	for fp.running {
		// Get next job from SQLite queue
		log.Printf("Worker %s checking for next job...", workerIDStr)
		job, err := fp.jobQueue.GetNextJob()
		if err != nil {
			log.Printf("Worker %s failed to get next job: %v", workerIDStr, err)
			time.Sleep(5 * time.Second) // Wait before retrying
			continue
		}

		if job == nil {
			// No pending jobs, wait a bit
			log.Printf("Worker %s found no pending jobs, waiting...", workerIDStr)
			time.Sleep(1 * time.Second)
			continue
		}

		// Claim the job
		err = fp.jobQueue.ClaimJob(job.ID, workerIDStr)
		if err != nil {
			log.Printf("Worker %s failed to claim job %s: %v", workerIDStr, job.ID, err)
			continue
		}

		startTime := time.Now()
		log.Printf("Worker %s processing %s job for %s", workerIDStr, job.Action, job.FilePath)

		err = fp.processJob(*job)
		processingTime := time.Since(startTime)

		if err != nil {
			log.Printf("Worker %s failed to process %s job for %s: %v (took %v)",
				workerIDStr, job.Action, job.FilePath, err, processingTime)

			// Mark job as failed
			fp.jobQueue.FailJob(job.ID, err.Error())
		} else {
			log.Printf("Worker %s completed %s job for %s (took %v)",
				workerIDStr, job.Action, job.FilePath, processingTime)

			// Mark job as completed
			fp.jobQueue.CompleteJob(job.ID)
		}
	}

	log.Printf("Worker %s stopped", workerIDStr)
}

// processJob processes a single job
func (fp *FileProcessor) processJob(job ProcessingJob) error {
	// Check if services are available
	if !fp.qdrantClient.IsAvailable() || !fp.embeddingService.IsAvailable() {
		return fmt.Errorf("semantic search services not available")
	}

	// Handle delete action
	if job.Action == "delete" {
		return fp.deleteFileEmbeddings(job.FilePath)
	}

	// Handle create/update actions
	return fp.processFileEmbeddings(job.FilePath, job.Content)
}

// processFileEmbeddings processes a file for embedding generation
func (fp *FileProcessor) processFileEmbeddings(filePath, content string) error {
	// Skip if content is empty
	if strings.TrimSpace(content) == "" {
		log.Printf("Skipping empty content for %s", filePath)
		return nil
	}

	// For updates, delete existing embeddings first to avoid stale data
	// This ensures we don't have leftover embeddings if the file has fewer chunks
	log.Printf("Deleting existing embeddings for %s before processing", filePath)
	if err := fp.deleteFileEmbeddings(filePath); err != nil {
		log.Printf("Warning: Failed to delete existing embeddings for %s: %v", filePath, err)
		// Continue processing even if deletion fails
	}

	// Chunk the content
	chunks := fp.chunker.ChunkText(content)
	if len(chunks) == 0 {
		log.Printf("No chunks generated for %s", filePath)
		return nil
	}

	log.Printf("Generated %d chunks for %s", len(chunks), filePath)

	// Generate embeddings for chunks in batches
	chunkTexts := make([]string, len(chunks))
	for i, chunk := range chunks {
		chunkTexts[i] = chunk.Text
	}

	log.Printf("Starting embedding generation for %s (%d chunks, batch size 5)", filePath, len(chunkTexts))
	startTime := time.Now()

	embeddings, err := fp.embeddingService.BatchGenerateEmbeddings(chunkTexts, 5)
	embeddingTime := time.Since(startTime)

	if err != nil {
		log.Printf("Embedding generation failed for %s after %v: %v", filePath, embeddingTime, err)
		return fmt.Errorf("failed to generate embeddings: %w", err)
	}

	log.Printf("Generated %d embeddings for %s in %v", len(embeddings), filePath, embeddingTime)

	// Create points for Qdrant
	var points []Point
	for i, embedding := range embeddings {
		if i >= len(chunks) {
			break
		}

		chunk := chunks[i]
		// Generate a simple ID for each chunk (Qdrant compatible)
		pointID := fmt.Sprintf("%s_%d", filePath, i)

		// Extract folder and file type
		folder := filepath.Dir(filePath)
		if folder == "." {
			folder = ""
		}

		fileType := "file"
		if strings.HasSuffix(strings.ToLower(filePath), ".md") {
			fileType = "markdown"
		}

		point := Point{
			ID:     pointID,
			Vector: embedding,
			Payload: map[string]interface{}{
				"file_path":   filePath,
				"chunk_text":  chunk.Text,
				"chunk_index": chunk.Index,
				"folder":      folder,
				"file_type":   fileType,
				"word_count":  chunk.WordCount,
				"char_count":  chunk.CharCount,
				"created_at":  time.Now().Unix(),
			},
		}

		points = append(points, point)
	}

	// Upsert points to Qdrant
	log.Printf("Upserting %d points to Qdrant for %s", len(points), filePath)
	upsertStartTime := time.Now()

	if err := fp.qdrantClient.UpsertPoints("workspace", points); err != nil {
		upsertTime := time.Since(upsertStartTime)
		log.Printf("Qdrant upsert failed for %s after %v: %v", filePath, upsertTime, err)
		return fmt.Errorf("failed to store embeddings: %w", err)
	}

	upsertTime := time.Since(upsertStartTime)
	log.Printf("Successfully stored %d embeddings for %s in %v", len(points), filePath, upsertTime)
	return nil
}

// deleteFileEmbeddings deletes embeddings for a file
func (fp *FileProcessor) deleteFileEmbeddings(filePath string) error {
	log.Printf("Deleting embeddings for %s", filePath)

	// First, query for all points with this file_path to get their IDs
	filter := map[string]interface{}{
		"file_path": filePath,
	}

	// Use a dummy vector to query (we only need the IDs from the filter)
	dummyVector := make([]float32, 1536) // OpenAI embedding dimension
	for i := range dummyVector {
		dummyVector[i] = 0.0
	}

	// Query for all points with this file_path
	results, err := fp.qdrantClient.SearchPoints("workspace", dummyVector, filter, 1000) // Large limit to get all
	if err != nil {
		log.Printf("Failed to query points for deletion: %v", err)
		return fmt.Errorf("failed to query points for deletion: %w", err)
	}

	if len(results) == 0 {
		log.Printf("No embeddings found for %s", filePath)
		return nil
	}

	// Extract point IDs
	var pointIDs []string
	for _, result := range results {
		pointIDs = append(pointIDs, result.ID)
	}

	log.Printf("Found %d embeddings to delete for %s", len(pointIDs), filePath)

	// Delete the points
	if err := fp.qdrantClient.DeletePoints("workspace", pointIDs); err != nil {
		log.Printf("Failed to delete points for %s: %v", filePath, err)
		return fmt.Errorf("failed to delete points: %w", err)
	}

	log.Printf("Successfully deleted %d embeddings for %s", len(pointIDs), filePath)
	return nil
}

// getJobPriority returns the priority for a job based on action
func (fp *FileProcessor) getJobPriority(action string) int {
	switch action {
	case "delete":
		return 3 // High priority for deletes
	case "update":
		return 2 // Medium priority for updates
	case "create":
		return 1 // Low priority for creates
	default:
		return 1
	}
}

// GetStats returns statistics about the file processor
func (fp *FileProcessor) GetStats() map[string]interface{} {
	stats := map[string]interface{}{
		"running":      fp.running,
		"worker_count": fp.workerCount,
	}

	// Get job queue statistics from SQLite
	if fp.jobQueue != nil {
		jobStats, err := fp.jobQueue.GetJobStats()
		if err == nil {
			stats["job_stats"] = jobStats
		}
	}

	return stats
}

// ResetStuckJobs resets jobs that have been stuck in processing state
func (fp *FileProcessor) ResetStuckJobs(timeoutDuration time.Duration) error {
	if fp.jobQueue == nil {
		return fmt.Errorf("job queue not initialized")
	}
	return fp.jobQueue.ResetStuckJobs(timeoutDuration)
}

// GetStuckJobsCount returns the count of jobs stuck in processing state
func (fp *FileProcessor) GetStuckJobsCount(timeoutDuration time.Duration) (int, error) {
	if fp.jobQueue == nil {
		return 0, fmt.Errorf("job queue not initialized")
	}
	return fp.jobQueue.GetStuckJobsCount(timeoutDuration)
}

// SetWorkerCount sets the number of worker goroutines
func (fp *FileProcessor) SetWorkerCount(count int) {
	if count > 0 && count <= 10 { // Limit to reasonable range
		fp.workerCount = count
		log.Printf("Set worker count to %d", count)
	}
}

// IsRunning returns whether the processor is running
func (fp *FileProcessor) IsRunning() bool {
	return fp.running
}

// QueueSize returns the current queue size
func (fp *FileProcessor) QueueSize() int {
	if fp.jobQueue == nil {
		return 0
	}

	stats, err := fp.jobQueue.GetJobStats()
	if err != nil {
		return 0
	}

	return stats["pending"] + stats["processing"]
}

// cleanupStuckJobs periodically resets jobs that have been stuck in processing state
func (fp *FileProcessor) cleanupStuckJobs() {
	log.Printf("Starting stuck jobs cleanup routine")

	// Check for stuck jobs every 5 minutes
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for fp.running {
		<-ticker.C
		// Reset jobs that have been processing for more than 10 minutes
		timeoutDuration := 10 * time.Minute

		// First check how many stuck jobs we have
		stuckCount, err := fp.jobQueue.GetStuckJobsCount(timeoutDuration)
		if err != nil {
			log.Printf("Failed to count stuck jobs: %v", err)
			continue
		}

		if stuckCount > 0 {
			log.Printf("Found %d jobs stuck in processing state for more than %v", stuckCount, timeoutDuration)

			// Reset the stuck jobs
			err = fp.jobQueue.ResetStuckJobs(timeoutDuration)
			if err != nil {
				log.Printf("Failed to reset stuck jobs: %v", err)
			}
		}
	}

	log.Printf("Stuck jobs cleanup routine stopped")
}
