package virtualtools

import (
	"context"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
	"mcp-agent-builder-go/agent_go/pkg/workspace"
)

// GetWorkspaceGitToolCategory returns the category name for workspace git tools
func GetWorkspaceGitToolCategory() string {
	return "workspace_git"
}

// CreateWorkspaceGitTools returns the shared git workspace tools from the workspace package
func CreateWorkspaceGitTools() []llmtypes.Tool {
	return workspace.GetGitToolDefinitions()
}

// CreateWorkspaceGitToolExecutors creates the execution functions for workspace git tools
// Uses the shared executors from pkg/workspace
// Includes FolderGuard to protect per-user folders (Chats/, Downloads/) from LLM writes
func CreateWorkspaceGitToolExecutors() map[string]func(ctx context.Context, args map[string]interface{}) (string, error) {
	client := workspace.NewClient(
		getWorkspaceAPIURL(),
		workspace.WithFolderGuard(getDefaultFolderGuard()),
	)
	return workspace.NewGitExecutor(client)
}
