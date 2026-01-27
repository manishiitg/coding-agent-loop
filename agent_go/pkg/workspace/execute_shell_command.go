package workspace

import (
	"context"
)

type ExecuteShellCommandParams struct {
	Command          string    `json:"command"`
	WorkingDirectory *string   `json:"working_directory,omitempty"`
	Args             *[]string `json:"args,omitempty"`
	Timeout          *int      `json:"timeout,omitempty"`
	UseShell         *bool     `json:"use_shell,omitempty"`
}

// ExecuteShellCommand executes a shell command using the REST API: POST /api/execute
func (c *Client) ExecuteShellCommand(ctx context.Context, params ExecuteShellCommandParams) (string, error) {
	// Validate working directory against folder guard (write operation since commands can modify files)
	if params.WorkingDirectory != nil && *params.WorkingDirectory != "" {
		if err := c.ValidatePath(*params.WorkingDirectory, true); err != nil {
			return "", err
		}
	}

	path := "/api/execute"
	respBody, err := c.request(ctx, "POST", path, params)
	if err != nil {
		return "", err
	}

	return string(respBody), nil
}
