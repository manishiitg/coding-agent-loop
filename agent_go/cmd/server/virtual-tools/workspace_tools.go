package virtualtools

import (
	"context"
	"os"

	"github.com/manishiitg/mcpagent/events"
	"mcp-agent-builder-go/agent_go/pkg/common"
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
	FilePath string          `json:"filepath"`
	Type     string          `json:"type"`
	Content  string          `json:"content,omitempty"`
	Children []WorkspaceFile `json:"children,omitempty"`
	Name     string          `json:"-"`
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
	Type     string                `json:"type"`
	Children []WorkspaceFolderItem `json:"children,omitempty"`
}

// WorkspaceFolderListing is the folder listing response (array of folder items)
type WorkspaceFolderListing []WorkspaceFolderItem

// WorkspaceEventEmitter interface for emitting workspace file operation events
type WorkspaceEventEmitter interface {
	HandleEvent(ctx context.Context, event *events.AgentEvent) error
}

// Context keys for workspace event emission and folder guard paths
// These are now imported from the common package
const (
	// WorkspaceEventEmitterKey is the context key for the workspace event emitter
	WorkspaceEventEmitterKey common.ContextKey = "workspace_event_emitter"
	// TurnKey is the context key for the turn number
	TurnKey common.ContextKey = "turn"
	// ServerNameKey is the context key for the server name
	ServerNameKey common.ContextKey = "server_name"
	// FolderGuardReadPathsKey is the context key for folder guard read paths
	FolderGuardReadPathsKey = common.FolderGuardReadPathsKey
	// FolderGuardWritePathsKey is the context key for folder guard write paths
	FolderGuardWritePathsKey = common.FolderGuardWritePathsKey
	// FolderGuardBlockedPathsKey is the context key for blocked paths (deny list)
	FolderGuardBlockedPathsKey = common.FolderGuardBlockedPathsKey
	// FolderGuardAllowedWriteFolderKey is the context key for the only folder allowed for writes (chat mode)
	FolderGuardAllowedWriteFolderKey = common.FolderGuardAllowedWriteFolderKey
)


// CreateWorkspaceToolExecutors creates the execution functions for all workspace tools (basic + advanced)
func CreateWorkspaceToolExecutors() map[string]func(ctx context.Context, args map[string]interface{}) (string, error) {
	executors := CreateWorkspaceBasicToolExecutors()
	for k, v := range CreateWorkspaceAdvancedToolExecutors() {
		executors[k] = v
	}
	return executors
}
