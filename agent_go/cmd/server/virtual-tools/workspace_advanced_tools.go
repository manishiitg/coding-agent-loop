package virtualtools

import (
	"context"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
	"mcp-agent-builder-go/agent_go/pkg/workspace"
)

// GetWorkspaceAdvancedToolCategory returns the category name for workspace advanced tools
func GetWorkspaceAdvancedToolCategory() string {
	return "workspace_advanced"
}

// CreateWorkspaceAdvancedTools returns the shared advanced workspace tools from the workspace package
func CreateWorkspaceAdvancedTools() []llmtypes.Tool {
	return workspace.GetAdvancedToolDefinitions()
}

// CreateWorkspaceAdvancedToolExecutors creates the execution functions for workspace advanced tools
// Uses the shared executors from pkg/workspace
// Includes FolderGuard to protect per-user folders (Chats/, Downloads/) from LLM writes
func CreateWorkspaceAdvancedToolExecutors() map[string]func(ctx context.Context, args map[string]interface{}) (string, error) {
	client := workspace.NewClient(
		getWorkspaceAPIURL(),
		workspace.WithFolderGuard(getDefaultFolderGuard()),
	)
	return workspace.NewAdvancedExecutor(client)
}

// CreateWorkspaceAdvancedToolExecutorsWithUserID creates workspace advanced tool executors
// with an explicit user ID set on the client, ensuring per-user folder isolation
// even if the context doesn't carry the user ID.
func CreateWorkspaceAdvancedToolExecutorsWithUserID(userID string) map[string]func(ctx context.Context, args map[string]interface{}) (string, error) {
	client := workspace.NewClient(
		getWorkspaceAPIURL(),
		workspace.WithFolderGuard(getDefaultFolderGuard()),
		workspace.WithUserID(userID),
	)
	return workspace.NewAdvancedExecutor(client)
}
