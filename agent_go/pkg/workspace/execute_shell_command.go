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

// SetSessionFolderGuardBlockedWritePaths delegates to the common version.
// BlockedWritePaths are the authoritative write-only deny list — they flow
// through to the isolator's FolderGuardConfig.BlockedWritePaths and are enforced
// at kernel sandbox level (sandbox-exec `(deny file-write*)` on macOS,
// read-only bind-mount on Linux). Reads are intentionally still permitted so
// the agent can inspect files (e.g. read plan.json) without the ability to
// modify them — distinct from BlockedPaths which denies reads as well.
func SetSessionFolderGuardBlockedWritePaths(sessionID string, blockedWritePaths []string) {
	common.SetSessionFolderGuardBlockedWritePaths(sessionID, blockedWritePaths)
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

var shellCommandSecretReplacements = []struct {
	pattern     *regexp.Regexp
	replacement string
}{
	{regexp.MustCompile(`(?i)AIza[0-9A-Za-z_-]{20,}`), "[REDACTED]"},
	{regexp.MustCompile(`(?i)sk-api-[0-9A-Za-z_-]{20,}`), "[REDACTED]"},
	{regexp.MustCompile(`(?i)sk_[0-9A-Za-z]{20,}`), "[REDACTED]"},
	{regexp.MustCompile(`(?i)sk-[0-9A-Za-z_-]{20,}`), "[REDACTED]"},
	{regexp.MustCompile(`(?i)(Bearer\s+)[0-9A-Za-z._~+/=-]{20,}`), `${1}[REDACTED]`},
	{regexp.MustCompile(`(?i)((?:api[_-]?key|token|secret|authorization)\s*[:=]\s*["']?)[^"'\\\s,}]{8,}`), `${1}[REDACTED]`},
}

func redactShellCommandForLog(command string) string {
	redacted := command
	for _, replacement := range shellCommandSecretReplacements {
		redacted = replacement.pattern.ReplaceAllString(redacted, replacement.replacement)
	}
	return redacted
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

	// Block agent-browser browser-driving CLI calls via shell — catches direct calls,
	// bash -c wrapping, piping, etc. The agent_browser tool handles CDP URL
	// resolution, session tracking, and folder guard. Calling agent-browser directly
	// via shell bypasses all of this. Read-only built-in skill commands are allowed
	// so agents can load version-matched usage docs from the installed CLI.
	//
	// We strip heredoc bodies and quoted string literals before the substring check so that
	// commands embedding the literal text "agent-browser" as data (e.g. a python3 <<PYEOF
	// script rewriting plan descriptions that mention the CLI, or `echo "use agent-browser"`)
	// don't trip the guard. What remains after stripping is the executable part of the
	// command, where a real invocation would appear.
	cmdTrimmed := strings.TrimSpace(params.Command)
	if containsAgentBrowserInvocation(cmdTrimmed) {
		log.Printf("[SHELL] Blocked agent-browser CLI call. Command: %s", redactShellCommandForLog(params.Command))

		// Context-aware error: guide LLM to the correct browser tool
		browserMode := ""
		if sessionCfg != nil {
			browserMode = sessionCfg.BrowserMode
		}
		if browserMode == "playwright" {
			toolName := "Playwright browser_* tools (browser_snapshot, browser_click, browser_type, etc.)"
			return ShellCommandResult{
				Stderr:   "ERROR: Do not call agent-browser via execute_shell_command. Use the " + toolName + " for browser automation.\n\nThe agent-browser CLI is not the correct tool for this workflow. Start with browser_snapshot to see the current page state, then use the appropriate browser_* tool for interactions.",
				ExitCode: 1,
			}, nil
		}
		return ShellCommandResult{
			Stderr:   "ERROR: Do not call agent-browser directly via execute_shell_command for browser actions. Use the agent_browser tool instead.\n\nAllowed shell exception for docs only:\n  agent-browser skills list\n  agent-browser skills get core\n  agent-browser skills get core --full\n\nFor direct tool call mode:\n  agent_browser(command=\"open\", args=[\"https://example.com\"], session=\"default\")\n\nFor code execution mode (MCP bridge):\n  Call get_api_spec(server_name=\"agent_browser\") to get the HTTP API spec,\n  then POST to the agent_browser endpoint via HTTP.\n\nThe agent_browser tool handles CDP connection, session management, and folder sandboxing automatically. Calling agent-browser CLI directly for browser actions bypasses CDP URL resolution and will fail inside Docker.",
			ExitCode: 1,
		}, nil
	}

	if rawCDP := detectRawChromeCDPAccess(params.Command); rawCDP != "" {
		log.Printf("[SHELL] Blocked raw Chrome CDP access via shell (%s). Command: %s", rawCDP, redactShellCommandForLog(params.Command))
		return ShellCommandResult{
			Stderr:   fmt.Sprintf("ERROR: Raw Chrome CDP access via execute_shell_command is blocked (%s).\n\nUse the agent_browser tool instead so shared-CDP tab selection and locking are enforced.\n\nAllowed replacements:\n  - Selected tab hint: agent_browser(command=\"tab\", args=[], session=\"<session>\")\n  - Select/create a tab: agent_browser(command=\"tab\", args=[\"<tab-id-or-label>\"], session=\"<session>\") or agent_browser(command=\"tab\", args=[\"new\", \"--label\", \"<label>\", \"https://example.com\"], session=\"<session>\")\n  - Navigate: agent_browser(command=\"open\", args=[\"https://example.com\"], session=\"<session>\")\n  - Run JavaScript: agent_browser(command=\"eval\", args=[\"tab\", \"<tab-id-or-label>\", \"document.title\"], session=\"<session>\")\n\nDo not use ws://localhost:9222/devtools/page, http://localhost:9222/json/*, websocket.create_connection(...9222...), or direct CDP Target/Runtime calls from shell. Those bypass the shared browser lock and can interfere with other running workflows.", rawCDP),
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
	redactedCommandForLog := redactShellCommandForLog(params.Command)

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
	if sessionCfg != nil && (len(sessionCfg.ReadPaths) > 0 || len(sessionCfg.WritePaths) > 0) {
		// Session config: set by SetSessionFolderGuard() — highest priority.
		// Covers CLI/Gemini providers that bypass the Go folder guard context wrappers.
		// BlockedWritePaths flow through as the write-only deny list enforced by the
		// isolator at kernel-sandbox level (reads still permitted), independent of
		// WritePaths overlap.
		readPaths := sessionCfg.WritePaths
		if len(sessionCfg.ReadPaths) > 0 {
			readPaths = deduplicateStrings(append(sessionCfg.ReadPaths, sessionCfg.WritePaths...))
		}
		params.FolderGuard = &FolderGuardConfig{
			Enabled:           true,
			WritePaths:        sessionCfg.WritePaths,
			ReadPaths:         readPaths,
			BlockedWritePaths: sessionCfg.BlockedWritePaths,
		}
		log.Printf("[FOLDER_GUARD_RESOLVE] SessionConfig: session=%s WritePaths=%v ReadPaths=%v BlockedWritePaths=%v cmd=%s", sessionID, sessionCfg.WritePaths, readPaths, sessionCfg.BlockedWritePaths, redactedCommandForLog)
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
		log.Printf("[FOLDER_GUARD_RESOLVE] System1 (chat/plan/prototype): URL=%s WritePaths=%v ReadPaths=%v cmd=%s", c.BaseURL, allowedWrites, readPaths, redactedCommandForLog)
	} else if ctxWrites, ok := ctx.Value(common.FolderGuardWritePathsKey).([]string); ok {
		// Context System 2: workflow orchestrator
		ctxReads, hasCtxReads := ctx.Value(common.FolderGuardReadPathsKey).([]string)
		if len(ctxWrites) == 0 && (!hasCtxReads || len(ctxReads) == 0) {
			log.Printf("[FOLDER_GUARD_RESOLVE] NO folder guard at all: URL=%s cmd=%s", c.BaseURL, redactedCommandForLog)
		} else {
			readPaths := ctxWrites
			if hasCtxReads && len(ctxReads) > 0 {
				readPaths = deduplicateStrings(append(ctxReads, ctxWrites...))
			}
			params.FolderGuard = &FolderGuardConfig{
				Enabled:    true,
				WritePaths: ctxWrites,
				ReadPaths:  readPaths,
			}
			log.Printf("[FOLDER_GUARD_RESOLVE] System2 (workflow): URL=%s WritePaths=%v ReadPaths=%v cmd=%s", c.BaseURL, ctxWrites, readPaths, redactedCommandForLog)
		}
	} else if c.FolderGuard != nil && c.FolderGuard.Enabled {
		// Client-level fallback — no session config, no context keys.
		params.FolderGuard = c.FolderGuard
		log.Printf("[FOLDER_GUARD_RESOLVE] FALLBACK to client-level guard: URL=%s ReadPaths=%v WritePaths=%v BlockedPaths=%v cmd=%s",
			c.BaseURL, c.FolderGuard.ReadPaths, c.FolderGuard.WritePaths, c.FolderGuard.BlockedPaths, redactedCommandForLog)
	} else {
		log.Printf("[FOLDER_GUARD_RESOLVE] NO folder guard at all: URL=%s cmd=%s", c.BaseURL, redactedCommandForLog)
	}

	// Block absolute host paths when folder guard is active.
	// Docker: VirtioFS auto-mounts /Users/ into containers, so absolute paths bypass sandbox.
	// Native: sandbox-exec profile only restricts workspace-docs subpaths, not the wider
	// filesystem, so this check is needed here too as defense-in-depth.
	if params.FolderGuard != nil && params.FolderGuard.Enabled {
		if err := blockAbsoluteHostPaths(params.Command, params.FolderGuard); err != nil {
			log.Printf("[FOLDER_GUARD] Blocked shell command with absolute host path: %s", redactedCommandForLog)
			return ShellCommandResult{}, err
		}
	}

	// Inject per-session env vars (e.g. DB_PATH, STEP_OUTPUT_DIR set by the
	// workflow orchestrator). The server-side bridge shell has no other channel
	// for these — unlike the in-process built-in executor, which is wrapped to
	// inject them. Explicit per-call extra_env wins; session env wins over the
	// client-level vars merged below.
	if sessionEnv := common.GetSessionShellEnv(sessionID); len(sessionEnv) > 0 {
		if params.ExtraEnv == nil {
			params.ExtraEnv = make(map[string]string)
		}
		for k, v := range sessionEnv {
			if _, exists := params.ExtraEnv[k]; !exists {
				params.ExtraEnv[k] = v
			}
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
	common.PopulateMCPBridgeShortEnv(params.ExtraEnv)

	path := "/api/execute"
	// Use a no-timeout HTTP client for shell execution. Shell commands can run
	// much longer than normal file operations, especially when they wrap a
	// call_sub_agent HTTP call that blocks until the sub-agent completes.
	respBody, err := c.requestWithTimeout(ctx, "POST", path, params, 0)
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
	executableText := strings.TrimSpace(stripShellDataRegions(cmd))
	if isAgentBrowserSkillsDocsCommand(executableText) {
		return false
	}
	return strings.Contains(executableText, "agent-browser")
}

// isAgentBrowserSkillsDocsCommand allows read-only agent-browser skill discovery.
// Browser-driving commands still must go through the managed agent_browser tool.
func isAgentBrowserSkillsDocsCommand(cmd string) bool {
	fields := strings.Fields(cmd)
	if len(fields) < 3 || fields[0] != "agent-browser" || fields[1] != "skills" {
		return false
	}
	switch fields[2] {
	case "list":
		return len(fields) == 3
	case "get":
		if len(fields) < 4 || strings.HasPrefix(fields[3], "-") {
			return false
		}
		for _, flag := range fields[4:] {
			if flag != "--full" {
				return false
			}
		}
		return true
	default:
		return false
	}
}

// detectRawChromeCDPAccess reports shell commands/scripts that talk directly to
// Chrome's remote debugging endpoint.
//
// Keep this narrow. Workflow steps often write plan/docs/scripts that mention CDP
// URLs as plain data. Those should pass. We only inspect executable shell text
// plus heredocs fed directly to an interpreter, and we require a CDP endpoint (or
// CDP URL variable) to appear together with code that actually opens or drives it.
func detectRawChromeCDPAccess(cmd string) string {
	if reason := detectRawChromeCDPInExecutableText(stripShellHeredocBodies(cmd)); reason != "" {
		return reason
	}
	for _, body := range executableHeredocBodies(cmd) {
		if reason := detectRawChromeCDPInExecutableText(body); reason != "" {
			return reason
		}
	}
	return ""
}

func detectRawChromeCDPInExecutableText(text string) string {
	lower := strings.ToLower(text)
	hasDevToolsPage := strings.Contains(lower, "ws://localhost:9222/devtools/page") ||
		strings.Contains(lower, "ws://127.0.0.1:9222/devtools/page") ||
		strings.Contains(lower, "ws://host.docker.internal:9222/devtools/page") ||
		strings.Contains(lower, "/devtools/page/")
	hasJSONEndpoint := strings.Contains(lower, "http://localhost:9222/json/") ||
		strings.Contains(lower, "http://127.0.0.1:9222/json/") ||
		strings.Contains(lower, "http://host.docker.internal:9222/json/") ||
		strings.Contains(lower, "localhost:9222/json/") ||
		strings.Contains(lower, "127.0.0.1:9222/json/") ||
		strings.Contains(lower, "host.docker.internal:9222/json/") ||
		strings.Contains(lower, "/json/list") ||
		strings.Contains(lower, "/json/version") ||
		strings.Contains(lower, "/json/new") ||
		strings.Contains(lower, "/json/close")
	hasCDPVariable := strings.Contains(lower, "var_twitter_cdp_url") ||
		strings.Contains(lower, "twitter_cdp_url") ||
		strings.Contains(lower, "cdp_url")
	hasCDPEndpointOrVariable := strings.Contains(lower, ":9222") ||
		strings.Contains(lower, "/devtools/page") ||
		strings.Contains(lower, "/json/list") ||
		strings.Contains(lower, "/json/version") ||
		strings.Contains(lower, "/json/new") ||
		strings.Contains(lower, "/json/close") ||
		hasCDPVariable

	hasWSOpen := strings.Contains(lower, "websocket.create_connection") ||
		strings.Contains(lower, "create_connection(") ||
		strings.Contains(lower, "new websocket(") ||
		strings.Contains(lower, "websocat ") ||
		strings.Contains(lower, "wscat ")
	if hasWSOpen && (hasDevToolsPage || hasCDPVariable) {
		return "raw CDP WebSocket connection"
	}

	hasHTTPFetch := strings.Contains(lower, "curl ") ||
		strings.Contains(lower, "curl\t") ||
		strings.Contains(lower, "wget ") ||
		strings.Contains(lower, "urlopen(") ||
		strings.Contains(lower, "requests.get(") ||
		strings.Contains(lower, "requests.post(") ||
		strings.Contains(lower, "httpx.get(") ||
		strings.Contains(lower, "httpx.post(") ||
		strings.Contains(lower, "fetch(") ||
		strings.Contains(lower, "axios.get(") ||
		strings.Contains(lower, "axios.post(") ||
		strings.Contains(lower, "http.get(") ||
		strings.Contains(lower, "https.get(")
	if hasHTTPFetch && (hasJSONEndpoint || hasCDPVariable) {
		return "Chrome /json target endpoint"
	}

	hasCDPMethod := strings.Contains(lower, "target.createtarget") ||
		strings.Contains(lower, "target.closetarget") ||
		strings.Contains(lower, "runtime.evaluate") ||
		strings.Contains(lower, "page.navigate") ||
		strings.Contains(lower, "input.dispatchmouseevent") ||
		strings.Contains(lower, "input.dispatchkeyevent")
	if hasCDPMethod && hasCDPEndpointOrVariable {
		return "Chrome CDP method call"
	}
	return ""
}

var executableHeredocCommandRe = regexp.MustCompile(`(?i)(^|[;&|]\s*)(python3?|node|ruby|perl|php|bash|sh|zsh|deno|tsx|npx\s+(?:tsx|ts-node))\b`)

func executableHeredocBodies(cmd string) []string {
	var bodies []string
	i := 0
	n := len(cmd)
	for i < n {
		if i+2 <= n && cmd[i] == '<' && cmd[i+1] == '<' {
			if loc := heredocStartRe.FindStringSubmatchIndex(cmd[i:]); loc != nil && loc[0] == 0 {
				lineStart := strings.LastIndex(cmd[:i], "\n") + 1
				prefix := strings.TrimSpace(cmd[lineStart:i])
				fullEnd := i + loc[1]
				var delim string
				for g := 1; g <= 3; g++ {
					if loc[2*g] >= 0 {
						delim = cmd[i+loc[2*g] : i+loc[2*g+1]]
						break
					}
				}

				i = fullEnd
				for i < n && cmd[i] != '\n' {
					i++
				}
				if i < n {
					i++
				}
				bodyStart := i
				bodyEnd := i
				for i < n {
					lineStart := i
					for i < n && cmd[i] != '\n' {
						i++
					}
					line := cmd[lineStart:i]
					if strings.TrimSpace(line) == delim {
						bodyEnd = lineStart
						if i < n {
							i++
						}
						break
					}
					if i < n {
						i++
					}
					bodyEnd = i
				}
				if executableHeredocCommandRe.MatchString(prefix) {
					bodies = append(bodies, cmd[bodyStart:bodyEnd])
				}
				continue
			}
		}
		i++
	}
	return bodies
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
// On desktop Mac the workspace root itself is also under /Users/, so absolute
// paths under workspace-docs must be allowed through. The workspace API
// receives the same FolderGuardConfig and enforces read/write restrictions in
// the sandbox layer; doing a second path-level rejection here turns expected
// sandbox denials into provider-level tool-call errors that agents cannot catch.
func blockAbsoluteHostPaths(command string, guard *FolderGuardConfig) error {
	if !strings.Contains(command, "/") || guard == nil {
		return nil
	}

	candidates := extractAbsoluteShellPaths(command)
	if len(candidates) == 0 {
		return nil
	}

	blockedDirs := []string{
		"/users",
		"/home",
		"/root",
	}

	for _, candidate := range candidates {
		if _, ok := normalizeAbsoluteWorkspacePath(candidate); ok {
			continue
		}

		candidateLower := strings.ToLower(candidate)
		for _, dir := range blockedDirs {
			if candidateLower == dir || strings.HasPrefix(candidateLower, dir+"/") {
				roots := workspaceDocsRoots()
				suggestion := closestWorkspaceRootHint(candidate, roots)
				return fmt.Errorf(
					"access denied: shell command references absolute host path (%s) that is not under any allowed workspace root. "+
						"Allowed workspace roots: %s. "+
						"%s"+
						"Use workspace-relative paths (e.g. 'Workflow/myproject/file.txt') or absolute paths under one of the allowed roots.",
					candidate, strings.Join(roots, ", "), suggestion,
				)
			}
		}
	}
	return nil
}

// closestWorkspaceRootHint returns a "Did you mean" hint when the rejected
// path's tail (everything after the first /Workflow, /skills, /Chats, etc.)
// could plausibly fit under one of the allowed workspace roots. Returns ""
// when no useful suggestion can be made — the caller passes through whatever
// it gets and the message reads cleanly either way.
func closestWorkspaceRootHint(rejected string, roots []string) string {
	clean := filepath.Clean(rejected)
	// Look for the first known top-level workspace folder name in the rejected
	// path; if found, the tail is what we'd append to a real root.
	knownTops := []string{"/Workflow/", "/skills/", "/Chats/", "/Downloads/", "/subagents/", "/memory/"}
	for _, top := range knownTops {
		if idx := strings.Index(clean, top); idx >= 0 {
			tail := clean[idx:] // e.g. "/Workflow/linkedin/..."
			for _, root := range roots {
				if root == "" {
					continue
				}
				return fmt.Sprintf("Did you mean: %s%s? ", filepath.Clean(root), tail)
			}
		}
	}
	return ""
}

func normalizeAbsoluteWorkspacePath(inputPath string) (string, bool) {
	cleanPath := filepath.Clean(inputPath)
	for _, root := range workspaceDocsRoots() {
		if root == "" {
			continue
		}
		root = filepath.Clean(root)
		if cleanPath == root {
			return ".", true
		}
		prefix := root + string(filepath.Separator)
		if strings.HasPrefix(cleanPath, prefix) {
			return strings.TrimPrefix(cleanPath, prefix), true
		}
	}
	return "", false
}

func workspaceDocsRoots() []string {
	roots := make([]string, 0, 8)
	if envRoot := strings.TrimSpace(os.Getenv("WORKSPACE_DOCS_PATH")); envRoot != "" {
		roots = append(roots, envRoot)
	}
	if cwd, err := os.Getwd(); err == nil {
		for dir := filepath.Clean(cwd); ; dir = filepath.Dir(dir) {
			candidate := filepath.Join(dir, "workspace-docs")
			if info, statErr := os.Stat(candidate); statErr == nil && info.IsDir() {
				roots = append(roots, candidate)
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
		}
	}
	roots = append(roots, "/app/workspace-docs", "/workspace-docs")
	return deduplicateStrings(roots)
}

func extractAbsoluteShellPaths(command string) []string {
	sanitized := stripShellHeredocBodies(command)
	seen := make(map[string]struct{})
	var paths []string

	addPath := func(raw string) {
		raw = strings.TrimSpace(raw)
		raw = strings.TrimRight(raw, ",)")
		if raw == "" || !filepath.IsAbs(raw) {
			return
		}
		clean := filepath.Clean(raw)
		if _, exists := seen[clean]; exists {
			return
		}
		seen[clean] = struct{}{}
		paths = append(paths, clean)
	}

	for i := 0; i < len(sanitized); {
		switch sanitized[i] {
		case '\'':
			start := i + 1
			i++
			for i < len(sanitized) && sanitized[i] != '\'' {
				i++
			}
			if start < len(sanitized) {
				addPath(sanitized[start:i])
			}
			if i < len(sanitized) {
				i++
			}
		case '"':
			start := i + 1
			i++
			for i < len(sanitized) && sanitized[i] != '"' {
				if sanitized[i] == '\\' && i+1 < len(sanitized) {
					i += 2
					continue
				}
				i++
			}
			if start < len(sanitized) {
				addPath(sanitized[start:i])
			}
			if i < len(sanitized) {
				i++
			}
		default:
			if sanitized[i] == '/' && (i == 0 || isShellPathBoundary(sanitized[i-1])) {
				start := i
				i++
				for i < len(sanitized) && !isShellPathTerminator(sanitized[i]) {
					i++
				}
				addPath(sanitized[start:i])
				continue
			}
			i++
		}
	}

	return paths
}

func isShellPathBoundary(ch byte) bool {
	switch ch {
	case ' ', '\t', '\n', '\r', '=', '(', ':', '<', '>':
		return true
	default:
		return false
	}
}

func isShellPathTerminator(ch byte) bool {
	switch ch {
	case ' ', '\t', '\n', '\r', '\'', '"', '|', '&', ';', '<', '>', ')':
		return true
	default:
		return false
	}
}

func stripShellHeredocBodies(cmd string) string {
	var out strings.Builder
	out.Grow(len(cmd))

	i := 0
	n := len(cmd)
	for i < n {
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
				for i < n && cmd[i] != '\n' {
					out.WriteByte(cmd[i])
					i++
				}
				if i < n {
					out.WriteByte('\n')
					i++
				}
				for i < n {
					lineStart := i
					for i < n && cmd[i] != '\n' {
						i++
					}
					line := cmd[lineStart:i]
					if i < n {
						i++
					}
					if strings.TrimSpace(line) == delim {
						out.WriteString(line)
						out.WriteByte('\n')
						break
					}
				}
				continue
			}
		}
		out.WriteByte(cmd[i])
		i++
	}
	return out.String()
}
