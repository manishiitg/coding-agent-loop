package workspace

import (
	"context"
	"encoding/json"
	"log"

	"mcp-agent-builder-go/agent_go/pkg/common"
)

type ExecuteShellCommandParams struct {
	Command          string             `json:"command"`
	WorkingDirectory string             `json:"working_directory,omitempty"`
	Timeout          *int               `json:"timeout,omitempty"`
	UseShell         *bool              `json:"use_shell,omitempty"`
	FolderGuard      *FolderGuardConfig `json:"folder_guard,omitempty"`
	ExtraEnv         map[string]string  `json:"extra_env,omitempty"`
}

// ExecuteShellCommand executes a shell command using the REST API: POST /api/execute
func (c *Client) ExecuteShellCommand(ctx context.Context, params ExecuteShellCommandParams) (string, error) {
	// Debug: log ExtraEnv keys
	if len(c.ExtraEnv) > 0 {
		keys := make([]string, 0, len(c.ExtraEnv))
		for k := range c.ExtraEnv {
			keys = append(keys, k)
		}
		log.Printf("[SHELL_DEBUG] Client.ExtraEnv keys: %v (count=%d)", keys, len(c.ExtraEnv))
	} else {
		log.Printf("[SHELL_DEBUG] Client.ExtraEnv is EMPTY")
	}
	// Populate folder guard configuration from context or client.
	// Two context key systems exist:
	//   1. Chat/plan/prototype modes: FolderGuardAllowedWriteFolderKey (from server.go wrappers)
	//   2. Workflow mode: FolderGuardWritePathsKey + FolderGuardReadPathsKey (from orchestrator)
	if params.FolderGuard == nil {
		// Read paths from context (shared by both systems)
		ctxReads, hasCtxReads := ctx.Value(common.FolderGuardReadPathsKey).([]string)

		if allowedWrites, ok := ctx.Value(common.FolderGuardAllowedWriteFolderKey).([]string); ok && len(allowedWrites) > 0 {
			// System 1: chat/plan/prototype mode
			// Merge: ctxReads + allowedWrites (write paths must always be readable
			// so the isolator creates mount points for them in step 4)
			readPaths := allowedWrites
			if hasCtxReads && len(ctxReads) > 0 {
				readPaths = deduplicateStrings(append(ctxReads, allowedWrites...))
			}
			params.FolderGuard = &FolderGuardConfig{
				Enabled:    true,
				WritePaths: allowedWrites,
				ReadPaths:  readPaths,
			}
			log.Printf("[FOLDER_GUARD_RESOLVE] System1 (chat/plan/prototype): URL=%s WritePaths=%v ReadPaths=%v cmd=%s", c.BaseURL, allowedWrites, readPaths, params.Command)
		} else if ctxWrites, ok := ctx.Value(common.FolderGuardWritePathsKey).([]string); ok && len(ctxWrites) > 0 {
			// System 2: workflow orchestrator
			// Merge: ctxReads + ctxWrites (write paths must always be readable
			// so the isolator creates mount points for them in step 4)
			readPaths := ctxWrites
			if hasCtxReads && len(ctxReads) > 0 {
				readPaths = deduplicateStrings(append(ctxReads, ctxWrites...))
			}
			params.FolderGuard = &FolderGuardConfig{
				Enabled:    true,
				WritePaths: ctxWrites,
				ReadPaths:  readPaths,
			}
			log.Printf("[FOLDER_GUARD_RESOLVE] System2 (workflow): URL=%s WritePaths=%v ReadPaths=%v cmd=%s", c.BaseURL, ctxWrites, readPaths, params.Command)
		} else if c.FolderGuard != nil && c.FolderGuard.Enabled {
			// Fallback to client-level folder guard — context had NO folder guard keys.
			// This means the wrapper (wrapExecutorsWithPlanFolderGuard / wrapExecutorsWithChatModeFolderGuard)
			// did NOT inject context values. This happens when the executor is called from a code path
			// that bypasses the wrapper (e.g., a stale registry entry or an unwrapped executor).
			params.FolderGuard = c.FolderGuard
			log.Printf("[FOLDER_GUARD_RESOLVE] FALLBACK to client-level guard: URL=%s ReadPaths=%v WritePaths=%v BlockedPaths=%v cmd=%s",
				c.BaseURL, c.FolderGuard.ReadPaths, c.FolderGuard.WritePaths, c.FolderGuard.BlockedPaths, params.Command)
		} else {
			log.Printf("[FOLDER_GUARD_RESOLVE] NO folder guard at all: URL=%s cmd=%s", c.BaseURL, params.Command)
		}
	} else {
		log.Printf("[FOLDER_GUARD_RESOLVE] params.FolderGuard already set (explicit): URL=%s ReadPaths=%v WritePaths=%v cmd=%s",
			c.BaseURL, params.FolderGuard.ReadPaths, params.FolderGuard.WritePaths, params.Command)
	}

	// Always use shell execution - removed from tool definition to simplify LLM interface
	useShell := true
	params.UseShell = &useShell

	// Set default working directory: client field > ExtraEnv hint > empty (workspace root)
	if params.WorkingDirectory == "" {
		if c.DefaultWorkingDir != "" {
			params.WorkingDirectory = c.DefaultWorkingDir
		} else if dir, ok := c.ExtraEnv["_DEFAULT_WORKING_DIR"]; ok && dir != "" {
			params.WorkingDirectory = dir
		}
	}

	// Inject extra env vars from client (e.g., MCP_API_URL, MCP_API_TOKEN)
	if len(c.ExtraEnv) > 0 && params.ExtraEnv == nil {
		params.ExtraEnv = c.ExtraEnv
	}

	path := "/api/execute"
	respBody, err := c.request(ctx, "POST", path, params)
	if err != nil {
		return "", err
	}

	// Always return the formatted result (stdout, stderr, exit_code) as a successful
	// tool result. Previously, non-zero exit codes were wrapped as Go errors, which
	// caused the LLM to see a generic "Tool execution failed" message instead of the
	// actual stdout/stderr. By returning the full output, the LLM can read exit_code,
	// stderr, and stdout to understand what went wrong and take corrective action.
	// Only infrastructure errors (network, validation) use Go errors.
	formatted, _ := formatShellResponse(respBody)
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
	// Skip "command" — the LLM already knows what it sent
	if data, ok := resp["data"]; ok {
		var dataFields map[string]json.RawMessage
		if err := json.Unmarshal(data, &dataFields); err == nil {
			for k, v := range dataFields {
				if k == "command" {
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

// deduplicateStrings removes duplicate entries from a string slice while preserving order.
func deduplicateStrings(input []string) []string {
	seen := make(map[string]bool, len(input))
	result := make([]string, 0, len(input))
	for _, s := range input {
		if !seen[s] {
			seen[s] = true
			result = append(result, s)
		}
	}
	return result
}
