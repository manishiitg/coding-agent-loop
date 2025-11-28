package handlers

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"workspace/models"
	"workspace/services"
	"workspace/utils"

	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
)

// Global services (will be initialized in main)
var (
	qdrantClient     *services.QdrantClient
	embeddingService *services.EmbeddingService
	chunker          *services.Chunker
	fileProcessor    *services.FileProcessor
	semanticEnabled  bool
)

// InitializeSemanticServices initializes the semantic search services
func InitializeSemanticServices() {
	semanticEnabled = true

	qdrantURL := viper.GetString("QDRANT_URL")
	if qdrantURL == "" {
		qdrantURL = "http://localhost:6333"
	}

	qdrantClient = services.NewQdrantClient(qdrantURL)
	embeddingService = services.NewEmbeddingService()
	chunker = services.NewChunker()

	// Initialize file processor with SQLite job queue
	dataDir := "/app/data" // Docker container data directory
	var err error
	fileProcessor, err = services.NewFileProcessor(qdrantClient, embeddingService, chunker, dataDir)
	if err != nil {
		log.Printf("Failed to initialize file processor: %v", err)
		return
	}

	// Start background file processor
	fileProcessor.Start()

	// Initialize collection if needed
	go initializeCollection()
}

// IsSemanticSearchEnabled returns whether semantic search is enabled
func IsSemanticSearchEnabled() bool {
	return semanticEnabled
}

// GetFileProcessor returns the global file processor instance
func GetFileProcessor() *services.FileProcessor {
	return fileProcessor
}

// initializeCollection initializes the Qdrant collection
func initializeCollection() {
	if !qdrantClient.IsAvailable() {
		return
	}

	collectionName := "workspace"
	exists, err := qdrantClient.CollectionExists(collectionName)
	if err != nil {
		fmt.Printf("Error checking collection existence: %v\n", err)
		return
	}

	if !exists {
		vectorSize := embeddingService.GetEmbeddingDimension()
		if err := qdrantClient.CreateCollection(collectionName, vectorSize); err != nil {
			fmt.Printf("Error creating collection: %v\n", err)
		} else {
			fmt.Printf("Created Qdrant collection '%s' with vector size %d\n", collectionName, vectorSize)
		}
	}
}

// SemanticSearch handles GET /api/search/semantic
func SemanticSearch(c *gin.Context) {
	startTime := time.Now()

	var req models.SemanticSearchRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.APIResponse[any]{
			Success: false,
			Message: "Invalid query parameters",
			Error:   err.Error(),
		})
		return
	}

	// Validate folder path if provided
	docsDir := viper.GetString("docs-dir")
	if req.Folder != "" {
		folderPath := filepath.Join(docsDir, req.Folder)
		if !utils.IsValidFilePath(folderPath, docsDir) {
			c.JSON(http.StatusBadRequest, models.APIResponse[any]{
				Success: false,
				Message: "Invalid folder path",
				Error:   "Folder path contains invalid characters or attempts directory traversal",
			})
			return
		}
	}

	// Check if semantic search is enabled
	if !IsSemanticSearchEnabled() {
		c.JSON(http.StatusOK, models.APIResponse[models.SemanticSearchResponse]{
			Success: true,
			Message: "Semantic search is disabled",
			Data: models.SemanticSearchResponse{
				Query:           req.Query,
				SearchMethod:    "disabled",
				VectorDBStatus:  "disabled",
				EmbeddingModel:  "disabled",
				TotalResults:    0,
				SemanticResults: []models.SemanticSearchResult{},
				ProcessingTime:  float64(time.Since(startTime).Nanoseconds()) / 1e6,
			},
		})
		return
	}

	// Check if semantic search is available
	semanticAvailable := qdrantClient.IsAvailable() && embeddingService.IsAvailable()
	if !semanticAvailable {
		c.JSON(http.StatusOK, models.APIResponse[models.SemanticSearchResponse]{
			Success: true,
			Message: "Semantic search is currently unavailable",
			Data: models.SemanticSearchResponse{
				Query:           req.Query,
				SearchMethod:    "unavailable",
				VectorDBStatus:  "unavailable",
				EmbeddingModel:  "unavailable",
				TotalResults:    0,
				SemanticResults: []models.SemanticSearchResult{},
				ProcessingTime:  float64(time.Since(startTime).Nanoseconds()) / 1e6,
			},
		})
		return
	}

	var response models.SemanticSearchResponse
	response.Query = req.Query
	response.ProcessingTime = float64(time.Since(startTime).Nanoseconds()) / 1e6 // Convert to milliseconds

	// Perform semantic search
	semanticResults, err := performSemanticSearch(req)
	if err != nil {
		fmt.Printf("Semantic search error: %v\n", err)
		c.JSON(http.StatusInternalServerError, models.APIResponse[any]{
			Success: false,
			Message: "Semantic search failed",
			Error:   err.Error(),
		})
		return
	}

	response.SemanticResults = semanticResults
	response.SearchMethod = "semantic"
	response.VectorDBStatus = "available"
	response.EmbeddingModel = embeddingService.GetModelInfo()["model"].(string)

	// Calculate total results
	response.TotalResults = len(response.SemanticResults)

	c.JSON(http.StatusOK, models.APIResponse[models.SemanticSearchResponse]{
		Success: true,
		Message: fmt.Sprintf("Found %d results using %s search", response.TotalResults, response.SearchMethod),
		Data:    response,
	})
}

// performSemanticSearch performs semantic search using Qdrant
func performSemanticSearch(req models.SemanticSearchRequest) ([]models.SemanticSearchResult, error) {
	log.Printf("DEBUG: Starting semantic search for query: '%s'", req.Query)
	log.Printf("DEBUG: Search parameters - limit: %d, folder: '%s'", req.Limit, req.Folder)

	// Generate query embedding
	log.Printf("DEBUG: Generating embedding for query...")
	queryEmbedding, err := embeddingService.GenerateEmbedding(req.Query)
	if err != nil {
		log.Printf("DEBUG: Failed to generate query embedding: %v", err)
		return nil, fmt.Errorf("failed to generate query embedding: %w", err)
	}
	log.Printf("DEBUG: Generated embedding with %d dimensions", len(queryEmbedding))

	// Build filter for folder if specified
	var filter map[string]interface{}
	if req.Folder != "" {
		filter = map[string]interface{}{
			"must": []map[string]interface{}{
				{
					"key": "folder",
					"match": map[string]interface{}{
						"value": req.Folder,
					},
				},
			},
		}
		log.Printf("DEBUG: Using folder filter: %+v", filter)
	} else {
		log.Printf("DEBUG: No folder filter specified")
	}

	// Search in Qdrant
	log.Printf("DEBUG: Searching Qdrant collection 'workspace'...")
	searchResults, err := qdrantClient.SearchPoints("workspace", queryEmbedding, filter, req.Limit)
	if err != nil {
		log.Printf("DEBUG: Qdrant search failed: %v", err)
		return nil, fmt.Errorf("failed to search points: %w", err)
	}
	log.Printf("DEBUG: Qdrant returned %d results", len(searchResults))

	// Convert to semantic search results
	var results []models.SemanticSearchResult
	log.Printf("DEBUG: Processing %d search results (no threshold filtering)", len(searchResults))

	for i, result := range searchResults {
		log.Printf("DEBUG: Result %d - Score: %.4f", i+1, result.Score)

		payload := result.Payload
		semanticResult := models.SemanticSearchResult{
			FilePath:     getStringFromPayload(payload, "file_path"),
			ChunkText:    getStringFromPayload(payload, "chunk_text"),
			ChunkIndex:   getIntFromPayload(payload, "chunk_index"),
			Score:        float64(result.Score),
			Folder:       getStringFromPayload(payload, "folder"),
			FileType:     getStringFromPayload(payload, "file_type"),
			WordCount:    getIntFromPayload(payload, "word_count"),
			CharCount:    getIntFromPayload(payload, "char_count"),
			SearchMethod: "semantic",
		}

		results = append(results, semanticResult)
		log.Printf("DEBUG: Added result for file: %s", semanticResult.FilePath)
	}

	log.Printf("DEBUG: Semantic search completed with %d final results", len(results))
	return results, nil
}

// Helper functions for extracting values from payload/map
func getStringFromPayload(payload map[string]interface{}, key string) string {
	if val, ok := payload[key]; ok {
		if str, ok := val.(string); ok {
			return str
		}
	}
	return ""
}

func getIntFromPayload(payload map[string]interface{}, key string) int {
	if val, ok := payload[key]; ok {
		if num, ok := val.(float64); ok {
			return int(num)
		}
		if num, ok := val.(int); ok {
			return num
		}
	}
	return 0
}

// ProcessFile handles POST /api/search/process-file
func ProcessFile(c *gin.Context) {
	var req models.FileProcessingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.APIResponse[any]{
			Success: false,
			Message: "Invalid request body",
			Error:   err.Error(),
		})
		return
	}

	startTime := time.Now()

	// Validate file path
	docsDir := viper.GetString("docs-dir")
	filePath := filepath.Join(docsDir, req.FilePath)
	if !utils.IsValidFilePath(filePath, docsDir) {
		c.JSON(http.StatusBadRequest, models.APIResponse[any]{
			Success: false,
			Message: "Invalid file path",
			Error:   "File path contains invalid characters or attempts directory traversal",
		})
		return
	}

	// Process the file
	response, err := processFileForEmbeddings(req.FilePath, req.Content, req.Action)
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.APIResponse[any]{
			Success: false,
			Message: "Failed to process file",
			Error:   err.Error(),
		})
		return
	}

	response.ProcessingTime = float64(time.Since(startTime).Nanoseconds()) / 1e6

	c.JSON(http.StatusOK, models.APIResponse[*models.FileProcessingResponse]{
		Success: true,
		Message: "File processed successfully",
		Data:    response,
	})
}

// processFileForEmbeddings processes a file for embedding generation
func processFileForEmbeddings(filePath, content, action string) (*models.FileProcessingResponse, error) {
	response := &models.FileProcessingResponse{
		FilePath: filePath,
		Action:   action,
		Success:  false,
	}

	// Check if semantic search is enabled
	if !IsSemanticSearchEnabled() {
		response.Error = "Semantic search is disabled"
		return response, nil
	}

	// Check if services are available
	if !qdrantClient.IsAvailable() || !embeddingService.IsAvailable() {
		response.Error = "Semantic search services not available"
		return response, nil
	}

	// Handle delete action
	if action == "delete" {
		// Delete all points for this file
		// This is a simplified approach - in production you'd want to track point IDs
		err := qdrantClient.DeletePoints("workspace", []string{filePath + "_*"})
		if err != nil {
			response.Error = fmt.Sprintf("Failed to delete embeddings: %v", err)
			return response, err
		}
		response.Success = true
		return response, nil
	}

	// Chunk the content
	chunks := chunker.ChunkText(content)
	response.ChunksCreated = len(chunks)

	if len(chunks) == 0 {
		response.Success = true
		return response, nil
	}

	// Generate embeddings for chunks
	chunkTexts := make([]string, len(chunks))
	for i, chunk := range chunks {
		chunkTexts[i] = chunk.Text
	}

	embeddings, err := embeddingService.BatchGenerateEmbeddings(chunkTexts, 5)
	if err != nil {
		response.Error = fmt.Sprintf("Failed to generate embeddings: %v", err)
		return response, err
	}

	response.EmbeddingsGenerated = len(embeddings)

	// Create points for Qdrant
	var points []services.Point
	for i, embedding := range embeddings {
		if i >= len(chunks) {
			break
		}

		chunk := chunks[i]
		// Generate a simple numeric ID for each chunk
		pointID := fmt.Sprintf("%d", time.Now().UnixNano()+int64(i))

		point := services.Point{
			ID:     pointID,
			Vector: embedding,
			Payload: map[string]interface{}{
				"file_path":   filePath,
				"chunk_text":  chunk.Text,
				"chunk_index": chunk.Index,
				"folder":      filepath.Dir(filePath),
				"file_type":   "file",
				"word_count":  chunk.WordCount,
				"char_count":  chunk.CharCount,
			},
		}

		points = append(points, point)
	}

	// Upsert points to Qdrant
	if err := qdrantClient.UpsertPoints("workspace", points); err != nil {
		response.Error = fmt.Sprintf("Failed to store embeddings: %v", err)
		return response, err
	}

	response.Success = true
	response.Stats = chunker.GetChunkStats(chunks)

	return response, nil
}

// GetJobStatus handles GET /api/semantic/jobs
func GetJobStatus(c *gin.Context) {
	if !IsSemanticSearchEnabled() {
		c.JSON(http.StatusOK, models.APIResponse[map[string]interface{}]{
			Success: true,
			Message: "Semantic search is disabled",
			Data: map[string]interface{}{
				"enabled": false,
				"status":  "disabled",
			},
		})
		return
	}

	if fileProcessor == nil {
		c.JSON(http.StatusOK, models.APIResponse[map[string]interface{}]{
			Success: true,
			Message: "Semantic search service not initialized",
			Data: map[string]interface{}{
				"enabled": true,
				"status":  "not_initialized",
			},
		})
		return
	}

	stats := fileProcessor.GetStats()

	c.JSON(http.StatusOK, models.APIResponse[map[string]interface{}]{
		Success: true,
		Message: "Job status retrieved",
		Data:    stats,
	})
}

// GetSemanticStats handles GET /api/semantic/stats
func GetSemanticStats(c *gin.Context) {
	if !IsSemanticSearchEnabled() {
		c.JSON(http.StatusOK, models.APIResponse[map[string]interface{}]{
			Success: true,
			Message: "Semantic search is disabled",
			Data: map[string]interface{}{
				"enabled": false,
				"status":  "disabled",
				"services": map[string]interface{}{
					"qdrant": map[string]interface{}{
						"available": false,
					},
					"embedding": map[string]interface{}{
						"available": false,
						"model":     "disabled",
					},
				},
				"timestamp": time.Now().Unix(),
			},
		})
		return
	}

	if fileProcessor == nil || qdrantClient == nil || embeddingService == nil {
		c.JSON(http.StatusOK, models.APIResponse[map[string]interface{}]{
			Success: true,
			Message: "Semantic search services not initialized",
			Data: map[string]interface{}{
				"enabled": true,
				"status":  "not_initialized",
			},
		})
		return
	}

	// Get job stats
	jobStats := fileProcessor.GetStats()

	// Get Qdrant collection info
	qdrantAvailable := qdrantClient.IsAvailable()

	// Get embedding service info
	embeddingAvailable := embeddingService.IsAvailable()
	embeddingModel := embeddingService.GetModelInfo()

	stats := map[string]interface{}{
		"services": map[string]interface{}{
			"qdrant": map[string]interface{}{
				"available": qdrantAvailable,
			},
			"embedding": map[string]interface{}{
				"available": embeddingAvailable,
				"model":     embeddingModel,
			},
		},
		"jobs":      jobStats,
		"timestamp": time.Now().Unix(),
	}

	c.JSON(http.StatusOK, models.APIResponse[map[string]interface{}]{
		Success: true,
		Message: "Semantic search statistics",
		Data:    stats,
	})
}

// TriggerResync handles POST /api/semantic/resync
func TriggerResync(c *gin.Context) {
	if !IsSemanticSearchEnabled() {
		c.JSON(http.StatusOK, models.APIResponse[map[string]interface{}]{
			Success: true,
			Message: "Semantic search is disabled",
			Data: map[string]interface{}{
				"enabled": false,
				"status":  "disabled",
				"message": "Cannot trigger resync when semantic search is disabled",
			},
		})
		return
	}

	if fileProcessor == nil || qdrantClient == nil || embeddingService == nil {
		c.JSON(http.StatusOK, models.APIResponse[map[string]interface{}]{
			Success: true,
			Message: "Semantic search services not initialized",
			Data: map[string]interface{}{
				"enabled": true,
				"status":  "not_initialized",
			},
		})
		return
	}

	// Check if services are available
	if !qdrantClient.IsAvailable() {
		c.JSON(http.StatusOK, models.APIResponse[map[string]interface{}]{
			Success: true,
			Message: "Qdrant service is not available",
			Data: map[string]interface{}{
				"enabled": true,
				"status":  "qdrant_unavailable",
			},
		})
		return
	}

	if !embeddingService.IsAvailable() {
		c.JSON(http.StatusOK, models.APIResponse[map[string]interface{}]{
			Success: true,
			Message: "Embedding service is not available",
			Data: map[string]interface{}{
				"enabled": true,
				"status":  "embedding_unavailable",
			},
		})
		return
	}

	// Get configuration
	docsDir := viper.GetString("docs-dir")
	qdrantURL := viper.GetString("qdrant-url")

	// Parse request body for options
	var req struct {
		DryRun bool `json:"dry_run"`
		Force  bool `json:"force"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		// If no JSON body, use defaults
		req.DryRun = false
		req.Force = false
	}

	// Start resync in background goroutine
	go func() {
		log.Printf("Starting API-triggered resync...")

		// Delete existing collection
		if err := qdrantClient.DeleteCollection("workspace"); err != nil {
			log.Printf("Failed to delete collection: %v", err)
		}

		// Create new collection with correct dimensions
		if err := qdrantClient.CreateCollection("workspace", 1536); err != nil {
			log.Printf("Failed to create collection: %v", err)
			return
		}

		// Scan directory for markdown files
		var files []string
		err := filepath.Walk(docsDir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if !info.IsDir() && strings.HasSuffix(strings.ToLower(path), ".md") {
				files = append(files, path)
			}
			return nil
		})

		if err != nil {
			log.Printf("Failed to scan directory: %v", err)
			return
		}

		log.Printf("Found %d markdown files to process", len(files))

		// Queue all files for processing
		queuedCount := 0
		for _, filePath := range files {
			// Read file content
			content, err := os.ReadFile(filePath)
			if err != nil {
				log.Printf("Failed to read file %s: %v", filePath, err)
				continue
			}

			// Queue for processing
			if err := fileProcessor.QueueJob(filePath, string(content), "create"); err != nil {
				log.Printf("Failed to queue file %s: %v", filePath, err)
				continue
			}

			queuedCount++
		}

		log.Printf("API resync completed: queued %d files for processing", queuedCount)
	}()

	c.JSON(http.StatusOK, models.APIResponse[map[string]interface{}]{
		Success: true,
		Message: "Resync triggered successfully",
		Data: map[string]interface{}{
			"docs_dir":   docsDir,
			"qdrant_url": qdrantURL,
			"dry_run":    req.DryRun,
			"force":      req.Force,
			"status":     "started",
			"note":       "Resync is running in background. Check /api/semantic/jobs for progress.",
		},
	})
}
