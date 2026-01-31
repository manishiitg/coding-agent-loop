package workspace

import (
	"context"
	"mcp-agent-builder-go/agent_go/pkg/common"
)

type ExecuteShellCommandParams struct {
	Command          string             `json:"command"`
	WorkingDirectory *string            `json:"working_directory,omitempty"`
	Args             *[]string          `json:"args,omitempty"`
	Timeout          *int               `json:"timeout,omitempty"`
	UseShell         *bool              `json:"use_shell,omitempty"`
	FolderGuard      *FolderGuardConfig `json:"folder_guard,omitempty"`
}

// ExecuteShellCommand executes a shell command using the REST API: POST /api/execute
func (c *Client) ExecuteShellCommand(ctx context.Context, params ExecuteShellCommandParams) (string, error) {
	// Validate working directory against folder guard (write operation since commands can modify files)
	if params.WorkingDirectory != nil && *params.WorkingDirectory != "" {
		if err := c.ValidatePath(*params.WorkingDirectory, true); err != nil {
			return "", err
		}
	}

	// Populate folder guard configuration from context or client
	if params.FolderGuard == nil {
		// Check for chat mode folder guard in context
		if allowedWrite, ok := ctx.Value(common.FolderGuardAllowedWriteFolderKey).(string); ok && allowedWrite != "" {
			params.FolderGuard = &FolderGuardConfig{
				Enabled:    true,
				WritePaths: []string{allowedWrite},
				// In chat mode, allow reading everything in the workspace
				ReadPaths: []string{"."},
			}
		} else if c.FolderGuard != nil && c.FolderGuard.Enabled {
			// Fallback to client-level folder guard
			params.FolderGuard = c.FolderGuard
		}
	}

	path := "/api/execute"
	respBody, err := c.request(ctx, "POST", path, params)
	if err != nil {
		return "", err
	}

	return string(respBody), nil
}
