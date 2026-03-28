package virtualtools

import (
	"context"

	"mcp-agent-builder-go/agent_go/pkg/workspace"
)

// GetWorkspaceBasicToolCategory returns the category name for workspace basic tools
func GetWorkspaceBasicToolCategory() string {
	return "workspace_basic"
}

// getDefaultFolderGuard returns the default FolderGuard config
// - Blocks _users/ directory (internal structure, prevents accessing other users' data)
// - Allows Chats/, Downloads/ access (routed to user's folder via X-User-ID header)
func getDefaultFolderGuard() *workspace.FolderGuardConfig {
	return &workspace.FolderGuardConfig{
		Enabled: true,
		// Only block _users/ directory - this prevents direct access to internal user structure
		// Chats/ and Downloads/ are allowed - they get routed to current user's folder via X-User-ID
		BlockedPaths: []string{"_users"},
	}
}

// CreateWorkspaceBasicToolExecutors creates the execution functions for workspace basic tools
// Uses the shared executors from pkg/workspace
// Includes FolderGuard to protect per-user folders (Chats/, Downloads/) from LLM writes
func CreateWorkspaceBasicToolExecutors() map[string]func(ctx context.Context, args map[string]interface{}) (string, error) {
	client := workspace.NewClient(
		getWorkspaceAPIURL(),
		workspace.WithFolderGuard(getDefaultFolderGuard()),
	)
	return workspace.NewBasicExecutor(client)
}

// CreateWorkspaceBasicToolExecutorsWithUserID creates workspace basic tool executors
// with an explicit user ID set on the client.
func CreateWorkspaceBasicToolExecutorsWithUserID(userID string) map[string]func(ctx context.Context, args map[string]interface{}) (string, error) {
	client := workspace.NewClient(
		getWorkspaceAPIURL(),
		workspace.WithFolderGuard(getDefaultFolderGuard()),
		workspace.WithUserID(userID),
	)
	return workspace.NewBasicExecutor(client)
}
