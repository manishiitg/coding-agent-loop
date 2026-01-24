package virtualtools

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	md "github.com/JohannesKaufmann/html-to-markdown"
	"github.com/ledongthuc/pdf"
	"github.com/manishiitg/multi-llm-provider-go/llmtypes"

	"github.com/manishiitg/mcpagent/events"
	"workspace/models"

	"golang.org/x/image/draw"
)

// WorkspaceEventEmitter interface for emitting workspace file operation events
type WorkspaceEventEmitter interface {
	HandleEvent(ctx context.Context, event *events.AgentEvent) error
}

// Context keys for workspace event emission and folder guard paths
type contextKey string

const (
	// WorkspaceEventEmitterKey is the context key for the workspace event emitter
	WorkspaceEventEmitterKey contextKey = "workspace_event_emitter"
	// TurnKey is the context key for the turn number
	TurnKey contextKey = "turn"
	// ServerNameKey is the context key for the server name
	ServerNameKey contextKey = "server_name"
	// FolderGuardReadPathsKey is the context key for folder guard read paths
	FolderGuardReadPathsKey contextKey = "folder_guard_read_paths"
	// FolderGuardWritePathsKey is the context key for folder guard write paths
	FolderGuardWritePathsKey contextKey = "folder_guard_write_paths"
	// FolderGuardBlockedPathsKey is the context key for blocked paths (deny list)
	FolderGuardBlockedPathsKey contextKey = "folder_guard_blocked_paths"
	// FolderGuardAllowedWriteFolderKey is the context key for the only folder allowed for writes (chat mode)
	FolderGuardAllowedWriteFolderKey contextKey = "folder_guard_allowed_write_folder"
)

// Legacy constants for backward compatibility (use exported versions)
var (
	workspaceEventEmitterKey = WorkspaceEventEmitterKey
	turnKey                  = TurnKey
	serverNameKey            = ServerNameKey
)

// getEventEmitterFromContext extracts the event emitter from context
// Tries multiple key types to handle different packages using their own contextKey types
// Also checks generic tool execution keys injected by the agent
func getEventEmitterFromContext(ctx context.Context) WorkspaceEventEmitter {
	// Try with our typed key first (for orchestrator direct calls)
	if emitter, ok := ctx.Value(workspaceEventEmitterKey).(WorkspaceEventEmitter); ok {
		return emitter
	}
	// Try with string key (for backward compatibility and cross-package compatibility)
	if emitter, ok := ctx.Value("workspace_event_emitter").(WorkspaceEventEmitter); ok {
		return emitter
	}
	// Try generic tool execution agent key (injected by agent for all tool calls)
	// The agent doesn't know about workspace - it just injects generic tool metadata
	// Note: The agent from mcpagent package implements HandleEvent, but type assertion
	// across modules might fail, so we use an interface check instead
	if agentVal := ctx.Value("tool_execution_agent"); agentVal != nil {
		// Check if agent implements HandleEvent with the correct signature
		// Use interface{} type assertion to work across module boundaries
		if agentWithHandleEvent, ok := agentVal.(interface {
			HandleEvent(ctx context.Context, event *events.AgentEvent) error
		}); ok {
			// Wrap it to satisfy WorkspaceEventEmitter interface
			return &agentEventEmitterWrapper{agent: agentWithHandleEvent}
		}
	}
	// Try with any contextKey type that has the same string value
	// This handles cases where other packages define their own contextKey types
	if val := ctx.Value("workspace_event_emitter"); val != nil {
		if emitter, ok := val.(WorkspaceEventEmitter); ok {
			return emitter
		}
	}
	return nil
}

// agentEventEmitterWrapper wraps an agent that has HandleEvent but isn't recognized as WorkspaceEventEmitter
// This bridges the gap when the agent is from a different module (mcpagent) and type assertions fail
type agentEventEmitterWrapper struct {
	agent interface {
		HandleEvent(ctx context.Context, event *events.AgentEvent) error
	}
}

func (w *agentEventEmitterWrapper) HandleEvent(ctx context.Context, event *events.AgentEvent) error {
	return w.agent.HandleEvent(ctx, event)
}

// getTurnFromContext extracts the turn number from context
// Tries multiple key types to handle different packages using their own contextKey types
// Also checks generic tool execution keys injected by the agent
func getTurnFromContext(ctx context.Context) int {
	// Try with our typed key first (for orchestrator direct calls)
	if turn, ok := ctx.Value(turnKey).(int); ok {
		return turn
	}
	// Try with string key (for backward compatibility and cross-package compatibility)
	if turn, ok := ctx.Value("turn").(int); ok {
		return turn
	}
	// Try generic tool execution turn key (injected by agent for all tool calls)
	if turn, ok := ctx.Value("tool_execution_turn").(int); ok {
		return turn
	}
	// Try with any contextKey type that has the same string value
	if val := ctx.Value("turn"); val != nil {
		if turn, ok := val.(int); ok {
			return turn
		}
	}
	return 0
}

// getServerNameFromContext extracts the server name from context
// Tries multiple key types to handle different packages using their own contextKey types
// Also checks generic tool execution keys injected by the agent
func getServerNameFromContext(ctx context.Context) string {
	// Try with our typed key first (for orchestrator direct calls)
	if serverName, ok := ctx.Value(serverNameKey).(string); ok {
		return serverName
	}
	// Try with string key (for backward compatibility and cross-package compatibility)
	if serverName, ok := ctx.Value("server_name").(string); ok {
		return serverName
	}
	// Try generic tool execution server key (injected by agent for all tool calls)
	if serverName, ok := ctx.Value("tool_execution_server").(string); ok {
		return serverName
	}
	// Try with any contextKey type that has the same string value
	if val := ctx.Value("server_name"); val != nil {
		if serverName, ok := val.(string); ok {
			return serverName
		}
	}
	return ""
}

// emitWorkspaceFileOperation emits a workspace file operation event
func emitWorkspaceFileOperation(ctx context.Context, operation, filepath, folder string) {
	// Debug: Check what's in context
	// if agentVal := ctx.Value("tool_execution_agent"); agentVal != nil {
	// 	fmt.Printf("[WorkspaceTools] emitWorkspaceFileOperation: Found agent in context, type: %T\n", agentVal)
	// } else {
	// 	fmt.Printf("[WorkspaceTools] emitWorkspaceFileOperation: No agent found with key 'tool_execution_agent'\n")
	// }

	emitter := getEventEmitterFromContext(ctx)
	if emitter == nil {
		// No emitter in context - this is expected for some orchestrator direct calls
		// fmt.Printf("[WorkspaceTools] emitWorkspaceFileOperation: No emitter in context for operation=%s, filepath=%s, folder=%s\n", operation, filepath, folder)
		return
	}

	// fmt.Printf("[WorkspaceTools] emitWorkspaceFileOperation: Found emitter, type: %T\n", emitter)

	turn := getTurnFromContext(ctx)
	serverName := getServerNameFromContext(ctx)

	// Determine if this file should be highlighted in the UI
	// Exclude logs/ folder and its subfolders from highlighting
	shouldHighlight := true
	if filepath != "" {
		// Check if filepath contains logs/ (at start or as subfolder)
		if strings.HasPrefix(filepath, "logs/") || strings.Contains(filepath, "/logs/") {
			shouldHighlight = false
		}
	}
	if folder != "" {
		// Check if folder path contains logs/
		if strings.HasPrefix(folder, "logs/") || strings.Contains(folder, "/logs/") {
			shouldHighlight = false
		}
	}

	// fmt.Printf("[WorkspaceTools] emitWorkspaceFileOperation: Emitting event operation=%s, filepath=%s, folder=%s, turn=%d, serverName=%s, shouldHighlight=%v\n",
	// 	operation, filepath, folder, turn, serverName, shouldHighlight)

	eventData := events.NewWorkspaceFileOperationEvent(operation, filepath, folder, turn, serverName, shouldHighlight)
	agentEvent := &events.AgentEvent{
		Type:      events.WorkspaceFileOperation,
		Timestamp: eventData.Timestamp,
		Data:      eventData,
	}

	if err := emitter.HandleEvent(ctx, agentEvent); err != nil {
		// Log error but don't break tool execution
		fmt.Printf("[WorkspaceTools] emitWorkspaceFileOperation: Error emitting event: %v\n", err)
	} else {
		// fmt.Printf("[WorkspaceTools] emitWorkspaceFileOperation: Successfully emitted event\n")
	}
}

// Type aliases for workspace models - using proper types from workspace/models package
type (
	// WorkspaceAPIResponse uses the generic API response from workspace models
	// Note: We use interface{} for Data since we need to handle various response types
	WorkspaceAPIResponse = models.APIResponse[interface{}]

	// WorkspaceDocument is an alias for workspace Document type
	WorkspaceDocument = models.Document

	// WorkspaceFile is an alias for workspace Document type (used for file listings)
	WorkspaceFile = models.Document

	// WorkspaceFileContent is an alias for workspace Document type (used for file content)
	WorkspaceFileContent = models.Document

	// WorkspaceFolderItem is an alias for workspace Document type (used for folder items)
	WorkspaceFolderItem = models.Document

	// WorkspaceFolderListing is an alias for []models.Document (folder listing response)
	WorkspaceFolderListing = []models.Document

	// WorkspaceSearchResult is an alias for workspace SearchResult type
	WorkspaceSearchResult = models.SearchResult

	// WorkspaceSearchResponse is an alias for workspace SearchResponse type
	WorkspaceSearchResponse = models.SearchResponse
)

// getWorkspaceAPIURL returns the workspace API base URL from environment or default
func getWorkspaceAPIURL() string {
	if url := os.Getenv("WORKSPACE_API_URL"); url != "" {
		return url
	}
	return "http://localhost:8081"
}

// isSemanticSearchEnabled checks if semantic search is enabled via environment variable
// Returns true only if WORKSPACE_ENABLE_SEMANTIC_SEARCH is explicitly set to "true" or "1"
func isSemanticSearchEnabled() bool {
	val := os.Getenv("WORKSPACE_ENABLE_SEMANTIC_SEARCH")
	return strings.ToLower(val) == "true" || val == "1"
}

// isImageFile checks if a file is an image based on its extension
func isImageFile(filepathStr string) bool {
	ext := strings.ToLower(filepath.Ext(filepathStr))
	switch ext {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp":
		return true
	default:
		return false
	}
}

// getMimeTypeFromExtension returns the MIME type for an image file extension
func getMimeTypeFromExtension(ext string) string {
	ext = strings.ToLower(ext)
	switch ext {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	default:
		return "image/jpeg" // Default fallback
	}
}

// resizeImage resizes an image to a maximum dimension while preserving aspect ratio
// maxDimension: maximum width or height (e.g., 1024)
// Returns resized image bytes and the MIME type, or original if no resize needed
func resizeImage(imageData []byte, mimeType string, maxDimension int) ([]byte, string, error) {
	// Decode image
	img, format, err := image.Decode(bytes.NewReader(imageData))
	if err != nil {
		return nil, "", fmt.Errorf("failed to decode image: %w", err)
	}

	// Get original dimensions
	bounds := img.Bounds()
	origWidth := bounds.Dx()
	origHeight := bounds.Dy()

	// Check if resize is needed
	if origWidth <= maxDimension && origHeight <= maxDimension {
		// No resize needed
		return imageData, mimeType, nil
	}

	// Calculate new dimensions preserving aspect ratio
	var newWidth, newHeight int
	if origWidth > origHeight {
		// Landscape: width is the limiting factor
		newWidth = maxDimension
		newHeight = int((int64(origHeight) * int64(maxDimension)) / int64(origWidth))
	} else {
		// Portrait or square: height is the limiting factor
		newHeight = maxDimension
		newWidth = int((int64(origWidth) * int64(maxDimension)) / int64(origHeight))
	}

	// Create new image with calculated dimensions
	resized := image.NewRGBA(image.Rect(0, 0, newWidth, newHeight))
	draw.BiLinear.Scale(resized, resized.Bounds(), img, bounds, draw.Over, nil)

	// Encode resized image
	var buf bytes.Buffer
	switch format {
	case "jpeg", "jpg":
		if err := jpeg.Encode(&buf, resized, &jpeg.Options{Quality: 85}); err != nil {
			return nil, "", fmt.Errorf("failed to encode JPEG: %w", err)
		}
		return buf.Bytes(), "image/jpeg", nil
	case "png":
		if err := png.Encode(&buf, resized); err != nil {
			return nil, "", fmt.Errorf("failed to encode PNG: %w", err)
		}
		return buf.Bytes(), "image/png", nil
	case "gif":
		// GIF resizing is more complex, skip for now and return original
		// TODO: Implement GIF resizing if needed
		return imageData, mimeType, nil
	default:
		// For unsupported formats (like WebP), return original
		// WebP requires external library, so we skip resizing for now
		return imageData, mimeType, nil
	}
}

// parseWorkspaceFileData extracts filepath and content from workspace API response data
func parseWorkspaceFileData(data interface{}) (filepath, content string, err error) {
	dataMap, ok := data.(map[string]interface{})
	if !ok {
		return "", "", fmt.Errorf("unexpected response format from workspace API - expected object, got %T", data)
	}

	filepath = getStringValue(dataMap, "filepath")
	content = getStringValue(dataMap, "content")

	if filepath == "" {
		return "", "", fmt.Errorf("filepath not found in workspace API response")
	}

	return filepath, content, nil
}

// CreateWorkspaceTools creates all workspace-related virtual tools (basic + git + advanced)
// This is the backward-compatible function that returns all 15 tools
func CreateWorkspaceTools() []llmtypes.Tool {
	// Combine basic, git, and advanced tools for backward compatibility
	tools := CreateWorkspaceBasicTools()
	tools = append(tools, CreateWorkspaceGitTools()...)
	tools = append(tools, CreateWorkspaceAdvancedTools()...)
	return tools
}

// GetWorkspaceToolCategory returns the category name for all workspace tools (backward compatible)
func GetWorkspaceToolCategory() string {
	return "workspace_tools"
}

// CreateWorkspaceToolExecutors creates the execution functions for all workspace tools (basic + advanced)
// This is the backward-compatible function that returns all executors
func CreateWorkspaceToolExecutors() map[string]func(ctx context.Context, args map[string]interface{}) (string, error) {
	// Combine basic, git, and advanced executors for backward compatibility
	executors := CreateWorkspaceBasicToolExecutors()
	for k, v := range CreateWorkspaceGitToolExecutors() {
		executors[k] = v
	}
	for k, v := range CreateWorkspaceAdvancedToolExecutors() {
		executors[k] = v
	}
	return executors
}

// handleListWorkspaceFiles handles the list_workspace_files tool execution
func handleListWorkspaceFiles(ctx context.Context, args map[string]interface{}) (string, error) {
	// Extract parameters
	folder, ok := args["folder"].(string)
	if !ok || folder == "" {
		return "", fmt.Errorf("folder is required and must be a string")
	}

	maxDepth := 3
	if d, ok := args["max_depth"].(float64); ok {
		maxDepth = int(d)
		if maxDepth > 10 {
			maxDepth = 10
		}
		if maxDepth < 1 {
			maxDepth = 1
		}
	}

	// Extract blocked paths from context (for folder guard)
	var blockedPaths []string
	if bp := ctx.Value(FolderGuardBlockedPathsKey); bp != nil {
		if paths, ok := bp.([]string); ok {
			blockedPaths = paths
			// fmt.Printf("[FOLDER GUARD DEBUG] list_workspace_files extracted blocked paths from context: %v\n", blockedPaths)
		}
	} else {
		// fmt.Printf("[FOLDER GUARD DEBUG] list_workspace_files no blocked paths in context\n")
	}

	// Build API URL
	apiURL := getWorkspaceAPIURL() + "/api/documents"

	// Create HTTP request with context
	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	// Add query parameters
	q := req.URL.Query()
	q.Add("folder", folder)
	q.Add("max_depth", fmt.Sprintf("%d", maxDepth))
	// Add blocked paths if present (for folder guard)
	if len(blockedPaths) > 0 {
		q.Add("blocked_paths", strings.Join(blockedPaths, ","))
	}
	req.URL.RawQuery = q.Encode()

	// Debug logging
	// fmt.Printf("[DEBUG list_workspace_files] Requesting folder: %s, max_depth: %d, URL: %s\n", folder, maxDepth, req.URL.String())

	// Set timeout
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	// Make the request
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to call workspace API: %w", err)
	}
	defer resp.Body.Close()

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	// Check HTTP status
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("workspace API returned status %d: %s", resp.StatusCode, string(body))
	}

	// Parse JSON response
	var apiResp WorkspaceAPIResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return "", fmt.Errorf("failed to parse API response: %w", err)
	}

	// Check API response success
	if !apiResp.Success {
		return "", fmt.Errorf("workspace API error: %s", apiResp.Error)
	}

	// Check if folder doesn't exist (API returns Success: true but with error message)
	if strings.Contains(apiResp.Message, "Folder does not exist") ||
		strings.Contains(apiResp.Error, "Folder not found") {
		if folder == "" {
			return "", fmt.Errorf("folder is empty or does not exist")
		}
		return "", fmt.Errorf("folder does not exist: %s", folder)
	}

	// Debug logging for troubleshooting
	if apiResp.Data == nil {
		// fmt.Printf("[DEBUG] Workspace API returned nil data for folder: %s, maxDepth: %d\n", folder, maxDepth)
	}

	// Note: Blocked paths filtering is now handled by the Workspace API
	// The blocked_paths query parameter was passed above, so results are already filtered

	// Marshal the response data
	responseData, err := json.Marshal(apiResp.Data)
	if err != nil {
		return "", fmt.Errorf("failed to marshal API response: %w", err)
	}

	// Check if data is an empty array (folder exists but is empty)
	// Only check if we didn't already detect a non-existent folder
	if string(responseData) == "[]" && apiResp.Error == "" && !strings.Contains(apiResp.Message, "Folder does not exist") {
		// Folder exists but is empty - return a message indicating this
		return fmt.Sprintf("Folder '%s' exists but contains no files or subfolders.", folder), nil
	}

	// Emit workspace file operation event for list operation
	emitWorkspaceFileOperation(ctx, "list", "", folder)

	return string(responseData), nil
}

// handleReadWorkspaceFile handles the read_workspace_file tool execution
func handleReadWorkspaceFile(ctx context.Context, args map[string]interface{}) (string, error) {
	// Extract filepath parameter
	filepathStr, ok := args["filepath"].(string)
	if !ok || filepathStr == "" {
		return "", fmt.Errorf("filepath is required and must be a string")
	}

	// URL-encode the filepath segments
	pathSegments := strings.Split(filepathStr, "/")
	encodedSegments := make([]string, len(pathSegments))
	for i, segment := range pathSegments {
		encodedSegments[i] = url.PathEscape(segment)
	}
	encodedPath := strings.Join(encodedSegments, "/")

	// Build API URL with encoded path
	apiURL := getWorkspaceAPIURL() + "/api/documents/" + encodedPath

	// Create HTTP request with context
	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	// Set timeout
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	// Make the request
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to call workspace API: %w", err)
	}
	defer resp.Body.Close()

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	// Check HTTP status
	if resp.StatusCode != http.StatusOK {
		// Check if it's a 404 with "Document not found" message
		if resp.StatusCode == http.StatusNotFound {
			var apiResp WorkspaceAPIResponse
			if err := json.Unmarshal(body, &apiResp); err == nil {
				// Check if the error message indicates document not found
				if strings.Contains(apiResp.Message, "Document not found") || strings.Contains(apiResp.Error, "Document not found") {
					return "", fmt.Errorf("workspace API returned status %d: %s. Use the 'list_workspace_files' tool to find the correct file path", resp.StatusCode, string(body))
				}
			}
		}
		return "", fmt.Errorf("workspace API returned status %d: %s", resp.StatusCode, string(body))
	}

	// Parse JSON response
	var apiResp WorkspaceAPIResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return "", fmt.Errorf("failed to parse API response: %w", err)
	}

	// Check API response success
	if !apiResp.Success {
		// Check if it's a "Document not found" error
		if strings.Contains(apiResp.Message, "Document not found") || strings.Contains(apiResp.Error, "Document not found") {
			return "", fmt.Errorf("workspace API error: %s. Use the 'list_workspace_files' tool to find the correct file path", apiResp.Error)
		}
		return "", fmt.Errorf("workspace API error: %s", apiResp.Error)
	}

	// Check if file doesn't exist (API returns Success: true but with error message)
	if strings.Contains(apiResp.Message, "File does not exist") ||
		strings.Contains(apiResp.Error, "File not found") {
		return "", fmt.Errorf("file does not exist: %s. Use the 'list_workspace_files' tool to find the correct file path", filepathStr)
	}

	// Check if file is an image - if so, inform LLM to use read_image tool
	if isImageFile(filepathStr) {
		return "", fmt.Errorf("this file is an image (%s). Please use the 'read_image' tool instead to read and analyze image files. The read_image tool requires both 'filepath' and 'query' parameters", filepathStr)
	}

	// For non-image files, return the raw API response directly
	responseData, err := json.Marshal(apiResp.Data)
	if err != nil {
		return "", fmt.Errorf("failed to marshal API response: %w", err)
	}

	// Emit workspace file operation event for read operation
	emitWorkspaceFileOperation(ctx, "read", filepathStr, "")

	return string(responseData), nil
}

// handleReadImage handles the read_image tool execution
func handleReadImage(ctx context.Context, args map[string]interface{}) (string, error) {
	// Extract parameters
	filepathStr, ok := args["filepath"].(string)
	if !ok || filepathStr == "" {
		return "", fmt.Errorf("filepath is required and must be a string")
	}

	query, ok := args["query"].(string)
	if !ok || query == "" {
		return "", fmt.Errorf("query is required and must be a string")
	}

	// Check if file is an image
	if !isImageFile(filepathStr) {
		return "", fmt.Errorf("file is not an image: %s (supported formats: jpg, jpeg, png, gif, webp)", filepathStr)
	}

	// URL-encode the filepath segments
	pathSegments := strings.Split(filepathStr, "/")
	encodedSegments := make([]string, len(pathSegments))
	for i, segment := range pathSegments {
		encodedSegments[i] = url.PathEscape(segment)
	}
	encodedPath := strings.Join(encodedSegments, "/")

	// Build API URL with encoded path
	apiURL := getWorkspaceAPIURL() + "/api/documents/" + encodedPath

	// Create HTTP request with context
	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	// Set timeout
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	// Make the request
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to call workspace API: %w", err)
	}
	defer resp.Body.Close()

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	// Check HTTP status
	if resp.StatusCode != http.StatusOK {
		// Check if it's a 404 with "Document not found" message
		if resp.StatusCode == http.StatusNotFound {
			var apiResp WorkspaceAPIResponse
			if err := json.Unmarshal(body, &apiResp); err == nil {
				// Check if the error message indicates document not found
				if strings.Contains(apiResp.Message, "Document not found") || strings.Contains(apiResp.Error, "Document not found") {
					return "", fmt.Errorf("workspace API returned status %d: %s. Use the 'list_workspace_files' tool to find the correct file path", resp.StatusCode, string(body))
				}
			}
		}
		return "", fmt.Errorf("workspace API returned status %d: %s", resp.StatusCode, string(body))
	}

	// Parse JSON response
	var apiResp WorkspaceAPIResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return "", fmt.Errorf("failed to parse API response: %w", err)
	}

	// Check API response success
	if !apiResp.Success {
		// Check if it's a "Document not found" error
		if strings.Contains(apiResp.Message, "Document not found") || strings.Contains(apiResp.Error, "Document not found") {
			return "", fmt.Errorf("workspace API error: %s. Use the 'list_workspace_files' tool to find the correct file path", apiResp.Error)
		}
		return "", fmt.Errorf("workspace API error: %s", apiResp.Error)
	}

	// Check if file doesn't exist (API returns Success: true but with error message)
	if strings.Contains(apiResp.Message, "File does not exist") ||
		strings.Contains(apiResp.Error, "File not found") {
		return "", fmt.Errorf("file does not exist: %s. Use the 'list_workspace_files' tool to find the correct file path", filepathStr)
	}

	// Parse workspace file data
	_, content, err := parseWorkspaceFileData(apiResp.Data)
	if err != nil {
		return "", fmt.Errorf("failed to parse workspace file data: %w", err)
	}

	if content == "" {
		return "", fmt.Errorf("no content found in workspace response")
	}

	// Handle base64 encoding and extract raw image data
	var imageBytes []byte
	if strings.HasPrefix(content, "data:image/") {
		// Content is already a data URL, extract base64 part
		parts := strings.Split(content, ",")
		if len(parts) == 2 {
			decoded, err := base64.StdEncoding.DecodeString(parts[1])
			if err != nil {
				return "", fmt.Errorf("failed to decode base64 data URL: %w", err)
			}
			imageBytes = decoded
		} else {
			return "", fmt.Errorf("invalid data URL format")
		}
	} else {
		// Check if content is already base64-encoded (without data URL prefix)
		decoded, err := base64.StdEncoding.DecodeString(content)
		if err == nil {
			// Already base64-encoded
			imageBytes = decoded
		} else {
			// Not base64, treat as raw bytes
			imageBytes = []byte(content)
		}
	}

	// Determine MIME type from file extension
	ext := strings.ToLower(filepath.Ext(filepathStr))
	mimeType := getMimeTypeFromExtension(ext)

	// Resize image if needed (max 1024x1024 pixels for optimal LLM performance)
	const maxDimension = 1024
	resizedBytes, resizedMimeType, err := resizeImage(imageBytes, mimeType, maxDimension)
	if err != nil {
		// Log error but continue with original image
		// This allows the function to work even if resizing fails
		resizedBytes = imageBytes
		resizedMimeType = mimeType
	}

	// Encode resized (or original) image to base64
	base64Data := base64.StdEncoding.EncodeToString(resizedBytes)
	mimeType = resizedMimeType

	// Return structured JSON
	result := map[string]interface{}{
		"_type":     "image_query",
		"filepath":  filepathStr,
		"query":     query,
		"mime_type": mimeType,
		"data":      base64Data,
	}

	responseData, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("failed to marshal response: %w", err)
	}

	return string(responseData), nil
}

// handleUpdateWorkspaceFile handles the update_workspace_file tool execution
func handleUpdateWorkspaceFile(ctx context.Context, args map[string]interface{}) (string, error) {
	// Extract parameters
	filepath, ok := args["filepath"].(string)
	if !ok || filepath == "" {
		return "", fmt.Errorf("filepath is required and must be a string")
	}

	content, ok := args["content"].(string)
	if !ok {
		return "", fmt.Errorf("content is required and must be a string")
	}

	commitMessage := getStringValue(args, "commit_message")

	// Build API URL
	apiURL := getWorkspaceAPIURL() + "/api/documents/" + filepath

	// Prepare request body
	requestBody := map[string]interface{}{
		"content": content,
	}
	if commitMessage != "" {
		requestBody["commit_message"] = commitMessage
	}

	// Create HTTP request with context
	jsonBody, err := json.Marshal(requestBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "PUT", apiURL, strings.NewReader(string(jsonBody)))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	// Set timeout
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	// Make the request
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to call workspace API: %w", err)
	}
	defer resp.Body.Close()

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	// Check HTTP status
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("workspace API returned status %d: %s", resp.StatusCode, string(body))
	}

	// Parse JSON response
	var apiResp WorkspaceAPIResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return "", fmt.Errorf("failed to parse API response: %w", err)
	}

	// Check API response success
	if !apiResp.Success {
		return "", fmt.Errorf("workspace API error: %s", apiResp.Error)
	}

	// Return the raw API response directly
	responseData, err := json.Marshal(apiResp.Data)
	if err != nil {
		return "", fmt.Errorf("failed to marshal API response: %w", err)
	}

	// Emit workspace file operation event for update operation
	emitWorkspaceFileOperation(ctx, "update", filepath, "")

	return string(responseData), nil
}

// handlePatchWorkspaceFile function removed - no longer needed

// handleGetWorkspaceFileNested function removed - no longer needed

// handleRegexSearchWorkspaceFiles handles the regex_search_workspace_files tool execution
func handleRegexSearchWorkspaceFiles(ctx context.Context, args map[string]interface{}) (string, error) {
	// Extract parameters
	query, ok := args["query"].(string)
	if !ok || query == "" {
		return "", fmt.Errorf("query is required and must be a string")
	}

	folder := getStringValue(args, "folder")
	if folder == "" {
		return "", fmt.Errorf("folder is required and must be a string")
	}

	limit := getIntValue(args, "limit")
	if limit == 0 {
		limit = 20 // Default limit
	}
	if limit > 100 {
		limit = 100 // Max limit
	}

	// Extract blocked paths from context (for folder guard)
	var blockedPaths []string
	if bp := ctx.Value(FolderGuardBlockedPathsKey); bp != nil {
		if paths, ok := bp.([]string); ok {
			blockedPaths = paths
		}
	}

	// Build API URL with proper URL encoding
	baseURL := getWorkspaceAPIURL() + "/api/search"
	u, err := url.Parse(baseURL)
	if err != nil {
		return "", fmt.Errorf("failed to parse base URL: %w", err)
	}

	// Add query parameters with proper encoding
	q := u.Query()
	q.Set("query", query)
	q.Set("folder", folder)
	q.Set("limit", fmt.Sprintf("%d", limit))
	// Add blocked paths if present
	if len(blockedPaths) > 0 {
		q.Set("blocked_paths", strings.Join(blockedPaths, ","))
	}
	u.RawQuery = q.Encode()

	apiURL := u.String()

	// Create HTTP request with context
	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	// Set timeout
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	// Make the request
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to call workspace API: %w", err)
	}
	defer resp.Body.Close()

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	// Check HTTP status
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("workspace API returned status %d: %s", resp.StatusCode, string(body))
	}

	// Parse JSON response
	var apiResp WorkspaceAPIResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return "", fmt.Errorf("failed to parse API response: %w (body: %s)", err, string(body))
	}

	// Check API response success
	if !apiResp.Success {
		return "", fmt.Errorf("workspace API error: %s", apiResp.Error)
	}

	// Note: Blocked paths filtering is now handled by the Workspace API
	// The blocked_paths query parameter was passed above, so results are already filtered

	// Debug: Log the actual data structure for troubleshooting
	if apiResp.Data != nil {
		if dataBytes, err := json.Marshal(apiResp.Data); err == nil {
			// Only log first 500 chars to avoid huge logs
			debugInfo := string(dataBytes)
			if len(debugInfo) > 500 {
				debugInfo = debugInfo[:500] + "..."
			}
			// Note: Using fmt.Printf for debugging - can be removed in production
			// fmt.Printf("[DEBUG] regex_search API response data: %s\n", debugInfo)
		}
	}

	// Format the search results for the LLM
	return formatWorkspaceSearchResults(apiResp.Data, query)
}

// handleSemanticSearchWorkspaceFiles handles the semantic_search_workspace_files tool execution
func handleSemanticSearchWorkspaceFiles(ctx context.Context, args map[string]interface{}) (string, error) {
	// Extract parameters
	query, ok := args["query"].(string)
	if !ok || query == "" {
		return "", fmt.Errorf("query is required and must be a string")
	}

	folder, ok := args["folder"].(string)
	if !ok || folder == "" {
		return "", fmt.Errorf("folder is required and must be a string")
	}

	limit := getIntValue(args, "limit")
	if limit == 0 {
		limit = 10 // Default limit for semantic search
	}
	if limit > 50 {
		limit = 50 // Max limit for semantic search
	}

	// Extract blocked paths from context (for folder guard)
	var blockedPaths []string
	if bp := ctx.Value(FolderGuardBlockedPathsKey); bp != nil {
		if paths, ok := bp.([]string); ok {
			blockedPaths = paths
		}
	}

	// Build API URL with proper URL encoding
	baseURL := getWorkspaceAPIURL() + "/api/search/semantic"
	u, err := url.Parse(baseURL)
	if err != nil {
		return "", fmt.Errorf("failed to parse base URL: %w", err)
	}

	// Add query parameters with proper encoding
	q := u.Query()
	q.Set("query", query)
	q.Set("folder", folder)
	q.Set("limit", fmt.Sprintf("%d", limit))
	// Add blocked paths if present
	if len(blockedPaths) > 0 {
		q.Set("blocked_paths", strings.Join(blockedPaths, ","))
	}

	u.RawQuery = q.Encode()
	finalURL := u.String()

	// Make HTTP request
	resp, err := http.Get(finalURL)
	if err != nil {
		return "", fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	// Check HTTP status
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("semantic search API returned status %d: %s", resp.StatusCode, string(body))
	}

	// Parse JSON response
	var apiResp WorkspaceAPIResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return "", fmt.Errorf("failed to parse API response: %w", err)
	}

	// Check API response success
	if !apiResp.Success {
		return "", fmt.Errorf("semantic search API error: %s", apiResp.Error)
	}

	// Note: Blocked paths filtering is now handled by the Workspace API
	// The blocked_paths query parameter was passed above, so results are already filtered

	// Format the semantic search results for the LLM
	return formatSemanticSearchResults(apiResp.Data, query)
}

// handleGlobDiscoverWorkspaceFiles handles the glob_discover_workspace_files tool execution
func handleGlobDiscoverWorkspaceFiles(ctx context.Context, args map[string]interface{}) (string, error) {
	// Extract parameters
	pattern, ok := args["pattern"].(string)
	if !ok || pattern == "" {
		return "", fmt.Errorf("pattern is required and must be a string")
	}

	folder := getStringValue(args, "folder")

	maxDepth := getIntValue(args, "max_depth")
	if maxDepth == 0 {
		maxDepth = -1 // Default to unlimited
	}

	includeDirs := getBoolValue(args, "include_dirs")

	// Extract blocked paths from context (for folder guard)
	var blockedPaths []string
	if bp := ctx.Value(FolderGuardBlockedPathsKey); bp != nil {
		if paths, ok := bp.([]string); ok {
			blockedPaths = paths
		}
	}

	// Build API URL with proper URL encoding
	baseURL := getWorkspaceAPIURL() + "/api/glob"
	u, err := url.Parse(baseURL)
	if err != nil {
		return "", fmt.Errorf("failed to parse base URL: %w", err)
	}

	// Add query parameters with proper encoding
	q := u.Query()
	q.Set("pattern", pattern)
	if folder != "" {
		q.Set("folder", folder)
	}
	if maxDepth >= 0 {
		q.Set("max_depth", fmt.Sprintf("%d", maxDepth))
	}
	if includeDirs {
		q.Set("include_dirs", "true")
	}
	// Add blocked paths if present
	if len(blockedPaths) > 0 {
		q.Set("blocked_paths", strings.Join(blockedPaths, ","))
	}
	u.RawQuery = q.Encode()

	apiURL := u.String()

	// Create HTTP request with context
	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	// Set timeout
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	// Make the request
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to call workspace API: %w", err)
	}
	defer resp.Body.Close()

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	// Check HTTP status
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("workspace API returned status %d: %s", resp.StatusCode, string(body))
	}

	// Parse JSON response
	var apiResp WorkspaceAPIResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return "", fmt.Errorf("failed to parse API response: %w", err)
	}

	// Check API response success
	if !apiResp.Success {
		return "", fmt.Errorf("workspace API error: %s", apiResp.Error)
	}

	// Note: Blocked paths filtering is now handled by the Workspace API
	// The blocked_paths query parameter was passed above, so results are already filtered

	// Format the glob discovery results for the LLM
	return formatGlobDiscoveryResults(apiResp.Data, pattern)
}

// formatGlobDiscoveryResults formats the glob discovery results for the LLM
// Returns only file paths - this is a file discovery tool, not content retrieval
func formatGlobDiscoveryResults(data interface{}, pattern string) (string, error) {
	// Handle nil data (no matches found)
	if data == nil {
		return fmt.Sprintf("**Glob: `%s`** - Found 0 files\n\nNo files found matching the pattern.\n", pattern), nil
	}

	// Extract file paths from response
	var filepaths []string

	// Handle both map response (with results key) and direct array response
	switch v := data.(type) {
	case map[string]interface{}:
		// API returns wrapped response like {"results": [...], "total": N}
		// Try common keys: "results", "files", "documents", "matches"
		var resultsArray []interface{}
		for _, key := range []string{"results", "files", "documents", "matches"} {
			if results, exists := v[key]; exists {
				if arr, ok := results.([]interface{}); ok {
					resultsArray = arr
					break
				}
			}
		}
		// If no known key found, try to use the map values directly
		if resultsArray == nil {
			// Debug: log available keys
			keys := make([]string, 0, len(v))
			for k := range v {
				keys = append(keys, k)
			}
			// fmt.Printf("[DEBUG] formatGlobDiscoveryResults: map keys available: %v\n", keys)
		}
		// Extract filepaths from results array
		for _, item := range resultsArray {
			if itemMap, ok := item.(map[string]interface{}); ok {
				if fp := getStringValue(itemMap, "filepath"); fp != "" {
					filepaths = append(filepaths, fp)
				} else if fp := getStringValue(itemMap, "path"); fp != "" {
					filepaths = append(filepaths, fp)
				} else if fp := getStringValue(itemMap, "name"); fp != "" {
					filepaths = append(filepaths, fp)
				}
			} else if str, ok := item.(string); ok {
				// Handle case where results is just an array of strings
				filepaths = append(filepaths, str)
			}
		}
	case []interface{}:
		// Direct array response
		for _, item := range v {
			if itemMap, ok := item.(map[string]interface{}); ok {
				if fp := getStringValue(itemMap, "filepath"); fp != "" {
					filepaths = append(filepaths, fp)
				} else if fp := getStringValue(itemMap, "path"); fp != "" {
					filepaths = append(filepaths, fp)
				} else if fp := getStringValue(itemMap, "name"); fp != "" {
					filepaths = append(filepaths, fp)
				}
			} else if str, ok := item.(string); ok {
				filepaths = append(filepaths, str)
			}
		}
	default:
		return "", fmt.Errorf("unexpected response format from glob API - expected array or object, got %T", data)
	}

	// Sort filepaths alphabetically
	sort.Strings(filepaths)

	// Format simple output - just paths
	var result strings.Builder
	result.WriteString(fmt.Sprintf("**Glob: `%s`** - Found %d files\n\n", pattern, len(filepaths)))

	if len(filepaths) == 0 {
		result.WriteString("No files found matching the pattern.\n")
		return result.String(), nil
	}

	// List paths only
	for _, fp := range filepaths {
		result.WriteString(fmt.Sprintf("- %s\n", fp))
	}

	return result.String(), nil
}

// handleDeleteWorkspaceFile handles the delete_workspace_file tool execution
func handleDeleteWorkspaceFile(ctx context.Context, args map[string]interface{}) (string, error) {
	// Extract filepath parameter
	filepath, ok := args["filepath"].(string)
	if !ok || filepath == "" {
		return "", fmt.Errorf("filepath is required and must be a string")
	}

	commitMessage := getStringValue(args, "commit_message")

	// Build API URL with confirm parameter
	apiURL := getWorkspaceAPIURL() + "/api/documents/" + filepath + "?confirm=true"
	if commitMessage != "" {
		apiURL += "&commit_message=" + url.QueryEscape(commitMessage)
	}

	// Create HTTP request with context
	req, err := http.NewRequestWithContext(ctx, "DELETE", apiURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	// Set timeout
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	// Make the request
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to call workspace API: %w", err)
	}
	defer resp.Body.Close()

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	// Check HTTP status
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("workspace API returned status %d: %s", resp.StatusCode, string(body))
	}

	// Parse JSON response
	var apiResp WorkspaceAPIResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return "", fmt.Errorf("failed to parse API response: %w", err)
	}

	// Check API response success
	if !apiResp.Success {
		return "", fmt.Errorf("workspace API error: %s", apiResp.Error)
	}

	// Emit workspace file operation event for delete operation
	emitWorkspaceFileOperation(ctx, "delete", filepath, "")

	// Return structured JSON for frontend parsing
	resultJSON := map[string]interface{}{
		"filepath": filepath,
		"deleted":  true,
	}
	if commitMessage != "" {
		resultJSON["commit_message"] = commitMessage
	}

	jsonBytes, err := json.Marshal(resultJSON)
	if err != nil {
		return "", fmt.Errorf("failed to marshal result: %w", err)
	}

	return string(jsonBytes), nil
}

// handleMoveWorkspaceFile handles the move_workspace_file tool execution
func handleMoveWorkspaceFile(ctx context.Context, args map[string]interface{}) (string, error) {
	// Extract parameters
	sourceFilepath, ok := args["source_filepath"].(string)
	if !ok || sourceFilepath == "" {
		return "", fmt.Errorf("source_filepath is required and must be a string")
	}

	destinationFilepath, ok := args["destination_filepath"].(string)
	if !ok || destinationFilepath == "" {
		return "", fmt.Errorf("destination_filepath is required and must be a string")
	}

	commitMessage := getStringValue(args, "commit_message")

	// Build API URL for moving documents
	apiURL := getWorkspaceAPIURL() + "/api/documents/" + sourceFilepath + "/move"

	// Prepare request body
	requestBody := map[string]interface{}{
		"destination_path": destinationFilepath,
	}
	if commitMessage != "" {
		requestBody["commit_message"] = commitMessage
	}

	// Create HTTP request with context
	jsonBody, err := json.Marshal(requestBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", apiURL, strings.NewReader(string(jsonBody)))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	// Set timeout
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	// Make the request
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to call workspace API: %w", err)
	}
	defer resp.Body.Close()

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	// Check HTTP status
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("workspace API returned status %d: %s", resp.StatusCode, string(body))
	}

	// Parse JSON response
	var apiResp WorkspaceAPIResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return "", fmt.Errorf("failed to parse API response: %w", err)
	}

	// Check API response success
	if !apiResp.Success {
		return "", fmt.Errorf("workspace API error: %s", apiResp.Error)
	}

	// Emit workspace file operation events for move operation (delete source + update destination)
	emitWorkspaceFileOperation(ctx, "delete", sourceFilepath, "")
	emitWorkspaceFileOperation(ctx, "update", destinationFilepath, "")

	// Format the response
	var result strings.Builder
	result.WriteString(fmt.Sprintf("📁 **File Moved: `%s` → `%s`**\n\n", sourceFilepath, destinationFilepath))

	if commitMessage != "" {
		result.WriteString(fmt.Sprintf("**Commit Message**: %s\n", commitMessage))
	}

	result.WriteString("**Status**: File successfully moved to new location")
	result.WriteString("\n\n✅ **Operation completed successfully**")

	return result.String(), nil
}

// handleExecuteShellCommand handles the execute_shell_command tool execution
func handleExecuteShellCommand(ctx context.Context, args map[string]interface{}) (string, error) {
	// Extract command parameter (required)
	command, ok := args["command"].(string)
	if !ok || command == "" {
		return "", fmt.Errorf("command is required and must be a string")
	}

	// Extract args parameter (optional)
	var argsArray []string
	if argsVal, exists := args["args"]; exists {
		if argsList, ok := argsVal.([]interface{}); ok {
			for _, arg := range argsList {
				if str, ok := arg.(string); ok {
					argsArray = append(argsArray, str)
				}
			}
		}
	}

	// Extract working_directory parameter (optional)
	workingDirectory := getStringValue(args, "working_directory")

	// Extract timeout parameter (optional)
	timeout := getIntValue(args, "timeout")
	if timeout == 0 {
		timeout = 60 // Default timeout
	}
	if timeout > 300 {
		timeout = 300 // Max timeout
	}

	// Extract use_shell parameter (optional)
	useShell := getBoolValue(args, "use_shell")

	// Build API URL
	apiURL := getWorkspaceAPIURL() + "/api/execute"

	// Prepare request body
	requestBody := map[string]interface{}{
		"command": command,
	}
	if len(argsArray) > 0 {
		requestBody["args"] = argsArray
	}
	if workingDirectory != "" {
		requestBody["working_directory"] = workingDirectory
	}
	if timeout > 0 {
		requestBody["timeout"] = timeout
	}
	if useShell {
		requestBody["use_shell"] = true
	}

	// NEW: Extract folder guard paths from context
	var folderGuardReadPaths []string
	var folderGuardWritePaths []string
	var folderGuardBlockedPaths []string

	if readPaths := ctx.Value(FolderGuardReadPathsKey); readPaths != nil {
		if paths, ok := readPaths.([]string); ok {
			folderGuardReadPaths = paths
		}
	}
	if writePaths := ctx.Value(FolderGuardWritePathsKey); writePaths != nil {
		if paths, ok := writePaths.([]string); ok {
			folderGuardWritePaths = paths
		}
	}
	if blockedPaths := ctx.Value(FolderGuardBlockedPathsKey); blockedPaths != nil {
		if paths, ok := blockedPaths.([]string); ok {
			folderGuardBlockedPaths = paths
		}
	}

	// Add folder guard configuration to request if paths are set
	if len(folderGuardReadPaths) > 0 || len(folderGuardWritePaths) > 0 || len(folderGuardBlockedPaths) > 0 {
		requestBody["folder_guard"] = map[string]interface{}{
			"enabled":          true,
			"read_paths":       folderGuardReadPaths,
			"write_paths":      folderGuardWritePaths,
			"blocked_paths":    folderGuardBlockedPaths,
			"enforcement_mode": "strict",
		}

		// fmt.Printf("[DEBUG] Folder guard enabled for shell execution - Read: %v, Write: %v, Blocked: %v\n",
		// 	folderGuardReadPaths, folderGuardWritePaths, folderGuardBlockedPaths)
	}

	// Create HTTP request with context
	jsonBody, err := json.Marshal(requestBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", apiURL, strings.NewReader(string(jsonBody)))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	// Set timeout (slightly longer than max command timeout)
	clientTimeout := time.Duration(timeout+10) * time.Second
	if clientTimeout > 310*time.Second {
		clientTimeout = 310 * time.Second
	}
	client := &http.Client{
		Timeout: clientTimeout,
	}

	// Make the request
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to call workspace API: %w", err)
	}
	defer resp.Body.Close()

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	// Parse JSON response
	var apiResp WorkspaceAPIResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return "", fmt.Errorf("failed to parse API response: %w", err)
	}

	// Check API response success
	if !apiResp.Success {
		// Try to extract error details from response
		errorMsg := apiResp.Error
		if errorMsg == "" {
			errorMsg = apiResp.Message
		}
		return "", fmt.Errorf("workspace API error: %s", errorMsg)
	}

	// Extract execution response data
	dataMap, ok := apiResp.Data.(map[string]interface{})
	if !ok {
		return "", fmt.Errorf("unexpected response format from workspace API - expected object, got %T", apiResp.Data)
	}

	// Extract fields from response
	stdout := getStringValue(dataMap, "stdout")
	stderr := getStringValue(dataMap, "stderr")
	exitCode := getIntValue(dataMap, "exit_code")
	executionTimeMs := getIntValue(dataMap, "execution_time_ms")
	executedCommand := getStringValue(dataMap, "command")

	// Format the response for LLM
	var result strings.Builder
	result.WriteString("🔧 **Shell Command Execution**\n\n")
	result.WriteString(fmt.Sprintf("**Command**: `%s`\n", executedCommand))

	if workingDirectory != "" {
		result.WriteString(fmt.Sprintf("**Working Directory**: `%s`\n", workingDirectory))
	}

	result.WriteString(fmt.Sprintf("**Exit Code**: %d\n", exitCode))
	result.WriteString(fmt.Sprintf("**Execution Time**: %d ms\n\n", executionTimeMs))

	// Display stdout if present
	if stdout != "" {
		result.WriteString("**Standard Output:**\n")
		result.WriteString("```\n")
		result.WriteString(stdout)
		result.WriteString("\n```\n\n")
	}

	// Display stderr if present (even on success, as some commands write to stderr)
	if stderr != "" {
		result.WriteString("**Standard Error:**\n")
		result.WriteString("```\n")
		result.WriteString(stderr)
		result.WriteString("\n```\n\n")
	}

	// Add status message
	if exitCode == 0 {
		result.WriteString("✅ **Command executed successfully**")

		// Add helpful note for Python commands with empty stdout
		isPythonCmd := strings.Contains(strings.ToLower(command), "python")
		if isPythonCmd && stdout == "" && stderr == "" {
			result.WriteString("\n\n💡 **Note**: Python command produced no output. If you expected output:\n")
			result.WriteString("- Ensure your script uses `print()` statements (not just file writes)\n")
			result.WriteString("- Add `flush=True` to print: `print('message', flush=True)`\n")
			result.WriteString("- Use `-u` flag for unbuffered output: `python3 -u script.py`")
		}
	} else {
		result.WriteString(fmt.Sprintf("⚠️ **Command exited with code %d**", exitCode))
	}

	return result.String(), nil
}

// formatWorkspaceSearchResults formats the search results response for the LLM
func formatWorkspaceSearchResults(data interface{}, query string) (string, error) {
	// Convert data to map for processing
	dataMap, ok := data.(map[string]interface{})
	if !ok {
		// Debug: Log the actual type for troubleshooting
		if dataBytes, err := json.Marshal(data); err == nil {
			debugInfo := string(dataBytes)
			if len(debugInfo) > 500 {
				debugInfo = debugInfo[:500] + "..."
			}
			// fmt.Printf("[DEBUG] formatWorkspaceSearchResults: data is not a map, type=%T, value=%s\n", data, debugInfo)
		}
		return "", fmt.Errorf("unexpected response format from workspace API - expected object, got %T", data)
	}

	// Extract search results
	results, exists := dataMap["results"]
	if !exists {
		// Debug: Log available keys
		keys := make([]string, 0, len(dataMap))
		for k := range dataMap {
			keys = append(keys, k)
		}
		return "", fmt.Errorf("no results found in search response (available keys: %v)", keys)
	}

	// Handle nil results
	if results == nil {
		return formatEmptySearchResults(query, getStringValue(dataMap, "method")), nil
	}

	// Try to convert results to array
	var resultsArray []interface{}
	switch v := results.(type) {
	case []interface{}:
		resultsArray = v
	case []map[string]interface{}:
		// Convert []map[string]interface{} to []interface{}
		resultsArray = make([]interface{}, len(v))
		for i, item := range v {
			resultsArray[i] = item
		}
	default:
		// Debug: Log the actual type for troubleshooting
		if resultsBytes, err := json.Marshal(results); err == nil {
			debugInfo := string(resultsBytes)
			if len(debugInfo) > 500 {
				debugInfo = debugInfo[:500] + "..."
			}
			// fmt.Printf("[DEBUG] formatWorkspaceSearchResults: results is not an array, type=%T, value=%s\n", results, debugInfo)
		}
		return "", fmt.Errorf("results is not an array (type: %T, value: %v)", results, results)
	}

	total := getIntValue(dataMap, "total")
	method := getStringValue(dataMap, "method")

	// Format the response
	var result strings.Builder
	result.WriteString(fmt.Sprintf("🔍 **Search Results for: `%s`**\n", query))
	result.WriteString(fmt.Sprintf("**Method**: %s | **Total**: %d results\n\n", method, total))

	if len(resultsArray) == 0 {
		result.WriteString("No files found matching your search query.\n")
		return result.String(), nil
	}

	result.WriteString(fmt.Sprintf("**Found %d results:**\n\n", len(resultsArray)))

	for i, searchResult := range resultsArray {
		if resultMap, ok := searchResult.(map[string]interface{}); ok {
			// Extract result data
			filepath := getStringValue(resultMap, "filepath")
			title := getStringValue(resultMap, "title")
			folder := getStringValue(resultMap, "folder")
			score := getIntValue(resultMap, "score")
			lineNumber := getIntValue(resultMap, "line_number")
			matchedText := getStringValue(resultMap, "matched_text")
			contentPreview := getStringValue(resultMap, "content_preview")
			lastModified := getTimeValue(resultMap, "last_modified")

			// Format file path (remove /app/workspace-docs/ prefix if present)
			displayPath := strings.TrimPrefix(filepath, "/app/workspace-docs/")

			// Format the result
			result.WriteString(fmt.Sprintf("**%d. %s** (Score: %d)\n", i+1, title, score))
			result.WriteString(fmt.Sprintf("   📁 **Path**: `%s`\n", displayPath))

			if folder != "" {
				result.WriteString(fmt.Sprintf("   📂 **Folder**: `%s`\n", folder))
			}

			if lineNumber > 0 {
				result.WriteString(fmt.Sprintf("   📍 **Line**: %d\n", lineNumber))
			}

			if !lastModified.IsZero() {
				result.WriteString(fmt.Sprintf("   🕒 **Modified**: %s\n", lastModified.Format("2006-01-02 15:04:05")))
			}

			// Add matched text preview
			if matchedText != "" {
				// Truncate if too long
				preview := matchedText
				if len(preview) > 100 {
					preview = preview[:97] + "..."
				}
				result.WriteString(fmt.Sprintf("   💬 **Match**: `%s`\n", strings.TrimSpace(preview)))
			}

			// Add content preview if different from matched text
			if contentPreview != "" && contentPreview != matchedText {
				// Truncate if too long
				preview := contentPreview
				if len(preview) > 150 {
					preview = preview[:147] + "..."
				}
				result.WriteString(fmt.Sprintf("   📄 **Preview**: %s\n", strings.TrimSpace(preview)))
			}

			result.WriteString("\n")
		}
	}

	result.WriteString("💡 **Tip**: Use `read_workspace_file` to read the full content of any file.")

	return result.String(), nil
}

// formatEmptySearchResults formats an empty search result response
func formatEmptySearchResults(query, method string) string {
	var result strings.Builder
	result.WriteString(fmt.Sprintf("🔍 **Search Results for: `%s`**\n", query))
	if method != "" {
		result.WriteString(fmt.Sprintf("**Method**: %s | **Total**: 0 results\n\n", method))
	} else {
		result.WriteString("**Total**: 0 results\n\n")
	}
	result.WriteString("No files found matching your search query.\n")
	return result.String()
}

// handleSyncWorkspaceToGitHub handles the sync_workspace_to_github tool execution
func handleSyncWorkspaceToGitHub(ctx context.Context, args map[string]interface{}) (string, error) {
	// Extract parameters
	force := getBoolValue(args, "force")
	resolveConflicts := getBoolValue(args, "resolve_conflicts")

	// Build API URL for GitHub sync
	apiURL := getWorkspaceAPIURL() + "/api/sync/github"

	// Prepare request body
	requestBody := map[string]interface{}{
		"force":             force,
		"resolve_conflicts": resolveConflicts,
	}

	// Create HTTP request with context
	jsonBody, err := json.Marshal(requestBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", apiURL, strings.NewReader(string(jsonBody)))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	// Set timeout
	client := &http.Client{
		Timeout: 60 * time.Second, // Longer timeout for sync operations
	}

	// Make the request
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to call workspace API: %w", err)
	}
	defer resp.Body.Close()

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	// Check HTTP status
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("workspace API returned status %d: %s", resp.StatusCode, string(body))
	}

	// Parse JSON response
	var apiResp WorkspaceAPIResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return "", fmt.Errorf("failed to parse API response: %w", err)
	}

	// Check API response success
	if !apiResp.Success {
		return "", fmt.Errorf("workspace API error: %s", apiResp.Error)
	}

	// Return the raw API response directly
	responseData, err := json.Marshal(apiResp.Data)
	if err != nil {
		return "", fmt.Errorf("failed to marshal API response: %w", err)
	}

	return string(responseData), nil
}

// handleGetWorkspaceGitHubStatus handles the get_workspace_github_status tool execution
func handleGetWorkspaceGitHubStatus(ctx context.Context, args map[string]interface{}) (string, error) {
	// Extract parameters
	showPending := getBoolValue(args, "show_pending")
	if !showPending {
		showPending = true // Default to true
	}
	showConflicts := getBoolValue(args, "show_conflicts")
	if !showConflicts {
		showConflicts = true // Default to true
	}

	// Build API URL with query parameters
	baseURL := getWorkspaceAPIURL() + "/api/sync/status"
	u, err := url.Parse(baseURL)
	if err != nil {
		return "", fmt.Errorf("failed to parse base URL: %w", err)
	}

	// Add query parameters
	q := u.Query()
	q.Set("show_pending", fmt.Sprintf("%t", showPending))
	q.Set("show_conflicts", fmt.Sprintf("%t", showConflicts))
	u.RawQuery = q.Encode()

	apiURL := u.String()

	// Create HTTP request with context
	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	// Set timeout
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	// Make the request
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to call workspace API: %w", err)
	}
	defer resp.Body.Close()

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	// Check HTTP status
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("workspace API returned status %d: %s", resp.StatusCode, string(body))
	}

	// Parse JSON response
	var apiResp WorkspaceAPIResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return "", fmt.Errorf("failed to parse API response: %w", err)
	}

	// Check API response success
	if !apiResp.Success {
		return "", fmt.Errorf("workspace API error: %s", apiResp.Error)
	}

	// Return the raw API response directly
	responseData, err := json.Marshal(apiResp.Data)
	if err != nil {
		return "", fmt.Errorf("failed to marshal API response: %w", err)
	}
	return string(responseData), nil
}

// handleDiffPatchWorkspaceFile handles the diff_patch_workspace_file tool execution
func handleDiffPatchWorkspaceFile(ctx context.Context, args map[string]interface{}) (string, error) {
	// Extract parameters
	filepath, ok := args["filepath"].(string)
	if !ok || filepath == "" {
		return "", fmt.Errorf("filepath is required and must be a string")
	}

	diff, ok := args["diff"].(string)
	if !ok || diff == "" {
		return "", fmt.Errorf("diff is required and must be a string")
	}

	commitMessage := getStringValue(args, "commit_message")

	// Build API URL for diff patching
	apiURL := getWorkspaceAPIURL() + "/api/documents/" + filepath + "/diff"

	// Prepare request body
	requestBody := map[string]interface{}{
		"diff": diff,
	}
	if commitMessage != "" {
		requestBody["commit_message"] = commitMessage
	}

	// Create HTTP request with context
	jsonBody, err := json.Marshal(requestBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "PATCH", apiURL, strings.NewReader(string(jsonBody)))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	// Set timeout
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	// Make the request
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to call workspace API: %w", err)
	}
	defer resp.Body.Close()

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	// Check HTTP status
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("workspace API returned status %d: %s", resp.StatusCode, string(body))
	}

	// Parse JSON response
	var apiResp WorkspaceAPIResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return "", fmt.Errorf("failed to parse API response: %w", err)
	}

	// Check API response success
	if !apiResp.Success {
		return "", fmt.Errorf("workspace API error: %s", apiResp.Error)
	}

	// Return the raw API response directly
	responseData, err := json.Marshal(apiResp.Data)
	if err != nil {
		return "", fmt.Errorf("failed to marshal API response: %w", err)
	}

	// Emit workspace file operation event for patch operation
	emitWorkspaceFileOperation(ctx, "patch", filepath, "")

	return string(responseData), nil
}

// Helper functions for safe type conversion
func getStringValue(m map[string]interface{}, key string) string {
	if val, exists := m[key]; exists {
		if str, ok := val.(string); ok {
			return str
		}
	}
	return ""
}

func getIntValue(m map[string]interface{}, key string) int {
	if val, exists := m[key]; exists {
		switch v := val.(type) {
		case int:
			return v
		case float64:
			return int(v)
		case string:
			if i, err := strconv.Atoi(v); err == nil {
				return i
			}
		}
	}
	return 0
}

func getFloatValue(m map[string]interface{}, key string) float64 {
	if val, exists := m[key]; exists {
		switch v := val.(type) {
		case float64:
			return v
		case int:
			return float64(v)
		case string:
			if f, err := strconv.ParseFloat(v, 64); err == nil {
				return f
			}
		}
	}
	return 0.0
}

func getBoolValue(m map[string]interface{}, key string) bool {
	if val, exists := m[key]; exists {
		switch v := val.(type) {
		case bool:
			return v
		case string:
			if b, err := strconv.ParseBool(v); err == nil {
				return b
			}
		}
	}
	return false
}

func getTimeValue(m map[string]interface{}, key string) time.Time {
	if val, exists := m[key]; exists {
		if str, ok := val.(string); ok {
			if t, err := time.Parse(time.RFC3339, str); err == nil {
				return t
			}
		}
	}
	return time.Time{}
}

// formatSemanticSearchResults formats the semantic search results response for the LLM
func formatSemanticSearchResults(data interface{}, query string) (string, error) {
	// Convert data to map for processing
	dataMap, ok := data.(map[string]interface{})
	if !ok {
		return "", fmt.Errorf("unexpected response format from semantic search API - expected object, got %T", data)
	}

	// Extract search method and status
	searchMethod := getStringValue(dataMap, "search_method")
	vectorDBStatus := getStringValue(dataMap, "vector_db_status")
	processingTime := getFloatValue(dataMap, "processing_time_ms")
	embeddingModel := getStringValue(dataMap, "embedding_model")

	// Extract semantic results
	semanticResults, exists := dataMap["semantic_results"]
	if !exists {
		semanticResults = []interface{}{}
	}

	semanticArray, ok := semanticResults.([]interface{})
	if !ok {
		semanticArray = []interface{}{}
	}

	totalResults := len(semanticArray)

	// Format the response
	var result strings.Builder
	result.WriteString("🔍 **Semantic Search Results**\n")
	result.WriteString(fmt.Sprintf("**Query**: %s\n", query))
	result.WriteString(fmt.Sprintf("**Method**: %s\n", searchMethod))
	result.WriteString(fmt.Sprintf("**Vector DB**: %s\n", vectorDBStatus))
	if embeddingModel != "" {
		result.WriteString(fmt.Sprintf("**Model**: %s\n", embeddingModel))
	}
	result.WriteString(fmt.Sprintf("**Processing Time**: %.2fms\n", processingTime))
	result.WriteString(fmt.Sprintf("**Total Results**: %d\n\n", totalResults))

	// Format semantic results
	if len(semanticArray) > 0 {
		result.WriteString("## 🧠 **Semantic Results** (AI-powered similarity)\n\n")
		for i, item := range semanticArray {
			if i >= 10 { // Limit to first 10 results
				break
			}

			itemMap, ok := item.(map[string]interface{})
			if !ok {
				continue
			}

			filePath := getStringValue(itemMap, "file_path")
			chunkText := getStringValue(itemMap, "chunk_text")
			score := getFloatValue(itemMap, "score")
			folder := getStringValue(itemMap, "folder")
			wordCount := getIntValue(itemMap, "word_count")

			result.WriteString(fmt.Sprintf("### %d. **%s** (Score: %.3f)\n", i+1, filePath, score))
			if folder != "" {
				result.WriteString(fmt.Sprintf("📁 **Folder**: %s\n", folder))
			}
			result.WriteString(fmt.Sprintf("📊 **Words**: %d\n", wordCount))
			result.WriteString(fmt.Sprintf("📝 **Content**:\n```\n%s\n```\n\n", chunkText))
		}
	}

	if totalResults == 0 {
		result.WriteString("❌ **No results found** for your query.\n")
		result.WriteString("💡 **Suggestions**:\n")
		result.WriteString("- Try different keywords\n")
		result.WriteString("- Use more general terms\n")
		result.WriteString("- Check if the folder path is correct\n")
		result.WriteString("- Increase the limit parameter\n")
		result.WriteString("- Use search_workspace_files for exact text matches\n")
	}

	return result.String(), nil
}

// handleFetchWebContent handles the fetch_web_content tool execution
func handleFetchWebContent(ctx context.Context, args map[string]interface{}) (string, error) {
	// Extract URL parameter (required)
	urlStr, ok := args["url"].(string)
	if !ok || urlStr == "" {
		return "", fmt.Errorf("url is required and must be a string")
	}

	// Validate URL format
	parsedURL, err := url.Parse(urlStr)
	if err != nil {
		return "", fmt.Errorf("invalid URL format: %w", err)
	}

	// Only allow http and https schemes
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return "", fmt.Errorf("URL must use http:// or https:// scheme, got: %s", parsedURL.Scheme)
	}

	// Extract timeout parameter (optional, default: 30, max: 120)
	timeout := 30
	if t, ok := args["timeout"].(float64); ok {
		timeout = int(t)
	}
	if timeout < 1 {
		timeout = 1
	}
	if timeout > 120 {
		timeout = 120
	}

	// Extract convert_to_markdown parameter (optional, default: true)
	convertToMarkdown := true
	if c, ok := args["convert_to_markdown"].(bool); ok {
		convertToMarkdown = c
	}

	// Extract custom headers (optional)
	customHeaders := make(map[string]string)
	if h, ok := args["headers"].(map[string]interface{}); ok {
		for k, v := range h {
			if strVal, ok := v.(string); ok {
				customHeaders[k] = strVal
			}
		}
	}

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, "GET", urlStr, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	// Set default headers
	req.Header.Set("User-Agent", "MCP-Agent-Builder/1.0 (Web Fetch Tool)")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")

	// Apply custom headers
	for k, v := range customHeaders {
		req.Header.Set(k, v)
	}

	// Create HTTP client with timeout
	client := &http.Client{
		Timeout: time.Duration(timeout) * time.Second,
	}

	// Execute request
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to fetch URL: %w", err)
	}
	defer resp.Body.Close()

	// Limit response size to 10MB to prevent memory issues
	const maxSize = 10 * 1024 * 1024
	limitedReader := io.LimitReader(resp.Body, maxSize)

	// Read response body
	body, err := io.ReadAll(limitedReader)
	if err != nil {
		return "", fmt.Errorf("failed to read response body: %w", err)
	}

	// Get content type
	contentType := resp.Header.Get("Content-Type")

	// Convert HTML to markdown if requested and content is HTML
	content := string(body)
	isHTML := strings.Contains(strings.ToLower(contentType), "text/html") ||
		strings.Contains(strings.ToLower(contentType), "application/xhtml")

	if convertToMarkdown && isHTML {
		converter := md.NewConverter("", true, nil)
		markdown, err := converter.ConvertString(content)
		if err == nil {
			content = markdown
		}
		// If conversion fails, keep original HTML content
	}

	// Truncate content if too large (keep first 100KB for LLM processing)
	const maxContentSize = 100 * 1024
	truncated := false
	if len(content) > maxContentSize {
		content = content[:maxContentSize]
		truncated = true
	}

	// Build response
	response := map[string]interface{}{
		"url":          urlStr,
		"status_code":  resp.StatusCode,
		"content_type": contentType,
		"content":      content,
	}

	if truncated {
		response["truncated"] = true
		response["truncated_message"] = "Content was truncated to 100KB for LLM processing"
	}

	if resp.StatusCode >= 400 {
		response["error"] = fmt.Sprintf("HTTP error: %s", resp.Status)
	}

	// Marshal response to JSON
	responseJSON, err := json.Marshal(response)
	if err != nil {
		return "", fmt.Errorf("failed to marshal response: %w", err)
	}

	return string(responseJSON), nil
}

// handleReadPDF handles the read_pdf tool execution
func handleReadPDF(ctx context.Context, args map[string]interface{}) (string, error) {
	// Extract filepath parameter (required)
	filepathStr, ok := args["filepath"].(string)
	if !ok || filepathStr == "" {
		return "", fmt.Errorf("filepath is required and must be a string")
	}

	// Validate file extension
	ext := strings.ToLower(filepath.Ext(filepathStr))
	if ext != ".pdf" {
		return "", fmt.Errorf("file must be a PDF (got extension: %s)", ext)
	}

	// Extract max_pages parameter (optional, default: 50, max: 100)
	maxPages := 50
	if mp, ok := args["max_pages"].(float64); ok {
		maxPages = int(mp)
	}
	if maxPages < 1 {
		maxPages = 1
	}
	if maxPages > 100 {
		maxPages = 100
	}

	// Extract page_range parameter (optional, default: "all")
	pageRange := "all"
	if pr, ok := args["page_range"].(string); ok && pr != "" {
		pageRange = pr
	}

	// URL-encode the filepath segments
	pathSegments := strings.Split(filepathStr, "/")
	encodedSegments := make([]string, len(pathSegments))
	for i, segment := range pathSegments {
		encodedSegments[i] = url.PathEscape(segment)
	}
	encodedPath := strings.Join(encodedSegments, "/")

	// Build API URL to get raw file content
	apiURL := getWorkspaceAPIURL() + "/api/documents/" + encodedPath + "/raw"

	// Create HTTP request with context
	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	// Set timeout
	client := &http.Client{
		Timeout: 60 * time.Second, // Longer timeout for PDF files
	}

	// Make the request
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to call workspace API: %w", err)
	}
	defer resp.Body.Close()

	// Check HTTP status
	if resp.StatusCode == http.StatusNotFound {
		return "", fmt.Errorf("PDF file not found: %s. Use 'list_workspace_files' to find the correct path", filepathStr)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("workspace API returned status %d: %s", resp.StatusCode, string(body))
	}

	// Read the PDF content into memory (limit to 50MB)
	const maxPDFSize = 50 * 1024 * 1024
	limitedReader := io.LimitReader(resp.Body, maxPDFSize)
	pdfData, err := io.ReadAll(limitedReader)
	if err != nil {
		return "", fmt.Errorf("failed to read PDF data: %w", err)
	}

	// Parse the PDF using the ledongthuc/pdf library
	pdfReader, err := pdf.NewReader(bytes.NewReader(pdfData), int64(len(pdfData)))
	if err != nil {
		return "", fmt.Errorf("failed to parse PDF: %w", err)
	}

	totalPages := pdfReader.NumPage()
	if totalPages == 0 {
		return "", fmt.Errorf("PDF has no pages")
	}

	// Parse page range
	pagesToExtract, err := parsePageRange(pageRange, totalPages, maxPages)
	if err != nil {
		return "", fmt.Errorf("invalid page_range: %w", err)
	}

	// Extract text from specified pages
	var textContent strings.Builder
	extractedPages := 0

	for _, pageNum := range pagesToExtract {
		if pageNum < 1 || pageNum > totalPages {
			continue
		}

		page := pdfReader.Page(pageNum)
		if page.V.IsNull() {
			continue
		}

		pageText, err := page.GetPlainText(nil)
		if err != nil {
			// Skip pages that can't be read
			continue
		}

		if pageText != "" {
			textContent.WriteString(fmt.Sprintf("\n--- Page %d ---\n", pageNum))
			textContent.WriteString(pageText)
			extractedPages++
		}
	}

	content := strings.TrimSpace(textContent.String())
	if content == "" {
		content = "(No text content could be extracted from this PDF. It may contain only images or scanned content.)"
	}

	// Truncate if content is too large for LLM (keep first 100KB)
	const maxContentSize = 100 * 1024
	truncated := false
	if len(content) > maxContentSize {
		content = content[:maxContentSize]
		truncated = true
	}

	// Build response
	response := map[string]interface{}{
		"filepath":        filepathStr,
		"total_pages":     totalPages,
		"extracted_pages": extractedPages,
		"page_range":      pageRange,
		"content":         content,
	}

	if truncated {
		response["truncated"] = true
		response["truncated_message"] = "Content was truncated to 100KB for LLM processing"
	}

	// Emit workspace file operation event for read operation
	emitWorkspaceFileOperation(ctx, "read", filepathStr, "")

	// Marshal response to JSON
	responseJSON, err := json.Marshal(response)
	if err != nil {
		return "", fmt.Errorf("failed to marshal response: %w", err)
	}

	return string(responseJSON), nil
}

// parsePageRange parses a page range string and returns a slice of page numbers
// Supports formats: "all", "1-5", "1,3,5", "1-3,5,7-9"
func parsePageRange(rangeStr string, totalPages, maxPages int) ([]int, error) {
	rangeStr = strings.TrimSpace(strings.ToLower(rangeStr))

	if rangeStr == "all" || rangeStr == "" {
		// Return all pages up to maxPages
		pages := make([]int, 0, min(totalPages, maxPages))
		for i := 1; i <= totalPages && len(pages) < maxPages; i++ {
			pages = append(pages, i)
		}
		return pages, nil
	}

	pageSet := make(map[int]bool)
	parts := strings.Split(rangeStr, ",")

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		// Check if it's a range (e.g., "1-5")
		if strings.Contains(part, "-") {
			rangeParts := strings.Split(part, "-")
			if len(rangeParts) != 2 {
				return nil, fmt.Errorf("invalid range format: %s", part)
			}

			start, err := strconv.Atoi(strings.TrimSpace(rangeParts[0]))
			if err != nil {
				return nil, fmt.Errorf("invalid start page in range: %s", part)
			}

			end, err := strconv.Atoi(strings.TrimSpace(rangeParts[1]))
			if err != nil {
				return nil, fmt.Errorf("invalid end page in range: %s", part)
			}

			if start > end {
				start, end = end, start // Swap if reversed
			}

			for i := start; i <= end; i++ {
				if i >= 1 && i <= totalPages {
					pageSet[i] = true
				}
			}
		} else {
			// Single page number
			pageNum, err := strconv.Atoi(part)
			if err != nil {
				return nil, fmt.Errorf("invalid page number: %s", part)
			}
			if pageNum >= 1 && pageNum <= totalPages {
				pageSet[pageNum] = true
			}
		}
	}

	// Convert set to sorted slice
	pages := make([]int, 0, len(pageSet))
	for page := range pageSet {
		pages = append(pages, page)
	}
	sort.Ints(pages)

	// Limit to maxPages
	if len(pages) > maxPages {
		pages = pages[:maxPages]
	}

	if len(pages) == 0 {
		return nil, fmt.Errorf("no valid pages in range")
	}

	return pages, nil
}
