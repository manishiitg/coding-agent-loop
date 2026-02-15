package workspace

import (
	"context"
	"encoding/json"
	"fmt"

	"mcp-agent-builder-go/agent_go/pkg/common"
)

type ExecuteShellCommandParams struct {
	Command          string             `json:"command"`
	WorkingDirectory string             `json:"working_directory"`
	Timeout          *int               `json:"timeout,omitempty"`
	UseShell         *bool              `json:"use_shell,omitempty"`
	FolderGuard      *FolderGuardConfig `json:"folder_guard,omitempty"`
}

// ExecuteShellCommand executes a shell command using the REST API: POST /api/execute
func (c *Client) ExecuteShellCommand(ctx context.Context, params ExecuteShellCommandParams) (string, error) {
	// Default empty working directory to "." (workspace root)
	if params.WorkingDirectory == "" {
		params.WorkingDirectory = "."
	}

	// Validate working directory against folder guard (write operation since commands can modify files)
	if params.WorkingDirectory != "" {
		if err := c.ValidatePath(params.WorkingDirectory, true); err != nil {
			return "", err
		}
	}

	// Populate folder guard configuration from context or client
	if params.FolderGuard == nil {
		// Check for chat mode folder guard in context
		if allowedWrites, ok := ctx.Value(common.FolderGuardAllowedWriteFolderKey).([]string); ok && len(allowedWrites) > 0 {
			params.FolderGuard = &FolderGuardConfig{
				Enabled:    true,
				WritePaths: allowedWrites,
				// In chat mode, allow reading everything in the workspace
				ReadPaths: []string{"."},
			}
		} else if c.FolderGuard != nil && c.FolderGuard.Enabled {
			// Fallback to client-level folder guard
			params.FolderGuard = c.FolderGuard
		}
	}

	// Always use shell execution - removed from tool definition to simplify LLM interface
	useShell := true
	params.UseShell = &useShell

	path := "/api/execute"
	respBody, err := c.request(ctx, "POST", path, params)
	if err != nil {
		return "", err
	}

	formatted, isError := formatShellResponse(respBody)
	if isError {
		return "", fmt.Errorf("%s", formatted)
	}
	return formatted, nil
}

// formatShellResponse strips the success/message envelope and returns
// just the data fields (stdout, stderr, exit_code, execution_time_ms) plus error if present.
// Returns the formatted string and whether the command failed (non-zero exit code).
func formatShellResponse(respBody []byte) (string, bool) {
	var resp map[string]json.RawMessage
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return string(respBody), false
	}

	// Build output with just the useful fields
	out := make(map[string]json.RawMessage)
	var exitCode float64

	// Copy data fields (stdout, stderr, exit_code, execution_time_ms)
	// Skip "command" and "working_directory" — the LLM already knows what it sent
	if data, ok := resp["data"]; ok {
		var dataFields map[string]json.RawMessage
		if err := json.Unmarshal(data, &dataFields); err == nil {
			for k, v := range dataFields {
				if k == "command" || k == "working_directory" {
					continue
				}
				out[k] = v
			}
			// Check exit code
			if ec, ok := dataFields["exit_code"]; ok {
				json.Unmarshal(ec, &exitCode)
			}
		}
	}

	// Include error if present
	hasError := false
	if errVal, ok := resp["error"]; ok && string(errVal) != `""` && string(errVal) != "null" {
		out["error"] = errVal
		hasError = true
	}

	result, err := json.Marshal(out)
	if err != nil {
		return string(respBody), false
	}

	// Command failed if exit code is non-zero or API returned an error
	commandFailed := exitCode != 0 || hasError

	return string(result), commandFailed
}
