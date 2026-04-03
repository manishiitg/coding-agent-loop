package workspace

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"mcp-agent-builder-go/agent_go/pkg/common"
)

// SessionShellConfig delegates to common.SessionShellConfig.
// Kept for backward compatibility — new code should use common.SessionShellConfig directly.
type SessionShellConfig = common.SessionShellConfig

// SetSessionWorkingDir delegates to common.SetSessionWorkingDir.
func SetSessionWorkingDir(sessionID, dir string) {
	common.SetSessionWorkingDir(sessionID, dir)
}

// SetSessionFolderGuard delegates to common.SetSessionFolderGuard.
func SetSessionFolderGuard(sessionID string, readPaths, writePaths []string) {
	common.SetSessionFolderGuard(sessionID, readPaths, writePaths)
}

// SetSessionGeminiProjectDirID delegates to common.SetSessionGeminiProjectDirID.
func SetSessionGeminiProjectDirID(sessionID, dirID string) {
	common.SetSessionGeminiProjectDirID(sessionID, dirID)
}

// ClearSessionShellConfig delegates to common.ClearSessionShellConfig.
func ClearSessionShellConfig(sessionID string) {
	common.ClearSessionShellConfig(sessionID)
}

// GetSessionShellConfig delegates to common.GetSessionShellConfig.
func GetSessionShellConfig(sessionID string) *SessionShellConfig {
	return common.GetSessionShellConfig(sessionID)
}

type ExecuteShellCommandParams struct {
	Command          string             `json:"command"`
	WorkingDirectory string             `json:"working_directory,omitempty"`
	Timeout          *int               `json:"timeout,omitempty"`
	UseShell         *bool              `json:"use_shell,omitempty"`
	FolderGuard      *FolderGuardConfig `json:"folder_guard,omitempty"` // Set internally — never from LLM args
	ExtraEnv         map[string]string  `json:"extra_env,omitempty"`
}

// ExecuteShellCommand executes a shell command using the REST API: POST /api/execute
func (c *Client) ExecuteShellCommand(ctx context.Context, params ExecuteShellCommandParams) (string, error) {
	// Debug: log ExtraEnv keys, MCP_API_URL value, and client pointer for identity tracking
	if len(c.ExtraEnv) > 0 {
		keys := make([]string, 0, len(c.ExtraEnv))
		for k := range c.ExtraEnv {
			keys = append(keys, k)
		}
		log.Printf("[SHELL_DEBUG] Client=%p ExtraEnv keys: %v (count=%d) MCP_API_URL=%s MCP_SESSION_ID=%s", c, keys, len(c.ExtraEnv), c.ExtraEnv["MCP_API_URL"], c.ExtraEnv["MCP_SESSION_ID"])
	} else {
		log.Printf("[SHELL_DEBUG] Client=%p ExtraEnv is EMPTY", c)
	}
	// Always use shell execution - removed from tool definition to simplify LLM interface
	useShell := true
	params.UseShell = &useShell

	// Override default shell timeout: send timeout=0 to signal "no timeout" to the
	// Docker /api/execute endpoint. The default 60s is too short for long-running
	// operations like sub-agent calls via curl (which can take up to 30 minutes).
	// This matches direct tool call behavior where Timeout: 0 = runs indefinitely.
	if params.Timeout == nil {
		noTimeout := 0
		params.Timeout = &noTimeout
	}

	// Look up per-session shell config (working dir + folder guard)
	sessionID := ""
	if sid, ok := ctx.Value(common.ChatSessionIDKey).(string); ok && sid != "" {
		sessionID = sid
	}
	sessionCfg := GetSessionShellConfig(sessionID)

	// Block agent-browser CLI calls via shell — catches direct calls, bash -c wrapping, piping, etc.
	// The agent_browser tool handles CDP URL resolution, session tracking, and folder guard.
	// Calling agent-browser directly via shell bypasses all of this.
	cmdTrimmed := strings.TrimSpace(params.Command)
	if strings.Contains(cmdTrimmed, "agent-browser") {
		log.Printf("[SHELL] Blocked agent-browser CLI call. Command: %s", params.Command)

		// Context-aware error: guide LLM to the correct browser tool
		browserMode := ""
		if sessionCfg != nil {
			browserMode = sessionCfg.BrowserMode
		}
		if browserMode == "playwright" || browserMode == "stealth" {
			toolName := "Playwright browser_* tools (browser_snapshot, browser_click, browser_type, etc.)"
			if browserMode == "stealth" {
				toolName = "Camofox MCP tools (snapshot, click, type_text, navigate, etc.)"
			}
			return "ERROR: Do not call agent-browser via execute_shell_command. Use the " + toolName + " for browser automation.\n\n" +
				"The agent-browser CLI is not the correct tool for this workflow. " +
				"Start with browser_snapshot to see the current page state, then use the appropriate browser_* tool for interactions.", nil
		}
		return "ERROR: Do not call agent-browser directly via execute_shell_command. Use the agent_browser tool instead.\n\n" +
			"For direct tool call mode:\n" +
			"  agent_browser(command=\"open\", args=[\"https://example.com\"], session=\"default\")\n\n" +
			"For code execution mode (MCP bridge):\n" +
			"  Call get_api_spec(server_name=\"agent_browser\") to get the HTTP API spec,\n" +
			"  then POST to the agent_browser endpoint via HTTP.\n\n" +
			"The agent_browser tool handles CDP connection, session management, and folder sandboxing automatically. " +
			"Calling agent-browser CLI directly bypasses CDP URL resolution and will fail inside Docker.", nil
	}

	if sessionCfg != nil && sessionCfg.GeminiProjectDirID != "" {
		params.Command = rewriteGeminiRelativePaths(params.Command, sessionCfg.GeminiProjectDirID)
		if params.ExtraEnv == nil {
			params.ExtraEnv = make(map[string]string)
		}
		if _, exists := params.ExtraEnv["GEMINI_PROJECT_DIR"]; !exists {
			params.ExtraEnv["GEMINI_PROJECT_DIR"] = geminiProjectDirPath(sessionCfg.GeminiProjectDirID)
		}
	}

	// Set default working directory:
	// Priority: param > session config > client field > ExtraEnv > empty (workspace root)
	if params.WorkingDirectory == "" {
		if sessionCfg != nil && sessionCfg.WorkingDir != "" {
			params.WorkingDirectory = sessionCfg.WorkingDir
		} else if c.DefaultWorkingDir != "" {
			params.WorkingDirectory = c.DefaultWorkingDir
		} else if dir, ok := c.ExtraEnv["_DEFAULT_WORKING_DIR"]; ok && dir != "" {
			params.WorkingDirectory = dir
		}
	}

	// Populate folder guard configuration for the Isolator.
	// params.FolderGuard is never set by callers — it's always nil on entry.
	// Priority: session config > context keys > client-level fallback.
	if sessionCfg != nil && len(sessionCfg.WritePaths) > 0 {
		// Session config: set by SetSessionFolderGuard() — highest priority.
		// Covers CLI/Gemini providers that bypass the Go folder guard context wrappers.
		readPaths := sessionCfg.WritePaths
		if len(sessionCfg.ReadPaths) > 0 {
			readPaths = deduplicateStrings(append(sessionCfg.ReadPaths, sessionCfg.WritePaths...))
		}
		params.FolderGuard = &FolderGuardConfig{
			Enabled:    true,
			WritePaths: sessionCfg.WritePaths,
			ReadPaths:  readPaths,
		}
		log.Printf("[FOLDER_GUARD_RESOLVE] SessionConfig: session=%s WritePaths=%v ReadPaths=%v cmd=%s", sessionID, sessionCfg.WritePaths, readPaths, params.Command)
	} else if allowedWrites, ok := ctx.Value(common.FolderGuardAllowedWriteFolderKey).([]string); ok && len(allowedWrites) > 0 {
		// Context System 1: chat/plan/prototype mode
		ctxReads, hasCtxReads := ctx.Value(common.FolderGuardReadPathsKey).([]string)
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
		// Context System 2: workflow orchestrator
		ctxReads, hasCtxReads := ctx.Value(common.FolderGuardReadPathsKey).([]string)
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
		// Client-level fallback — no session config, no context keys.
		params.FolderGuard = c.FolderGuard
		log.Printf("[FOLDER_GUARD_RESOLVE] FALLBACK to client-level guard: URL=%s ReadPaths=%v WritePaths=%v BlockedPaths=%v cmd=%s",
			c.BaseURL, c.FolderGuard.ReadPaths, c.FolderGuard.WritePaths, c.FolderGuard.BlockedPaths, params.Command)
	} else {
		log.Printf("[FOLDER_GUARD_RESOLVE] NO folder guard at all: URL=%s cmd=%s", c.BaseURL, params.Command)
	}

	// Block absolute host paths when folder guard is active.
	// The Docker isolator sandboxes /app/workspace-docs/ but Docker Desktop
	// for Mac auto-mounts /Users/ into containers via VirtioFS. Commands with
	// absolute host paths bypass the workspace sandbox entirely.
	if params.FolderGuard != nil && params.FolderGuard.Enabled {
		if err := blockAbsoluteHostPaths(params.Command); err != nil {
			log.Printf("[FOLDER_GUARD] Blocked shell command with absolute host path: %s", params.Command)
			return "", err
		}
	}

	// Inject extra env vars from client (e.g., MCP_API_URL, MCP_API_TOKEN, SECRET_*)
	if len(c.ExtraEnv) > 0 {
		if params.ExtraEnv == nil {
			params.ExtraEnv = c.ExtraEnv
		} else {
			// Merge client env into params (client vars don't override explicit params)
			for k, v := range c.ExtraEnv {
				if _, exists := params.ExtraEnv[k]; !exists {
					params.ExtraEnv[k] = v
				}
			}
		}
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
	// Debug: log result size and first 500 chars to diagnose truncation issues
	if len(formatted) < 200 && strings.Contains(params.Command, "call_sub_agent") {
		log.Printf("[SHELL_RESULT_DEBUG] call_sub_agent via shell: formatted_len=%d raw_resp_len=%d formatted=%s", len(formatted), len(respBody), formatted)
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

	// Command failed if exit code is non-zero or API returned an error
	commandFailed := exitCode != 0 || hasError

	// Unwrap MCP API response from stdout to reduce JSON nesting for the LLM.
	// When the shell command calls an MCP/virtual tool via HTTP (e.g. call_sub_agent),
	// stdout contains: {"success":true,"result":"{...actual JSON...}"}
	// This unwraps it so the LLM sees the actual result directly instead of
	// triple-nested escaped JSON.
	if exitCode == 0 && !hasError {
		if stdoutRaw, ok := out["stdout"]; ok {
			var stdout string
			if json.Unmarshal(stdoutRaw, &stdout) == nil {
				stdout = strings.TrimSpace(stdout)
				if unwrapped := tryUnwrapMCPAPIResponse(stdout); unwrapped != "" {
					out["stdout"] = json.RawMessage(fmt.Sprintf("%q", unwrapped))
				}
			}
		}
	}

	result, err := json.Marshal(out)
	if err != nil {
		return string(respBody), false
	}

	return string(result), commandFailed
}

// tryUnwrapMCPAPIResponse attempts to unwrap a nested MCP API response.
// Input format: {"success":true,"result":"{...escaped JSON...}"}
// Returns the inner result string (which may itself be JSON) or "" if not applicable.
func tryUnwrapMCPAPIResponse(stdout string) string {
	if !strings.HasPrefix(stdout, "{") {
		return ""
	}

	var apiResp struct {
		Success bool   `json:"success"`
		Result  string `json:"result"`
		Error   string `json:"error"`
	}
	if err := json.Unmarshal([]byte(stdout), &apiResp); err != nil {
		return ""
	}

	// Only unwrap if it matches the MCP API response shape (has success + result/error)
	if !apiResp.Success && apiResp.Error != "" {
		return fmt.Sprintf("ERROR: %s", apiResp.Error)
	}

	if apiResp.Result == "" {
		return ""
	}

	// If the result is itself valid JSON, return it as-is (pretty for the LLM)
	var jsonCheck json.RawMessage
	if json.Unmarshal([]byte(apiResp.Result), &jsonCheck) == nil {
		return apiResp.Result
	}

	// Plain text result
	return apiResp.Result
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

var geminiRelativePathPattern = regexp.MustCompile("(^|[\\s\\\"'=:(])(?:(?:\\.\\./|\\./)*)\\.gemini($|[/\\s\\\"'`:)])")

func geminiProjectDirPath(projectDirID string) string {
	return filepath.Join(os.TempDir(), "gemini-cli-project-"+projectDirID)
}

func rewriteGeminiRelativePaths(command, projectDirID string) string {
	if projectDirID == "" || !strings.Contains(command, ".gemini") {
		return command
	}

	absoluteGeminiDir := filepath.Join(geminiProjectDirPath(projectDirID), ".gemini")
	rewritten := geminiRelativePathPattern.ReplaceAllString(command, "${1}"+absoluteGeminiDir+"${2}")
	if rewritten != command {
		log.Printf("[SHELL] Rewrote Gemini relative path for project dir %s: %s -> %s", projectDirID, command, rewritten)
	}
	return rewritten
}

// blockAbsoluteHostPaths rejects commands containing absolute paths that
// reference the host filesystem rather than the Docker workspace.
// The Docker workspace root /app/workspace-docs/ and /tmp/ are allowed;
// host paths like /Users/, /home/ are blocked.
func blockAbsoluteHostPaths(command string) error {
	if !strings.Contains(command, "/") {
		return nil
	}

	// Check both with and without trailing slash to catch:
	//   "ls /Users/"        → matches "/Users/"
	//   "find /Users -type" → matches "/Users " (space after)
	//   "cat /Users"        → matches "/Users" at end of string
	blockedDirs := []string{
		"/users",
		"/home",
		"/root",
	}

	cmdLower := strings.ToLower(command)
	for _, dir := range blockedDirs {
		if strings.Contains(cmdLower, dir+"/") ||
			strings.Contains(cmdLower, dir+" ") ||
			strings.HasSuffix(cmdLower, dir) {
			return fmt.Errorf(
				"access denied: shell command references absolute host path (%s). "+
					"Use workspace-relative paths (e.g. 'Workflow/myproject/file.txt') or "+
					"absolute Docker paths (/app/workspace-docs/...) instead", dir)
		}
	}
	return nil
}
