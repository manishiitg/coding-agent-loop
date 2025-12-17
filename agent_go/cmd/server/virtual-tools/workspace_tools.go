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
	"strconv"
	"strings"
	"time"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"

	"mcpagent/events"

	"golang.org/x/image/draw"
)

// WorkspaceEventEmitter interface for emitting workspace file operation events
type WorkspaceEventEmitter interface {
	HandleEvent(ctx context.Context, event *events.AgentEvent) error
}

// Context keys for workspace event emission
type contextKey string

const (
	// WorkspaceEventEmitterKey is the context key for the workspace event emitter
	WorkspaceEventEmitterKey contextKey = "workspace_event_emitter"
	// TurnKey is the context key for the turn number
	TurnKey contextKey = "turn"
	// ServerNameKey is the context key for the server name
	ServerNameKey contextKey = "server_name"
)

// Legacy constants for backward compatibility (use exported versions)
var (
	workspaceEventEmitterKey = WorkspaceEventEmitterKey
	turnKey                  = TurnKey
	serverNameKey            = ServerNameKey
)

// getEventEmitterFromContext extracts the event emitter from context
// Tries multiple key types to handle different packages using their own contextKey types
func getEventEmitterFromContext(ctx context.Context) WorkspaceEventEmitter {
	// Try with our typed key first
	if emitter, ok := ctx.Value(workspaceEventEmitterKey).(WorkspaceEventEmitter); ok {
		return emitter
	}
	// Try with string key (for backward compatibility and cross-package compatibility)
	if emitter, ok := ctx.Value("workspace_event_emitter").(WorkspaceEventEmitter); ok {
		return emitter
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

// getTurnFromContext extracts the turn number from context
// Tries multiple key types to handle different packages using their own contextKey types
func getTurnFromContext(ctx context.Context) int {
	// Try with our typed key first
	if turn, ok := ctx.Value(turnKey).(int); ok {
		return turn
	}
	// Try with string key (for backward compatibility and cross-package compatibility)
	if turn, ok := ctx.Value("turn").(int); ok {
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
func getServerNameFromContext(ctx context.Context) string {
	// Try with our typed key first
	if serverName, ok := ctx.Value(serverNameKey).(string); ok {
		return serverName
	}
	// Try with string key (for backward compatibility and cross-package compatibility)
	if serverName, ok := ctx.Value("server_name").(string); ok {
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
	emitter := getEventEmitterFromContext(ctx)
	if emitter == nil {
		// No emitter in context - this is expected for some orchestrator direct calls
		fmt.Printf("[WorkspaceTools] emitWorkspaceFileOperation: No emitter in context for operation=%s, filepath=%s, folder=%s\n", operation, filepath, folder)
		return
	}

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

	fmt.Printf("[WorkspaceTools] emitWorkspaceFileOperation: Emitting event operation=%s, filepath=%s, folder=%s, turn=%d, serverName=%s, shouldHighlight=%v\n",
		operation, filepath, folder, turn, serverName, shouldHighlight)

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
		fmt.Printf("[WorkspaceTools] emitWorkspaceFileOperation: Successfully emitted event\n")
	}
}

// WorkspaceAPIResponse represents the response structure from the workspace API
type WorkspaceAPIResponse struct {
	Success bool        `json:"success"`
	Message string      `json:"message"`
	Data    interface{} `json:"data"` // Can be WorkspaceFolderListing, WorkspaceFileContent, etc.
	Error   string      `json:"error,omitempty"`
}

// WorkspaceFile represents a file in the workspace
type WorkspaceFile struct {
	Filepath    string    `json:"filepath"`
	Size        int64     `json:"size,omitempty"`
	ModifiedAt  time.Time `json:"modified_at,omitempty"`
	IsDirectory bool      `json:"is_directory,omitempty"`
}

// WorkspaceFolderItem represents a single item (file or folder) in a workspace folder listing
type WorkspaceFolderItem struct {
	Filepath    string                `json:"filepath"`
	Folder      string                `json:"folder,omitempty"`
	Name        string                `json:"name,omitempty"`
	Size        int64                 `json:"size,omitempty"`
	ModifiedAt  time.Time             `json:"modified_at,omitempty"`
	Type        string                `json:"type,omitempty"` // "file" or "folder"
	IsDirectory bool                  `json:"is_directory,omitempty"`
	IsDir       bool                  `json:"is_dir,omitempty"`   // Alternative field name
	Children    []WorkspaceFolderItem `json:"children,omitempty"` // Nested children for folders
}

// WorkspaceFolderListing represents the folder listing response from workspace API
// The API returns an array of folder items, where each item can have nested children
type WorkspaceFolderListing []WorkspaceFolderItem

// WorkspaceFileContent represents the content response when reading a file
type WorkspaceFileContent struct {
	Filepath string `json:"filepath"`
	Content  string `json:"content"`
}

// getWorkspaceAPIURL returns the workspace API base URL from environment or default
func getWorkspaceAPIURL() string {
	if url := os.Getenv("WORKSPACE_API_URL"); url != "" {
		return url
	}
	return "http://localhost:8081"
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

	// Add execute_shell_command tool
	executeShellTool := llmtypes.Tool{
		Type: "function",
		Function: &llmtypes.FunctionDefinition{
			Name:        "execute_shell_command",
			Description: "Execute shell commands and scripts within the workspace directory. Commands run with a 60-second timeout (configurable up to 300 seconds) and are restricted to the workspace boundary (/app/workspace-docs).\n\n**PATH USAGE RULES:**\n- **Tool Parameters**: Use relative paths (e.g., 'working_directory: \"scripts\"' resolves to '/app/workspace-docs/scripts')\n- **Inside Scripts**: When writing Python/shell scripts that reference files, use absolute paths starting with '/app/workspace-docs' (e.g., '/app/workspace-docs/script.py', '/app/workspace-docs/data/file.csv'). This ensures scripts work regardless of the working_directory setting.\n\nReturns stdout, stderr, and exit code. Use 'use_shell: true' for complex commands with pipes (|), redirects (>), chaining (&&, ||), environment variables, or wildcards.",
			Parameters: llmtypes.NewParameters(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"command": map[string]interface{}{
						"type":        "string",
						"description": "Shell command to execute. If use_shell is true, this can be a complex command with pipes, redirects, etc. (e.g., 'ls', 'grep', 'find', './script.sh', 'ls | grep .md', 'cd dir && ls', 'VAR=value command')",
					},
					"args": map[string]interface{}{
						"type":        "array",
						"items":       map[string]interface{}{"type": "string"},
						"description": "Command arguments as an array of strings (e.g., ['-l', '-a'] for 'ls -l -a'). Ignored if use_shell is true - include arguments in command string instead.",
					},
					"working_directory": map[string]interface{}{
						"type":        "string",
						"description": "Relative directory path within workspace to execute command (default: root of workspace). Example: 'scripts' resolves to '/app/workspace-docs/scripts'. Sets the current working directory (CWD) for command execution, allowing relative paths in commands to resolve relative to this directory.",
					},
					"timeout": map[string]interface{}{
						"type":        "integer",
						"description": "Timeout in seconds (default: 60, max: 300)",
					},
					"use_shell": map[string]interface{}{
						"type":        "boolean",
						"description": "Execute through shell interpreter (sh -c). Enables complex commands with pipes (|), redirects (>), chaining (&&, ||), environment variables, wildcards, etc. Default: false (direct execution, more secure). Set to true for complex commands.",
					},
				},
				"required": []string{"command"},
			}),
		},
	}
	workspaceTools = append(workspaceTools, executeShellTool)

	// Add read_image tool
	readImageTool := llmtypes.Tool{
		Type: "function",
		Function: &llmtypes.FunctionDefinition{
			Name:        "read_image",
			Description: "Read an image file from workspace and ask a question about it. This tool will process the image and your question together.",
			Parameters: llmtypes.NewParameters(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"filepath": map[string]interface{}{
						"type":        "string",
						"description": "Path to the image file. Must always be workspace-relative (e.g., 'Downloads/hdfc_login.png', 'images/photo.jpg', 'screenshots/screen.png'). Do not use absolute paths.",
					},
					"query": map[string]interface{}{
						"type":        "string",
						"description": "Question to ask about the image (e.g., 'What is in this image?', 'Describe this image', 'What text is written here?')",
					},
				},
				"required": []string{"filepath", "query"},
			}),
		},
	}
	workspaceTools = append(workspaceTools, readImageTool)

	return workspaceTools
}

// GetToolCategory returns the category name for workspace tools
func GetWorkspaceToolCategory() string {
	return "workspace"
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
	executors["execute_shell_command"] = handleExecuteShellCommand
	executors["read_image"] = handleReadImage

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

	// Debug logging
	fmt.Printf("[DEBUG list_workspace_files] Requesting folder: %s, max_depth: %d, URL: %s\n", folder, maxDepth, req.URL.String())

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
		fmt.Printf("[DEBUG] Workspace API returned nil data for folder: %s, maxDepth: %d\n", folder, maxDepth)
	}

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
		return "", fmt.Errorf("failed to parse API response: %w (body: %s)", err, string(body))
	}

	// Check API response success
	if !apiResp.Success {
		return "", fmt.Errorf("workspace API error: %s", apiResp.Error)
	}

	// Debug: Log the actual data structure for troubleshooting
	if apiResp.Data != nil {
		if dataBytes, err := json.Marshal(apiResp.Data); err == nil {
			// Only log first 500 chars to avoid huge logs
			debugInfo := string(dataBytes)
			if len(debugInfo) > 500 {
				debugInfo = debugInfo[:500] + "..."
			}
			// Note: Using fmt.Printf for debugging - can be removed in production
			fmt.Printf("[DEBUG] regex_search API response data: %s\n", debugInfo)
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
			fmt.Printf("[DEBUG] formatWorkspaceSearchResults: data is not a map, type=%T, value=%s\n", data, debugInfo)
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
			fmt.Printf("[DEBUG] formatWorkspaceSearchResults: results is not an array, type=%T, value=%s\n", results, debugInfo)
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
