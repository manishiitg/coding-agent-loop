package workspace

import (
	"context"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

type GetWorkspaceGithubStatusParams struct {
	ShowPending   *bool `json:"show_pending,omitempty"`
	ShowConflicts *bool `json:"show_conflicts,omitempty"`
}

// GetWorkspaceGithubStatus checks git status via REST API: GET /api/sync/status
func (c *Client) GetWorkspaceGithubStatus(ctx context.Context, params GetWorkspaceGithubStatusParams) (string, error) {
	path := "/api/sync/status"
	// Query params could be added here if the API supported filtering, 
	// but /api/sync/status usually returns everything.
	respBody, err := c.request(ctx, "GET", path, nil)
	if err != nil {
		return "", err
	}
	return string(respBody), nil
}

type SyncWorkspaceToGithubParams struct {
	CommitMessage *string `json:"commit_message,omitempty"`
	Force         *bool   `json:"force,omitempty"`
}

// SyncWorkspaceToGithub syncs with remote via REST API: POST /api/sync/github
func (c *Client) SyncWorkspaceToGithub(ctx context.Context, params SyncWorkspaceToGithubParams) (string, error) {
	path := "/api/sync/github"
	    respBody, err := c.request(ctx, "POST", path, params)
	    if err != nil {
	        return "", err
	    }
	    return string(respBody), nil
	}
	
	// GetGitToolDefinitions returns the tool definitions for the git workspace tools
	func GetGitToolDefinitions() []llmtypes.Tool {
		return []llmtypes.Tool{
			{
				Type: "function",
				Function: &llmtypes.FunctionDefinition{
					Name:        "get_workspace_github_status",
					Description: "Get the current GitHub sync status including pending changes, conflicts, and repository information. Uses git commands to check local repository status and connection to GitHub remote.",
					Parameters: llmtypes.NewParameters(map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"show_pending": map[string]interface{}{
								"type": "boolean",
								"description": "Show pending changes (default: true)",
							},
							"show_conflicts": map[string]interface{}{
								"type": "boolean",
								"description": "Show conflicts if any (default: true)",
							},
						},
					}),
				},
			},
			{
				Type: "function",
				Function: &llmtypes.FunctionDefinition{
					Name:        "sync_workspace_to_github",
					Description: "Sync all workspace files to GitHub repository using standard git workflow: commit 	 pull 	 push. Always pulls first to ensure synchronization. Fails if merge conflicts are detected (requires manual resolution).",
					Parameters: llmtypes.NewParameters(map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"force": map[string]interface{}{
								"type": "boolean",
								"description": "Force sync even if there are conflicts (not recommended, default: false)",
							},
							"commit_message": map[string]interface{}{
								"type": "string",
								"description": "Custom commit message for the sync operation (optional)",	
							},
						},
					}),
				},
			},
		}
	}
	
