package virtualtools

import (
	"context"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

// GetWorkspaceGitToolCategory returns the category name for workspace git tools
func GetWorkspaceGitToolCategory() string {
	return "workspace_git"
}

// CreateWorkspaceGitTools creates workspace git virtual tools (2 tools)
// These are the GitHub sync and status tools
func CreateWorkspaceGitTools() []llmtypes.Tool {
	var tools []llmtypes.Tool

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
	tools = append(tools, syncGitHubTool)

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
	tools = append(tools, gitHubStatusTool)

	return tools
}

// CreateWorkspaceGitToolExecutors creates the execution functions for workspace git tools
func CreateWorkspaceGitToolExecutors() map[string]func(ctx context.Context, args map[string]interface{}) (string, error) {
	executors := make(map[string]func(ctx context.Context, args map[string]interface{}) (string, error))

	executors["sync_workspace_to_github"] = handleSyncWorkspaceToGitHub
	executors["get_workspace_github_status"] = handleGetWorkspaceGitHubStatus

	return executors
}
