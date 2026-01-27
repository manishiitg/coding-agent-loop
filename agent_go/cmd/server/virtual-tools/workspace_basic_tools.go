package virtualtools

import (
	"context"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
	"mcp-agent-builder-go/agent_go/pkg/workspace"
)

// GetWorkspaceBasicToolCategory returns the category name for workspace basic tools
func GetWorkspaceBasicToolCategory() string {
	return "workspace_basic"
}

// CreateWorkspaceBasicTools returns the shared basic workspace tools from the workspace package
func CreateWorkspaceBasicTools() []llmtypes.Tool {
	return workspace.GetBasicToolDefinitions()
}

// CreateWorkspaceBasicToolExecutors creates the execution functions for workspace basic tools
// Uses the shared executors from pkg/workspace
func CreateWorkspaceBasicToolExecutors() map[string]func(ctx context.Context, args map[string]interface{}) (string, error) {
	client := workspace.NewClient(getWorkspaceAPIURL())
	return workspace.NewBasicExecutor(client)
}
