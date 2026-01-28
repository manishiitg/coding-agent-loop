package virtualtools

import (
	"context"
	"os"

	"github.com/manishiitg/mcpagent/events"
	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

// getWorkspaceAPIURL returns the workspace API base URL from environment or default
func getWorkspaceAPIURL() string {
	if url := os.Getenv("WORKSPACE_API_URL"); url != "" {
		return url
	}
	return "http://localhost:8081"
}

// WorkspaceFileContent is the response shape for read_workspace_file (used by orchestrator and server)
type WorkspaceFileContent struct {
	Content string `json:"content"`
}

// WorkspaceFile represents a file or folder from the workspace list API (used by orchestrator)
type WorkspaceFile struct {
	FilePath    string `json:"filepath"`
	Folder      string `json:"folder,omitempty"`
	Type        string `json:"type"`
	IsDirectory bool   `json:"is_directory,omitempty"`
	Size        int64  `json:"size,omitempty"`
	ModifiedAt  string `json:"modified_at,omitempty"`
	Content     string `json:"content,omitempty"`
	Name        string `json:"-"`
}

// WorkspaceAPIResponse is the generic workspace API response (Success, Message, Error, Data)
type WorkspaceAPIResponse struct {
	Success bool        `json:"success"`
	Message string      `json:"message"`
	Error   string      `json:"error"`
	Data    interface{} `json:"data"`
}

// WorkspaceFolderItem is a single item in a folder listing (can have Children for nested listing)
type WorkspaceFolderItem struct {
	FilePath    string                `json:"filepath"`
	Type        string                `json:"type"`
	IsDirectory bool                  `json:"is_directory,omitempty"`
	Children    []WorkspaceFolderItem `json:"children,omitempty"`
}

// WorkspaceFolderListing is the folder listing response (array of folder items)
type WorkspaceFolderListing []WorkspaceFolderItem

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

// CreateWorkspaceTools creates all workspace-related virtual tools (basic + git + advanced)
// This is the backward-compatible function that returns all tools
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
