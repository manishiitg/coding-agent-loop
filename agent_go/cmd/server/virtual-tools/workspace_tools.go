package virtualtools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mcp-agent/agent_go/internal/llmtypes"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// WorkspaceAPIResponse represents the response structure from the workspace API
type WorkspaceAPIResponse struct {
	Success bool        `json:"success"`
	Message string      `json:"message"`
	Data    interface{} `json:"data"`
	Error   string      `json:"error,omitempty"`
}

// WorkspaceFile represents a file in the workspace
type WorkspaceFile struct {
	Filepath    string    `json:"filepath"`
	Size        int64     `json:"size,omitempty"`
	ModifiedAt  time.Time `json:"modified_at,omitempty"`
	IsDirectory bool      `json:"is_directory,omitempty"`
}

// getWorkspaceAPIURL returns the workspace API base URL from environment or default
func getWorkspaceAPIURL() string {
	if url := os.Getenv("PLANNER_API_URL"); url != "" {
		return url
	}
	return "http://localhost:8081"
}

// CreateWorkspaceTools creates workspace-related virtual tools
func CreateWorkspaceTools() []llmtypes.Tool {
	var workspaceTools []llmtypes.Tool

	// Add list_workspace_files tool
	listFilesTool := llmtypes.Tool{
		Type: "function",
		Function: &llmtypes.FunctionDefinition{
			Name:        "list_workspace_files",
			Description: "List all files and folders in the workspace.",
			Parameters: llmtypes.NewParameters(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"folder": map[string]interface{}{
						"type":        "string",
						"description": "Folder path to filter results (e.g., 'docs', 'examples', 'folder/subfolder')",
					},
					"max_depth": map[string]interface{}{
						"type":        "integer",
						"description": "Maximum depth of hierarchical structure to return (default: 3, max: 10)",
					},
				},
				"required": []string{"folder"},
			}),
		},
	}
	workspaceTools = append(workspaceTools, listFilesTool)

	// Add read_workspace_file tool
	readFileTool := llmtypes.Tool{
		Type: "function",
		Function: &llmtypes.FunctionDefinition{
			Name:        "read_workspace_file",
			Description: "Read the content of a specific file from the workspace by filepath",
			Parameters: llmtypes.NewParameters(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"filepath": map[string]interface{}{
						"type":        "string",
						"description": "Full file path (e.g., 'docs/example.md', 'configs/settings.json', 'README.md')",
					},
				},
				"required": []string{"filepath"},
			}),
		},
	}
	workspaceTools = append(workspaceTools, readFileTool)

	// Add update_workspace_file tool
	updateFileTool := llmtypes.Tool{
		Type: "function",
		Function: &llmtypes.FunctionDefinition{
			Name:        "update_workspace_file",
			Description: "Create a new file or update/replace the entire content of an existing file in the workspace (upsert behavior). If you are using existing file prefer to use diff_patch_workspace_file instead",
			Parameters: llmtypes.NewParameters(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"filepath": map[string]interface{}{
						"type":        "string",
						"description": "Full file path of the file to create or update (e.g., 'docs/guide.md', 'configs/settings.json')",
					},
					"content": map[string]interface{}{
						"type":        "string",
						"description": "Content to write to the file (will create new file or replace entire existing file)",
					},
					"commit_message": map[string]interface{}{
						"type":        "string",
						"description": "Optional commit message for version control",
					},
				},
				"required": []string{"filepath", "content"},
			}),
		},
	}
	workspaceTools = append(workspaceTools, updateFileTool)

	// Add diff_patch_workspace_file tool (unified diff patching)
	diffPatchFileTool := llmtypes.Tool{
		Type: "function",
		Function: &llmtypes.FunctionDefinition{
			Name:        "diff_patch_workspace_file",
			Description: "🚨 CRITICAL WORKFLOW: 1) MANDATORY - Use read_workspace_file first to see exact current content 2) Generate diff using 'diff -U0' format with perfect context matching 3) Apply patch. This tool requires precise unified diff format - context lines must match file exactly. Use for targeted, surgical changes to specific file sections. ⚠️ FAILURE TO FOLLOW WORKFLOW WILL RESULT IN PATCH FAILURES.",
			Parameters: llmtypes.NewParameters(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"filepath": map[string]interface{}{
						"type":        "string",
						"description": "Full file path of the file to patch (e.g., 'docs/guide.md', 'configs/settings.json')",
					},
					"diff": map[string]interface{}{
						"type":        "string",
						"description": "🚨 CRITICAL REQUIREMENTS - Unified diff format string to apply:\n\n**MANDATORY FORMAT (like 'diff -U0'):**\n- Headers: --- a/file.md\\n+++ b/file.md\n- Hunk headers: @@ -startLine,lineCount +startLine,lineCount @@\n- Context lines: ' ' prefix (SPACE + content - MUST match file exactly)\n- Removals: '-' prefix (MINUS + content)\n- Additions: '+' prefix (PLUS + content)\n- MUST end with newline character\n\n🚨 CRITICAL: Context lines start with SPACE ( ), NOT minus (-)!\n   Correct: ' # Header' (space + content)\n   Wrong:   '- # Header' (minus + content)\n\n**PERFECT EXAMPLE:**\n--- a/todo.md\n+++ b/todo.md\n@@ -1,3 +1,4 @@\n # Todo List\n+**New addition**: Added via unified diff\n \n ## Objective\n@@ -4,3 +5,4 @@\n ## Notes\n - Leverages tavily-search for comprehensive research\n+- Added new methodology note\n\n**🚨 CRITICAL VALIDATION CHECKLIST:**\n- ✅ File exists and was read with read_workspace_file\n- ✅ Context lines copied EXACTLY from file content (including whitespace)\n- ✅ Hunk headers show correct line numbers\n- ✅ Diff ends with newline character\n- ✅ Proper unified diff format (---/+++ headers)\n- ✅ No truncated or malformed lines\n- ✅ Test with simple single-line addition first",
					},
					"commit_message": map[string]interface{}{
						"type":        "string",
						"description": "Optional commit message for version control",
					},
				},
				"required": []string{"filepath", "diff"},
			}),
		},
	}
	workspaceTools = append(workspaceTools, diffPatchFileTool)

	// get_workspace_file_nested tool removed - no longer needed

	// Add regex_search_workspace_files tool
	regexSearchTool := llmtypes.Tool{
		Type: "function",
		Function: &llmtypes.FunctionDefinition{
			Name:        "regex_search_workspace_files",
			Description: "Search files in the workspace using regex patterns across full content. Searches text-based files within the specified folder only. Requires 'folder' parameter.",
			Parameters: llmtypes.NewParameters(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"query": map[string]interface{}{
						"type":        "string",
						"description": "Regex search query to find in files (e.g., 'docker', 'test.*file', \\d{4}-\\d{2}-\\d{2}', '(error|exception)', 'markdown')",
					},
					"folder": map[string]interface{}{
						"type":        "string",
						"description": "Folder path to search within (e.g., 'docs', 'src', 'configs'). Required.",
					},
					"limit": map[string]interface{}{
						"type":        "integer",
						"description": "Maximum number of results to return (default: 20, max: 100)",
					},
				},
				"required": []string{"query", "folder"},
			}),
		},
	}
	workspaceTools = append(workspaceTools, regexSearchTool)

	// Add semantic_search_workspace_files tool
	semanticSearchTool := llmtypes.Tool{
		Type: "function",
		Function: &llmtypes.FunctionDefinition{
			Name:        "semantic_search_workspace_files",
			Description: "Search files using AI-powered semantic similarity. Finds content by meaning, not just exact text matches. Uses embeddings to understand context and relationships between concepts. For exact text matches, use search_workspace_files tool instead.",
			Parameters: llmtypes.NewParameters(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"query": map[string]interface{}{
						"type":        "string",
						"description": "Natural language search query (e.g., 'docker configuration', 'error handling', 'API endpoints', 'authentication setup', 'database connection')",
					},
					"folder": map[string]interface{}{
						"type":        "string",
						"description": "Folder path to search within (e.g., 'docs', 'src', 'configs'). Required parameter for semantic search.",
					},
					"limit": map[string]interface{}{
						"type":        "integer",
						"description": "Maximum number of semantic results to return (default: 10, max: 50)",
					},
				},
				"required": []string{"query", "folder"},
			}),
		},
	}
	workspaceTools = append(workspaceTools, semanticSearchTool)

	// Add sync_workspace_to_github tool
	syncGitHubTool := llmtypes.Tool{
		Type: "function",
		Function: &llmtypes.FunctionDefinition{
			Name:        "sync_workspace_to_github",
			Description: "Sync all workspace files to GitHub repository using standard git workflow: commit → pull → push. Always pulls first to ensure synchronization. Fails if merge conflicts are detected (requires manual resolution).",
			Parameters: llmtypes.NewParameters(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"force": map[string]interface{}{
						"type":        "boolean",
						"description": "Force sync even if there are conflicts (not recommended, default: false)",
					},
					"commit_message": map[string]interface{}{
						"type":        "string",
						"description": "Custom commit message for the sync operation (optional)",
					},
				},
			}),
		},
	}
	workspaceTools = append(workspaceTools, syncGitHubTool)

	// Add get_workspace_github_status tool
	gitHubStatusTool := llmtypes.Tool{
		Type: "function",
		Function: &llmtypes.FunctionDefinition{
			Name:        "get_workspace_github_status",
			Description: "Get the current GitHub sync status including pending changes, conflicts, and repository information. Uses git commands to check local repository status and connection to GitHub remote.",
			Parameters: llmtypes.NewParameters(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"show_pending": map[string]interface{}{
						"type":        "boolean",
						"description": "Show pending changes (default: true)",
					},
					"show_conflicts": map[string]interface{}{
						"type":        "boolean",
						"description": "Show conflicts if any (default: true)",
					},
				},
			}),
		},
	}
	workspaceTools = append(workspaceTools, gitHubStatusTool)

	// Add delete_workspace_file tool
	deleteFileTool := llmtypes.Tool{
		Type: "function",
		Function: &llmtypes.FunctionDefinition{
			Name:        "delete_workspace_file",
			Description: "Delete a specific file from the workspace permanently. This action cannot be undone. Use with caution.",
			Parameters: llmtypes.NewParameters(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"filepath": map[string]interface{}{
						"type":        "string",
						"description": "Full file path of the file to delete (e.g., 'docs/example.md', 'configs/settings.json')",
					},
					"commit_message": map[string]interface{}{
						"type":        "string",
						"description": "Optional commit message for version control",
					},
				},
				"required": []string{"filepath"},
			}),
		},
	}
	workspaceTools = append(workspaceTools, deleteFileTool)

	// Add move_workspace_file tool
	moveFileTool := llmtypes.Tool{
		Type: "function",
		Function: &llmtypes.FunctionDefinition{
			Name:        "move_workspace_file",
			Description: "Move a file from one location to another in the workspace. Can be used to move files between folders or rename files.",
			Parameters: llmtypes.NewParameters(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"source_filepath": map[string]interface{}{
						"type":        "string",
						"description": "Current file path of the file to move (e.g., 'docs/old-file.md', 'configs/settings.json')",
					},
					"destination_filepath": map[string]interface{}{
						"type":        "string",
						"description": "New file path where the file should be moved (e.g., 'archive/old-file.md', 'settings/config.json')",
					},
					"commit_message": map[string]interface{}{
						"type":        "string",
						"description": "Optional commit message for version control",
					},
				},
				"required": []string{"source_filepath", "destination_filepath"},
			}),
		},
	}
	workspaceTools = append(workspaceTools, moveFileTool)

	return workspaceTools
}

// CreateWorkspaceToolExecutors creates the execution functions for workspace tools
func CreateWorkspaceToolExecutors() map[string]func(ctx context.Context, args map[string]interface{}) (string, error) {
	executors := make(map[string]func(ctx context.Context, args map[string]interface{}) (string, error))

	executors["list_workspace_files"] = handleListWorkspaceFiles
	executors["read_workspace_file"] = handleReadWorkspaceFile
	executors["update_workspace_file"] = handleUpdateWorkspaceFile
	// executors["patch_workspace_file"] = handlePatchWorkspaceFile // REMOVED - no longer needed
	executors["diff_patch_workspace_file"] = handleDiffPatchWorkspaceFile
	// executors["get_workspace_file_nested"] = handleGetWorkspaceFileNested // REMOVED - no longer needed
	executors["regex_search_workspace_files"] = handleRegexSearchWorkspaceFiles
	executors["semantic_search_workspace_files"] = handleSemanticSearchWorkspaceFiles
	executors["sync_workspace_to_github"] = handleSyncWorkspaceToGitHub
	executors["get_workspace_github_status"] = handleGetWorkspaceGitHubStatus
	executors["delete_workspace_file"] = handleDeleteWorkspaceFile
	executors["move_workspace_file"] = handleMoveWorkspaceFile

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
	req.URL.RawQuery = q.Encode()

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

	// Debug logging for troubleshooting
	if apiResp.Data == nil {
		fmt.Printf("[DEBUG] Workspace API returned nil data for folder: %s, maxDepth: %d\n", folder, maxDepth)
	}

	// Return the raw API response directly
	responseData, err := json.Marshal(apiResp.Data)
	if err != nil {
		return "", fmt.Errorf("failed to marshal API response: %w", err)
	}
	return string(responseData), nil
}

// handleReadWorkspaceFile handles the read_workspace_file tool execution
func handleReadWorkspaceFile(ctx context.Context, args map[string]interface{}) (string, error) {
	// Extract filepath parameter
	filepath, ok := args["filepath"].(string)
	if !ok || filepath == "" {
		return "", fmt.Errorf("filepath is required and must be a string")
	}

	// Build API URL
	apiURL := getWorkspaceAPIURL() + "/api/documents/" + filepath

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

	// Format the semantic search results for the LLM
	return formatSemanticSearchResults(apiResp.Data, query)
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

	// Format the response
	var result strings.Builder
	result.WriteString(fmt.Sprintf("🗑️ **File Deleted: `%s`**\n\n", filepath))

	if commitMessage != "" {
		result.WriteString(fmt.Sprintf("**Commit Message**: %s\n", commitMessage))
	}

	result.WriteString("**Status**: File permanently deleted from workspace")
	result.WriteString("\n\n⚠️ **Warning**: This action cannot be undone. The file has been permanently removed.")

	return result.String(), nil
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

// formatWorkspaceSearchResults formats the search results response for the LLM
func formatWorkspaceSearchResults(data interface{}, query string) (string, error) {
	// Convert data to map for processing
	dataMap, ok := data.(map[string]interface{})
	if !ok {
		return "", fmt.Errorf("unexpected response format from workspace API - expected object, got %T", data)
	}

	// Extract search results
	results, exists := dataMap["results"]
	if !exists {
		return "", fmt.Errorf("no results found in search response")
	}

	resultsArray, ok := results.([]interface{})
	if !ok {
		return "", fmt.Errorf("results is not an array")
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

			// Format file path (remove /app/planner-docs/ prefix if present)
			displayPath := strings.TrimPrefix(filepath, "/app/planner-docs/")

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
