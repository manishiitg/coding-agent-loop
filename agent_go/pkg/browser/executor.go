package browser

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"mcp-agent-builder-go/agent_go/pkg/common"
)

// ExecutorOption is a functional option for configuring an Executor
type ExecutorOption func(*Executor)

// WithCdpPort configures the executor to connect to an existing Chrome via CDP on the given port
func WithCdpPort(port int) ExecutorOption {
	return func(e *Executor) {
		e.CdpPort = port
	}
}

// Executor handles the execution of browser tool commands
type Executor struct {
	Client  *Client
	CdpPort int // When > 0, connect to existing Chrome via CDP instead of launching headless
}

// NewExecutor creates a new browser executor
func NewExecutor(client *Client, opts ...ExecutorOption) *Executor {
	e := &Executor{
		Client: client,
	}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// HandleAgentBrowser executes the agent-browser CLI command
func (e *Executor) HandleAgentBrowser(ctx context.Context, args map[string]interface{}) (string, error) {
	command, ok := args["command"].(string)
	if !ok || command == "" {
		return "", fmt.Errorf("command is required")
	}

	// Build command arguments
	var cmdArgs []string

	// Headless mode: add stealth options to avoid bot detection (not needed for CDP/user's real Chrome)
	// CDP mode: agent passes --cdp in args (prompt instructs it to use --cdp http://host.docker.internal:9222)
	// The args processing below resolves host.docker.internal to an IP for Chrome compatibility.
	isCdpMode := e.CdpPort > 0
	if !isCdpMode {
		cmdArgs = append(cmdArgs, "--user-agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
		cmdArgs = append(cmdArgs, "--args", "--disable-blink-features=AutomationControlled")
	}
	log.Printf("[BROWSER] mode=%s command=%s session=%s", map[bool]string{true: "cdp", false: "headless"}[isCdpMode], command, args["session"])

	// Add session flag (required)
	session, ok := args["session"].(string)
	if !ok || session == "" {
		return "", fmt.Errorf("session is required")
	}

	// Get agent-level and workflow-level session IDs from context.
	// agentSessionID = isolated ID for share_browser=false, parent ID for share_browser=true.
	// workflowSessionID = always the parent/root workflow session ID.
	agentSessionID := ""
	if sid, ok := ctx.Value(common.ChatSessionIDKey).(string); ok {
		agentSessionID = sid
	}
	workflowSessionID := ""
	if wid, ok := ctx.Value(common.WorkflowSessionIDKey).(string); ok {
		workflowSessionID = wid
	}
	// Fallback: if no workflow session set, use agent session (non-workflow/chat mode)
	if workflowSessionID == "" {
		workflowSessionID = agentSessionID
	}

	resolvedSession := common.ResolveBrowserSessionID(agentSessionID, session)
	if resolvedSession != "" && resolvedSession != session {
		log.Printf("[BROWSER] remapped session %q -> %q for agent=%q", session, resolvedSession, agentSessionID)
		session = resolvedSession
	}

	log.Printf("[BROWSER] session=%q agent=%q workflow=%q command=%q", session, agentSessionID, workflowSessionID, command)

	// Track headless browser sessions to prevent unbounded growth.
	// CDP mode connects to user's real browser — no tracking needed.
	isHeadless := e.CdpPort <= 0
	tracker := GetSessionTracker()

	if isHeadless {
		isOpenCommand := command == "open" || command == "goto" || command == "navigate"

		if isOpenCommand {
			// Check per-agent and per-workflow limits — return error if exceeded
			if limitMsg := tracker.CheckLimits(session, agentSessionID, workflowSessionID); limitMsg != "" {
				log.Printf("[BROWSER_TRACKER] LIMIT EXCEEDED: browser=%q agent=%q workflow=%q command=%q agent_count=%d wf_count=%d global=%d",
					session, agentSessionID, workflowSessionID, command,
					tracker.CountForAgent(agentSessionID), tracker.CountForWorkflow(workflowSessionID), tracker.Count())
				return limitMsg, nil // Return as tool output, not error — LLM can read and react
			}

			// Check global limit — auto-evict oldest if needed
			if tracker.Count() >= MaxBrowserSessionsGlobal {
				oldest := tracker.GetOldestSession()
				if oldest != "" && oldest != session {
					log.Printf("[BROWSER_TRACKER] Global limit (%d) reached, auto-closing oldest: %q",
						MaxBrowserSessionsGlobal, oldest)
					closeArgs := []string{"--user-agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
						"--args", "--disable-blink-features=AutomationControlled",
						"--session", oldest, "close", "--json"}
					_, closeErr := e.Client.ExecuteCommand(ctx, closeArgs, &ExecuteOptions{Timeout: 10 * time.Second})
					if closeErr != nil {
						log.Printf("[BROWSER_TRACKER] Failed to auto-close session %q: %v", oldest, closeErr)
					}
					tracker.Remove(oldest)
				}
			}

			log.Printf("[BROWSER_TRACKER] Opening browser: browser=%q agent=%q workflow=%q (agent_count=%d, wf_count=%d, global=%d)",
				session, agentSessionID, workflowSessionID,
				tracker.CountForAgent(agentSessionID), tracker.CountForWorkflow(workflowSessionID), tracker.Count())
		}

		// Track the session: only register new sessions on open commands.
		// Non-open commands only update lastUsed if already tracked (prevents
		// non-open commands from bypassing limits by pre-registering sessions).
		if isOpenCommand {
			tracker.Touch(session, agentSessionID, workflowSessionID)
		} else {
			tracker.TouchExisting(session)
		}

		// Log every command for debugging
		log.Printf("[BROWSER_TRACKER] Command: browser=%q agent=%q cmd=%q (agent_count=%d, wf_count=%d, global=%d)",
			session, agentSessionID, command,
			tracker.CountForAgent(agentSessionID), tracker.CountForWorkflow(workflowSessionID), tracker.Count())

		// If closing, remove from tracker
		if command == "close" || command == "quit" || command == "exit" {
			log.Printf("[BROWSER_TRACKER] Closing browser: browser=%q agent=%q", session, agentSessionID)
			defer tracker.Remove(session)
		}
	}

	cmdArgs = append(cmdArgs, "--session", session)

	// Add the command
	cmdArgs = append(cmdArgs, command)

	// Add command arguments if provided
	// In CDP mode: intercept --cdp URL from agent and resolve host.docker.internal to IP
	// If agent forgot --cdp in CDP mode, inject it as fallback
	agentProvidedCdp := false
	if argsArray, ok := args["args"].([]interface{}); ok {
		for i, arg := range argsArray {
			if argStr, ok := arg.(string); ok {
				if argStr == "--cdp" && i+1 < len(argsArray) {
					agentProvidedCdp = true
					// Agent passed --cdp <url> — resolve to proper CDP URL
					cdpURL := resolveCdpURL(e.CdpPort)
					cmdArgs = append(cmdArgs, "--cdp", cdpURL)
					log.Printf("[BROWSER] CDP: agent passed --cdp, resolved to %s", cdpURL)
					// Skip the next arg (the URL value the agent passed)
					continue
				}
				// Skip the URL value after --cdp (already handled above)
				if i > 0 {
					if prevStr, ok := argsArray[i-1].(string); ok && prevStr == "--cdp" {
						continue
					}
				}
				cmdArgs = append(cmdArgs, argStr)
			}
		}
	}
	// Fallback: if CDP mode but agent didn't pass --cdp, inject it
	if isCdpMode && !agentProvidedCdp {
		cdpURL := resolveCdpURL(e.CdpPort)
		cmdArgs = append(cmdArgs, "--cdp", cdpURL)
		log.Printf("[BROWSER] CDP: agent omitted --cdp, injected fallback %s", cdpURL)
	}

	// Always add --json for machine-readable output
	cmdArgs = append(cmdArgs, "--json")

	// Determine timeout
	timeout := getTimeoutForCommand(command)

	// Resolve folder guard and working directory — same priority as execute_shell_command.
	// This ensures agent_browser has identical sandboxing to shell commands.
	var folderGuard *FolderGuardConfig
	workingDir := "."

	// Look up per-session config (working dir + folder guard)
	var sessionCfg *common.SessionShellConfig
	if agentSessionID != "" {
		sessionCfg = common.GetSessionShellConfig(agentSessionID)
	}
	// Fallback: try workflow session if agent session didn't have config
	if sessionCfg == nil && workflowSessionID != "" && workflowSessionID != agentSessionID {
		sessionCfg = common.GetSessionShellConfig(workflowSessionID)
	}

	// Working directory priority: browser downloads path > session config > default
	if downloadsPath, ok := ctx.Value(common.BrowserDownloadsPathKey).(string); ok && downloadsPath != "" {
		workingDir = downloadsPath
	} else if sessionCfg != nil && sessionCfg.WorkingDir != "" {
		workingDir = sessionCfg.WorkingDir
	}

	// Folder guard priority: session config > context system 1 > context system 2
	if sessionCfg != nil && len(sessionCfg.WritePaths) > 0 {
		readPaths := sessionCfg.WritePaths
		if len(sessionCfg.ReadPaths) > 0 {
			readPaths = common.DeduplicateStrings(append(sessionCfg.ReadPaths, sessionCfg.WritePaths...))
		}
		folderGuard = &FolderGuardConfig{
			Enabled:    true,
			WritePaths: sessionCfg.WritePaths,
			ReadPaths:  readPaths,
		}
	} else if allowedWrites, ok := ctx.Value(common.FolderGuardAllowedWriteFolderKey).([]string); ok && len(allowedWrites) > 0 {
		// Context System 1: chat/plan/prototype mode
		ctxReads, hasCtxReads := ctx.Value(common.FolderGuardReadPathsKey).([]string)
		readPaths := allowedWrites
		if hasCtxReads && len(ctxReads) > 0 {
			readPaths = common.DeduplicateStrings(append(ctxReads, allowedWrites...))
		}
		folderGuard = &FolderGuardConfig{
			Enabled:    true,
			WritePaths: allowedWrites,
			ReadPaths:  readPaths,
		}
	} else if ctxWrites, ok := ctx.Value(common.FolderGuardWritePathsKey).([]string); ok && len(ctxWrites) > 0 {
		// Context System 2: workflow orchestrator
		ctxReads, hasCtxReads := ctx.Value(common.FolderGuardReadPathsKey).([]string)
		readPaths := ctxWrites
		if hasCtxReads && len(ctxReads) > 0 {
			readPaths = common.DeduplicateStrings(append(ctxReads, ctxWrites...))
		}
		folderGuard = &FolderGuardConfig{
			Enabled:    true,
			WritePaths: ctxWrites,
			ReadPaths:  readPaths,
		}
	}

	// Execute via client
	opts := &ExecuteOptions{
		Timeout:          timeout,
		FolderGuard:      folderGuard,
		WorkingDirectory: workingDir,
	}
	output, err := e.Client.ExecuteCommand(ctx, cmdArgs, opts)

	// Auto-recover from "CDP response channel closed" — Chrome crashed but the
	// agent-browser runtime is still alive with a dead CDP connection. Remove the
	// session files so the retry starts a fresh runtime + Chrome.
	if err != nil && strings.Contains(err.Error(), "CDP response channel closed") {
		log.Printf("[BROWSER] CDP connection dead for session %q, removing stale session files and retrying", session)
		removeSessionFiles(session)
		output, err = e.Client.ExecuteCommand(ctx, cmdArgs, opts)
	}

	if err != nil {
		return "", err
	}

	return output, nil
}

// resolveCdpURL builds the CDP URL for agent-browser.
// CDP_HOST env var overrides auto-detection.
// In native mode, agent-browser runs on the host so localhost reaches Chrome directly.
// In Docker mode, agent-browser runs inside the workspace container so we use
// host.docker.internal (client.go resolves it to an IP for Chrome compatibility).
func resolveCdpURL(port int) string {
	if host := os.Getenv("CDP_HOST"); host != "" {
		return fmt.Sprintf("http://%s:%d", host, port)
	}
	if common.IsNativeWorkspace() {
		return fmt.Sprintf("http://localhost:%d", port)
	}
	return fmt.Sprintf("http://host.docker.internal:%d", port)
}

// getTimeoutForCommand returns an appropriate timeout based on the command type
func getTimeoutForCommand(command string) time.Duration {
	switch command {
	case "open", "goto", "navigate":
		return 60 * time.Second // Navigation can take longer
	case "screenshot", "pdf":
		return 60 * time.Second // Screenshots/PDFs can take longer
	case "wait":
		return 120 * time.Second // Wait operations may have longer timeouts
	case "close", "quit", "exit":
		return 10 * time.Second // Close should be quick
	default:
		return 30 * time.Second // Default timeout for most operations
	}
}

// removeSessionFiles removes agent-browser session state files (.pid, .sock, etc.)
// so the next command starts a fresh runtime + Chrome instead of connecting to a
// dead one. Does not kill any processes — the orphaned runtime is harmless.
func removeSessionFiles(session string) {
	homeDir, _ := os.UserHomeDir()
	dirs := []string{}
	if homeDir != "" {
		dirs = append(dirs, filepath.Join(homeDir, ".agent-browser"))
	}
	tmpDir := "/tmp/.agent-browser"
	if homeDir == "" || tmpDir != filepath.Join(homeDir, ".agent-browser") {
		dirs = append(dirs, tmpDir)
	}

	for _, dir := range dirs {
		removed := false
		for _, ext := range []string{".pid", ".sock", ".stream", ".engine", ".version"} {
			f := filepath.Join(dir, session+ext)
			if err := os.Remove(f); err == nil {
				removed = true
			}
		}
		if removed {
			log.Printf("[BROWSER] Removed stale session files for %q in %s", session, dir)
		}
	}
}
