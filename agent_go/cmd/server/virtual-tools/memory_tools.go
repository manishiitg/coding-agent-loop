package virtualtools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"mcp-agent/agent_go/internal/llmtypes"
)

// MemoryAPIResponse represents the response structure from the memory API
type MemoryAPIResponse struct {
	Success bool        `json:"success"`
	Message string      `json:"message"`
	Data    interface{} `json:"data"`
}

// AddMemoryRequest represents the request structure for adding memory
type AddMemoryRequest struct {
	Name              string `json:"name"`
	Content           string `json:"content"`
	SourceType        string `json:"source_type,omitempty"`
	SourceDescription string `json:"source_description,omitempty"`
}

// SearchFactsRequest represents the request structure for searching facts
type SearchFactsRequest struct {
	Query string `json:"query"`
	Limit int    `json:"limit,omitempty"`
}

// DeleteEpisodeRequest represents the request structure for deleting episodes
type DeleteEpisodeRequest struct {
	EpisodeUUID string `json:"episode_uuid"`
}

// CreateMemoryTools creates memory tools for React agents
func CreateMemoryTools() []llmtypes.Tool {
	var memoryTools []llmtypes.Tool

	// Add add_memory tool
	addMemoryTool := llmtypes.Tool{
		Type: "function",
		Function: &llmtypes.FunctionDefinition{
			Name:        "add_memory",
			Description: "Store important information in knowledge graph for future reference.",
			Parameters: llmtypes.NewParameters(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name": map[string]interface{}{
						"type":        "string",
						"description": "Memory title",
					},
					"content": map[string]interface{}{
						"type":        "string",
						"description": "Content to store",
					},
					"source_type": map[string]interface{}{
						"type":        "string",
						"description": "Source type: 'text' or 'json' (default: 'text')",
						"enum":        []string{"text", "json"},
					},
					"source_description": map[string]interface{}{
						"type":        "string",
						"description": "Source description",
					},
				},
				"required": []string{"name", "content", "source_description"},
			}),
		},
	}
	memoryTools = append(memoryTools, addMemoryTool)

	// Add search_episodes tool
	searchEpisodesTool := llmtypes.Tool{
		Type: "function",
		Function: &llmtypes.FunctionDefinition{
			Name:        "search_memory",
			Description: "Search knowledge graph for relevant past information and context.",
			Parameters: llmtypes.NewParameters(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"query": map[string]interface{}{
						"type":        "string",
						"description": "Search query",
					},
					"limit": map[string]interface{}{
						"type":        "integer",
						"description": "Max results (default: 10)",
						"minimum":     1,
						"maximum":     50,
					},
				},
				"required": []string{"query"},
			}),
		},
	}
	memoryTools = append(memoryTools, searchEpisodesTool)

	// Add delete_memory tool
	deleteMemoryTool := llmtypes.Tool{
		Type: "function",
		Function: &llmtypes.FunctionDefinition{
			Name:        "delete_memory",
			Description: "Delete outdated or incorrect memories from the knowledge graph.",
			Parameters: llmtypes.NewParameters(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"episode_uuid": map[string]interface{}{
						"type":        "string",
						"description": "UUID of the memory/episode to delete",
					},
					"confirmation": map[string]interface{}{
						"type":        "boolean",
						"description": "Confirmation that you want to delete this memory (default: false)",
					},
				},
				"required": []string{"episode_uuid"},
			}),
		},
	}
	memoryTools = append(memoryTools, deleteMemoryTool)

	return memoryTools
}

// CreateMemoryToolExecutors creates the execution functions for memory tools
func CreateMemoryToolExecutors() map[string]func(ctx context.Context, args map[string]interface{}) (string, error) {
	executors := make(map[string]func(ctx context.Context, args map[string]interface{}) (string, error))

	executors["add_memory"] = handleAddMemory
	executors["search_memory"] = handleSearchEpisodes
	executors["delete_memory"] = handleDeleteMemory

	return executors
}

// handleAddMemory handles the add_memory tool execution
func handleAddMemory(ctx context.Context, args map[string]interface{}) (string, error) {
	// Get memory API URL from environment or use default
	memoryAPIURL := os.Getenv("MEMORY_API_URL")
	if memoryAPIURL == "" {
		memoryAPIURL = "http://localhost:8000"
	}

	// Extract and validate arguments
	name, ok := args["name"].(string)
	if !ok || name == "" {
		return "", fmt.Errorf("name is required and must be a string")
	}

	content, ok := args["content"].(string)
	if !ok || content == "" {
		return "", fmt.Errorf("content is required and must be a string")
	}

	// Set defaults for optional fields
	sourceType := "text"
	if st, ok := args["source_type"].(string); ok && st != "" {
		sourceType = st
	}

	sourceDescription := ""
	if sd, ok := args["source_description"].(string); ok {
		sourceDescription = sd
	}

	// Set default if not provided
	if sourceDescription == "" {
		sourceDescription = "MCP Agent Memory Tool"
	}

	// Create request
	req := AddMemoryRequest{
		Name:              name,
		Content:           content,
		SourceType:        sourceType,
		SourceDescription: sourceDescription,
	}

	// Make HTTP request
	response, err := makeMemoryAPIRequest(ctx, memoryAPIURL+"/add_memory", "POST", req)
	if err != nil {
		return "", fmt.Errorf("failed to add memory: %w", err)
	}

	// Parse response
	var apiResp MemoryAPIResponse
	if err := json.Unmarshal(response, &apiResp); err != nil {
		return "", fmt.Errorf("failed to parse memory API response: %w", err)
	}

	if !apiResp.Success {
		return "", fmt.Errorf("memory API error: %s", apiResp.Message)
	}

	// Extract episode UUID from response data
	if data, ok := apiResp.Data.(map[string]interface{}); ok {
		if episodeUUID, exists := data["episode_uuid"]; exists {
			return fmt.Sprintf("✅ Memory stored successfully!\n\n**Episode:** %s\n**UUID:** %s\n**Message:** %s",
				name, episodeUUID, apiResp.Message), nil
		}
	}

	return fmt.Sprintf("✅ Memory stored successfully!\n\n**Episode:** %s\n**Message:** %s", name, apiResp.Message), nil
}

// handleSearchEpisodes handles the search_episodes tool execution
func handleSearchEpisodes(ctx context.Context, args map[string]interface{}) (string, error) {
	// Get memory API URL from environment or use default
	memoryAPIURL := os.Getenv("MEMORY_API_URL")
	if memoryAPIURL == "" {
		memoryAPIURL = "http://localhost:8000"
	}

	// Extract and validate arguments
	query, ok := args["query"].(string)
	if !ok || query == "" {
		return "", fmt.Errorf("query is required and must be a string")
	}

	// Set default limit
	limit := 10
	if l, ok := args["limit"].(float64); ok && l > 0 {
		limit = int(l)
	}

	// Create request
	req := SearchFactsRequest{
		Query: query,
		Limit: limit,
	}

	// Make HTTP request
	response, err := makeMemoryAPIRequest(ctx, memoryAPIURL+"/search_facts", "POST", req)
	if err != nil {
		return "", fmt.Errorf("failed to search episodes: %w", err)
	}

	// Parse response
	var apiResp MemoryAPIResponse
	if err := json.Unmarshal(response, &apiResp); err != nil {
		return "", fmt.Errorf("failed to parse memory API response: %w", err)
	}

	if !apiResp.Success {
		return "", fmt.Errorf("memory API error: %s", apiResp.Message)
	}

	// Extract facts from response data
	if data, ok := apiResp.Data.(map[string]interface{}); ok {
		if facts, exists := data["facts"].([]interface{}); exists {
			if len(facts) == 0 {
				return fmt.Sprintf("🔍 **Search Results for:** %s\n\nNo relevant memories found. Try a different search query or add some memories first.", query), nil
			}

			// Format facts for display
			var result strings.Builder
			result.WriteString(fmt.Sprintf("🔍 **Search Results for:** %s\n\n", query))
			result.WriteString(fmt.Sprintf("Found %d relevant memories:\n\n", len(facts)))

			for i, fact := range facts {
				if factMap, ok := fact.(map[string]interface{}); ok {
					factText := ""
					if f, exists := factMap["fact"].(string); exists {
						factText = f
					}

					validFrom := ""
					if vf, exists := factMap["valid_from"].(string); exists && vf != "" {
						validFrom = fmt.Sprintf(" (from %s)", vf)
					}

					result.WriteString(fmt.Sprintf("%d. **%s**%s\n", i+1, factText, validFrom))
				}
			}

			return result.String(), nil
		}
	}

	return fmt.Sprintf("🔍 **Search Results for:** %s\n\nNo structured facts found in response.", query), nil
}

// handleDeleteMemory handles the delete_memory tool execution
func handleDeleteMemory(ctx context.Context, args map[string]interface{}) (string, error) {
	// Get memory API URL from environment or use default
	memoryAPIURL := os.Getenv("MEMORY_API_URL")
	if memoryAPIURL == "" {
		memoryAPIURL = "http://localhost:8000"
	}

	// Extract and validate arguments
	episodeUUID, ok := args["episode_uuid"].(string)
	if !ok || episodeUUID == "" {
		return "", fmt.Errorf("episode_uuid is required and must be a string")
	}

	// Check confirmation (optional, defaults to false)
	confirmation := false
	if conf, ok := args["confirmation"].(bool); ok {
		confirmation = conf
	}

	// Require explicit confirmation for safety
	if !confirmation {
		return "❌ **Deletion cancelled** - Please set confirmation=true to confirm deletion of this memory.", nil
	}

	// Create request
	req := DeleteEpisodeRequest{
		EpisodeUUID: episodeUUID,
	}

	// Make HTTP request
	response, err := makeMemoryAPIRequest(ctx, memoryAPIURL+"/delete_episode", "POST", req)
	if err != nil {
		return "", fmt.Errorf("failed to delete memory: %w", err)
	}

	// Parse response
	var apiResp MemoryAPIResponse
	if err := json.Unmarshal(response, &apiResp); err != nil {
		return "", fmt.Errorf("failed to parse memory API response: %w", err)
	}

	if !apiResp.Success {
		return "", fmt.Errorf("memory API error: %s", apiResp.Message)
	}

	// Extract deleted UUID from response data
	if data, ok := apiResp.Data.(map[string]interface{}); ok {
		if deletedUUID, exists := data["deleted_uuid"]; exists {
			return fmt.Sprintf("✅ **Memory deleted successfully!**\n\n**Deleted UUID:** %s\n**Message:** %s",
				deletedUUID, apiResp.Message), nil
		}
	}

	return fmt.Sprintf("✅ **Memory deleted successfully!**\n\n**Message:** %s", apiResp.Message), nil
}

// makeMemoryAPIRequest makes an HTTP request to the memory API
func makeMemoryAPIRequest(ctx context.Context, url, method string, payload interface{}) ([]byte, error) {
	// Create HTTP client with timeout
	client := &http.Client{
		Timeout: 5 * time.Minute,
	}

	// Marshal payload to JSON
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Create request
	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	// Make request
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	// Read response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Check status code
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("memory API returned status %d: %s", resp.StatusCode, string(body))
	}

	return body, nil
}
