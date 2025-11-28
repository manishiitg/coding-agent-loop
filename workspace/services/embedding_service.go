package services

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// EmbeddingService handles text embedding generation using OpenAI
type EmbeddingService struct {
	apiKey  string
	model   string
	client  *http.Client
	enabled bool
}

// OpenAIEmbeddingRequest represents the request structure for OpenAI embedding API
type OpenAIEmbeddingRequest struct {
	Input string `json:"input"`
	Model string `json:"model"`
}

// OpenAIEmbeddingResponse represents the response structure from OpenAI embedding API
type OpenAIEmbeddingResponse struct {
	Object string `json:"object"`
	Data   []struct {
		Object    string    `json:"object"`
		Index     int       `json:"index"`
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
	Model string `json:"model"`
	Usage struct {
		PromptTokens int `json:"prompt_tokens"`
		TotalTokens  int `json:"total_tokens"`
	} `json:"usage"`
}

// NewEmbeddingService creates a new embedding service
func NewEmbeddingService() *EmbeddingService {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		fmt.Println("WARNING: OPENAI_API_KEY is not set. Embedding service will be disabled.")
		return &EmbeddingService{enabled: false}
	}

	model := os.Getenv("OPENAI_EMBEDDING_MODEL")
	if model == "" {
		model = "text-embedding-3-small" // Default OpenAI embedding model
	}

	fmt.Printf("DEBUG: Initializing OpenAI embedding service with model: %s\n", model)
	fmt.Printf("DEBUG: API key length: %d characters\n", len(apiKey))

	return &EmbeddingService{
		apiKey: apiKey,
		model:  model,
		client: &http.Client{
			Timeout: 60 * time.Second, // Longer timeout for embedding generation
		},
		enabled: true,
	}
}

// IsAvailable checks if OpenAI embedding service is available
func (e *EmbeddingService) IsAvailable() bool {
	if !e.enabled {
		fmt.Println("DEBUG: Embedding service is disabled")
		return false
	}

	fmt.Println("DEBUG: Testing OpenAI embedding service availability...")

	// Try to make a simple request to check if API key is valid
	req := OpenAIEmbeddingRequest{
		Input: "test",
		Model: e.model,
	}

	jsonData, err := json.Marshal(req)
	if err != nil {
		fmt.Printf("DEBUG: Failed to marshal request: %v\n", err)
		return false
	}

	httpReq, err := http.NewRequest("POST", "https://api.openai.com/v1/embeddings", bytes.NewBuffer(jsonData))
	if err != nil {
		fmt.Printf("DEBUG: Failed to create HTTP request: %v\n", err)
		return false
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+e.apiKey)

	resp, err := e.client.Do(httpReq)
	if err != nil {
		fmt.Printf("DEBUG: HTTP request failed: %v\n", err)
		return false
	}
	defer resp.Body.Close()

	fmt.Printf("DEBUG: OpenAI API response status: %d\n", resp.StatusCode)

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		fmt.Printf("DEBUG: OpenAI API error response: %s\n", string(body))
		return false
	}

	fmt.Println("DEBUG: OpenAI embedding service is available")
	return true
}

// GenerateEmbedding generates a single embedding for the given text
func (e *EmbeddingService) GenerateEmbedding(text string) ([]float32, error) {
	embeddings, err := e.GenerateEmbeddings([]string{text})
	if err != nil {
		return nil, err
	}

	if len(embeddings) == 0 {
		return nil, fmt.Errorf("no embeddings generated")
	}

	return embeddings[0], nil
}

// GenerateEmbeddings generates embeddings for multiple texts with retry logic
func (e *EmbeddingService) GenerateEmbeddings(texts []string) ([][]float32, error) {
	if !e.IsAvailable() {
		return nil, fmt.Errorf("embedding service is not available")
	}

	var allEmbeddings [][]float32

	// Process texts one by one (OpenAI API handles single inputs better)
	for _, text := range texts {
		embedding, err := e.generateEmbeddingWithRetry(text)
		if err != nil {
			return nil, fmt.Errorf("failed to generate embedding for text: %w", err)
		}
		allEmbeddings = append(allEmbeddings, embedding)
	}

	return allEmbeddings, nil
}

// generateEmbeddingWithRetry generates a single embedding with retry logic
func (e *EmbeddingService) generateEmbeddingWithRetry(text string) ([]float32, error) {
	const maxRetries = 3
	const baseDelay = time.Second

	for attempt := 0; attempt <= maxRetries; attempt++ {
		req := OpenAIEmbeddingRequest{
			Input: text,
			Model: e.model,
		}

		jsonData, err := json.Marshal(req)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request: %w", err)
		}

		httpReq, err := http.NewRequest("POST", "https://api.openai.com/v1/embeddings", bytes.NewBuffer(jsonData))
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}

		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Authorization", "Bearer "+e.apiKey)

		resp, err := e.client.Do(httpReq)
		if err != nil {
			if attempt < maxRetries {
				delay := baseDelay * time.Duration(1<<attempt) // Exponential backoff
				fmt.Printf("DEBUG: API request failed (attempt %d/%d), retrying in %v: %v\n",
					attempt+1, maxRetries+1, delay, err)
				time.Sleep(delay)
				continue
			}
			return nil, fmt.Errorf("failed to generate embeddings after %d attempts: %w", maxRetries+1, err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)

			// Check if this is a retryable error
			if e.isRetryableError(resp.StatusCode) && attempt < maxRetries {
				delay := baseDelay * time.Duration(1<<attempt) // Exponential backoff
				fmt.Printf("DEBUG: API returned retryable error %d (attempt %d/%d), retrying in %v: %s\n",
					resp.StatusCode, attempt+1, maxRetries+1, delay, string(body))
				time.Sleep(delay)
				continue
			}

			return nil, fmt.Errorf("failed to generate embeddings (status %d): %s", resp.StatusCode, string(body))
		}

		var embeddingResp OpenAIEmbeddingResponse
		if err := json.NewDecoder(resp.Body).Decode(&embeddingResp); err != nil {
			if attempt < maxRetries {
				delay := baseDelay * time.Duration(1<<attempt)
				fmt.Printf("DEBUG: Failed to decode response (attempt %d/%d), retrying in %v: %v\n",
					attempt+1, maxRetries+1, delay, err)
				time.Sleep(delay)
				continue
			}
			return nil, fmt.Errorf("failed to decode response after %d attempts: %w", maxRetries+1, err)
		}

		if len(embeddingResp.Data) == 0 {
			return nil, fmt.Errorf("no embedding data in response")
		}

		return embeddingResp.Data[0].Embedding, nil
	}

	return nil, fmt.Errorf("unexpected error: retry loop completed without result")
}

// isRetryableError checks if an HTTP status code indicates a retryable error
func (e *EmbeddingService) isRetryableError(statusCode int) bool {
	switch statusCode {
	case 429: // Too Many Requests
		return true
	case 500: // Internal Server Error
		return true
	case 502: // Bad Gateway
		return true
	case 503: // Service Unavailable
		return true
	case 504: // Gateway Timeout
		return true
	default:
		return false
	}
}

// GetModelInfo returns information about the embedding model
func (e *EmbeddingService) GetModelInfo() map[string]interface{} {
	return map[string]interface{}{
		"model":     e.model,
		"provider":  "openai",
		"enabled":   e.enabled,
		"available": e.IsAvailable(),
	}
}

// SetEnabled enables or disables the embedding service
func (e *EmbeddingService) SetEnabled(enabled bool) {
	e.enabled = enabled
}

// GetEmbeddingDimension returns the expected dimension of embeddings
func (e *EmbeddingService) GetEmbeddingDimension() int {
	// text-embedding-3-small produces 1536-dimensional embeddings
	// text-embedding-3-large produces 3072-dimensional embeddings
	switch e.model {
	case "text-embedding-3-small":
		return 1536
	case "text-embedding-3-large":
		return 3072
	case "text-embedding-ada-002":
		return 1536
	default:
		return 1536 // Default for text-embedding-3-small
	}
}

// BatchGenerateEmbeddings generates embeddings for texts in batches
func (e *EmbeddingService) BatchGenerateEmbeddings(texts []string, batchSize int) ([][]float32, error) {
	if batchSize <= 0 {
		batchSize = 10 // Default batch size
	}

	fmt.Printf("DEBUG: BatchGenerateEmbeddings called with %d texts, batch size %d\n", len(texts), batchSize)
	var allEmbeddings [][]float32

	for i := 0; i < len(texts); i += batchSize {
		end := i + batchSize
		if end > len(texts) {
			end = len(texts)
		}

		batch := texts[i:end]
		fmt.Printf("DEBUG: Processing batch %d-%d (%d texts)\n", i, end-1, len(batch))

		startTime := time.Now()
		embeddings, err := e.GenerateEmbeddings(batch)
		batchTime := time.Since(startTime)

		if err != nil {
			fmt.Printf("DEBUG: Batch %d-%d failed after %v: %v\n", i, end-1, batchTime, err)
			return nil, fmt.Errorf("failed to generate embeddings for batch %d-%d: %w", i, end-1, err)
		}

		fmt.Printf("DEBUG: Batch %d-%d completed in %v, generated %d embeddings\n", i, end-1, batchTime, len(embeddings))
		allEmbeddings = append(allEmbeddings, embeddings...)
	}

	fmt.Printf("DEBUG: BatchGenerateEmbeddings completed, total embeddings: %d\n", len(allEmbeddings))
	return allEmbeddings, nil
}
