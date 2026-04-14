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
	"time"

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
func (c *Client) ExecuteShellCommand(ctx context.Context, params ExecuteShellCommandParams) (ShellCommandResult, error) {
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
	//
	// We strip heredoc bodies and quoted string literals before the substring check so that
	// commands embedding the literal text "agent-browser" as data (e.g. a python3 <<PYEOF
	// script rewriting plan descriptions that mention the CLI, or `echo "use agent-browser"`)
	// don't trip the guard. What remains after stripping is the executable part of the
	// command, where a real invocation would appear.
	cmdTrimmed := strings.TrimSpace(params.Command)
	if containsAgentBrowserInvocation(cmdTrimmed) {
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
			return ShellCommandResult{
				Stderr:   "ERROR: Do not call agent-browser via execute_shell_command. Use the " + toolName + " for browser automation.\n\nThe agent-browser CLI is not the correct tool for this workflow. Start with browser_snapshot to see the current page state, then use the appropriate browser_* tool for interactions.",
				ExitCode: 1,
			}, nil
		}
		return ShellCommandResult{
			Stderr:   "ERROR: Do not call agent-browser directly via execute_shell_command. Use the agent_browser tool instead.\n\nFor direct tool call mode:\n  agent_browser(command=\"open\", args=[\"https://example.com\"], session=\"default\")\n\nFor code execution mode (MCP bridge):\n  Call get_api_spec(server_name=\"agent_browser\") to get the HTTP API spec,\n  then POST to the agent_browser endpoint via HTTP.\n\nThe agent_browser tool handles CDP connection, session management, and folder sandboxing automatically. Calling agent-browser CLI directly bypasses CDP URL resolution and will fail inside Docker.",
			ExitCode: 1,
		}, nil
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
	// Docker: VirtioFS auto-mounts /Users/ into containers, so absolute paths bypass sandbox.
	// Native: sandbox-exec profile only restricts workspace-docs subpaths, not the wider
	// filesystem, so this check is needed here too as defense-in-depth.
	if params.FolderGuard != nil && params.FolderGuard.Enabled {
		if err := blockAbsoluteHostPaths(params.Command); err != nil {
			log.Printf("[FOLDER_GUARD] Blocked shell command with absolute host path: %s", params.Command)
			return ShellCommandResult{}, err
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
	// Use a long-timeout HTTP client for shell execution. The default workspace
	// client has a 5-minute timeout (sufficient for file operations), but shell
	// commands can run much longer — e.g., when a Python script wraps a
	// call_sub_agent HTTP call that blocks until the sub-agent completes.
	// Without this, the Go HTTP client times out before the shell finishes,
	// the LLM sees "context deadline exceeded", retries, and spawns duplicate
	// sub-agents while the original is still running on its detached context.
	respBody, err := c.requestWithTimeout(ctx, "POST", path, params, 90*time.Minute)
	if err != nil {
		return ShellCommandResult{}, err
	}

	// Parse the shell response into a typed struct.
	// Infrastructure errors (network, validation) are returned as Go errors above.
	// Command failures (non-zero exit code) are returned in the struct so callers
	// can inspect stdout/stderr to understand what went wrong.
	result := parseShellResponse(respBody)
	// Debug: log result size for call_sub_agent diagnostics
	if result.Stdout != "" && len(result.Stdout) < 200 && strings.Contains(params.Command, "call_sub_agent") {
		log.Printf("[SHELL_RESULT_DEBUG] call_sub_agent via shell: stdout_len=%d raw_resp_len=%d", len(result.Stdout), len(respBody))
	}
	return result, nil
}

// containsAgentBrowserInvocation reports whether cmd appears to invoke the agent-browser
// CLI as an executable, rather than merely mentioning the string "agent-browser" inside
// data regions (heredoc bodies, quoted string literals).
//
// It strips heredoc bodies (<<EOF / <<'EOF' / <<"EOF" / <<-EOF) and single/double-quoted
// string literals, then does a substring check on what remains. This is a guardrail for
// a well-intentioned LLM, not a security boundary — adversarial constructs like
// $(printf agent)-browser are out of scope.
func containsAgentBrowserInvocation(cmd string) bool {
	return strings.Contains(stripShellDataRegions(cmd), "agent-browser")
}

// heredocStartRe matches the start of a heredoc redirection and captures the delimiter.
// Handles <<WORD, <<-WORD, <<'WORD', <<"WORD" (the <<- form strips leading tabs but the
// delimiter word itself is the same).
var heredocStartRe = regexp.MustCompile(`<<-?\s*(?:'([^']+)'|"([^"]+)"|([A-Za-z_][A-Za-z0-9_]*))`)

// stripShellDataRegions removes heredoc bodies and single/double-quoted string literals
// from a shell command, leaving the executable skeleton for substring inspection.
func stripShellDataRegions(cmd string) string {
	var out strings.Builder
	out.Grow(len(cmd))

	i := 0
	n := len(cmd)
	for i < n {
		// Heredoc: <<EOF ... \nEOF\n — strip body through matching delimiter line.
		if i+2 <= n && cmd[i] == '<' && cmd[i+1] == '<' {
			if loc := heredocStartRe.FindStringSubmatchIndex(cmd[i:]); loc != nil && loc[0] == 0 {
				full := cmd[i : i+loc[1]]
				var delim string
				for g := 1; g <= 3; g++ {
					if loc[2*g] >= 0 {
						delim = cmd[i+loc[2*g] : i+loc[2*g+1]]
						break
					}
				}
				out.WriteString(full)
				i += loc[1]
				// Advance to end of the current line (the heredoc body starts on the next line).
				for i < n && cmd[i] != '\n' {
					out.WriteByte(cmd[i])
					i++
				}
				if i < n {
					out.WriteByte('\n')
					i++
				}
				// Consume body up to a line whose trimmed content equals the delimiter.
				for i < n {
					lineStart := i
					for i < n && cmd[i] != '\n' {
						i++
					}
					line := cmd[lineStart:i]
					if i < n {
						i++ // consume newline
					}
					if strings.TrimSpace(line) == delim {
						// Preserve the delimiter line so later logic can still see command structure.
						out.WriteString(line)
						out.WriteByte('\n')
						break
					}
					// Body line — drop.
				}
				continue
			}
		}

		ch := cmd[i]
		// Single-quoted string: everything until the next single quote is literal.
		if ch == '\'' {
			out.WriteByte('\'')
			i++
			for i < n && cmd[i] != '\'' {
				i++
			}
			if i < n {
				out.WriteByte('\'')
				i++
			}
			continue
		}
		// Double-quoted string: strip body but keep the quotes. We don't try to honor
		// $(...) / `...` expansions inside — the guard is a guardrail, not a parser.
		if ch == '"' {
			out.WriteByte('"')
			i++
			for i < n && cmd[i] != '"' {
				if cmd[i] == '\\' && i+1 < n {
					i += 2
					continue
				}
				i++
			}
			if i < n {
				out.WriteByte('"')
				i++
			}
			continue
		}
		out.WriteByte(ch)
		i++
	}
	return out.String()
}

// parseShellResponse parses the workspace API shell response into a typed ShellCommandResult.
func parseShellResponse(respBody []byte) ShellCommandResult {
	var resp map[string]json.RawMessage
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return ShellCommandResult{Stdout: string(respBody)}
	}

	var result ShellCommandResult

	// Extract fields from the "data" envelope
	if data, ok := resp["data"]; ok {
		var dataFields struct {
			Stdout          string  `json:"stdout"`
			Stderr          string  `json:"stderr"`
			ExitCode        int     `json:"exit_code"`
			ExecutionTimeMs float64 `json:"execution_time_ms"`
		}
		if json.Unmarshal(data, &dataFields) == nil {
			result.Stdout = dataFields.Stdout
			result.Stderr = dataFields.Stderr
			result.ExitCode = dataFields.ExitCode
			result.ExecutionTimeMs = dataFields.ExecutionTimeMs
		}
	}

	// Include error if present
	if errVal, ok := resp["error"]; ok {
		var errStr string
		if json.Unmarshal(errVal, &errStr) == nil && errStr != "" {
			result.Error = errStr
		}
	}

	// Unwrap MCP API response from stdout to reduce JSON nesting for the LLM.
	// When the shell command calls an MCP/virtual tool via HTTP (e.g. call_sub_agent),
	// stdout contains: {"success":true,"result":"{...actual JSON...}"}
	// This unwraps it so the LLM sees the actual result directly.
	if result.ExitCode == 0 && result.Error == "" {
		stdout := strings.TrimSpace(result.Stdout)
		if unwrapped := tryUnwrapMCPAPIResponse(stdout); unwrapped != "" {
			result.Stdout = unwrapped
		}
	}

	return result
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
// reference the host filesystem outside the workspace.
//
// In Docker, VirtioFS auto-mounts /Users/ into containers — an LLM-generated
// command like "cat /Users/foo/secret.txt" would bypass workspace isolation.
// This function blocks /Users/, /home/, /root/ to prevent that.
//
// On desktop Mac (WORKSPACE_DOCS_PATH set), the workspace root IS under /Users/,
// so we skip blocking when the workspace root is a subdirectory of a blocked dir.
// The workspace server's own sandbox-exec isolation handles fine-grained access.
func blockAbsoluteHostPaths(command string) error {
	if !strings.Contains(command, "/") {
		return nil
	}

	blockedDirs := []string{
		"/users",
		"/home",
		"/root",
	}

	// If workspace root is under a blocked dir (e.g. /Users/.../workspace-docs on Mac),
	// allow commands that reference paths under the workspace root.
	wsRoot := strings.ToLower(os.Getenv("WORKSPACE_DOCS_PATH"))

	cmdLower := strings.ToLower(command)
	for _, dir := range blockedDirs {
		matched := strings.Contains(cmdLower, dir+"/") ||
			strings.Contains(cmdLower, dir+" ") ||
			strings.HasSuffix(cmdLower, dir)
		if !matched {
			continue
		}
		// If the workspace root is under this blocked dir, skip blocking —
		// the command is likely referencing workspace paths.
		if wsRoot != "" && strings.HasPrefix(wsRoot, dir+"/") {
			continue
		}
		return fmt.Errorf(
			"access denied: shell command references absolute host path (%s). "+
				"Use workspace-relative paths (e.g. 'Workflow/myproject/file.txt') or "+
				"absolute workspace paths instead", dir)
	}
	return nil
}
