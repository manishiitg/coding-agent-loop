package browser

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"mcp-agent-builder-go/agent_go/pkg/common"
)

// execCmd is an alias so tests can swap it out; defaults to exec.Command.
var execCmd = exec.Command

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

	// Headless mode: add anti-detection launch options (not needed for CDP/user's real Chrome)
	// CDP mode: agent passes --cdp in args (prompt instructs it to use --cdp http://host.docker.internal:9222)
	// The args processing below resolves host.docker.internal to an IP for Chrome compatibility.
	isCdpMode := e.CdpPort > 0
	if !isCdpMode {
		cmdArgs = append(cmdArgs, "--user-agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
		// --no-sandbox removes the sandbox broker process (~100MB saved)
		// --disable-gpu removes the GPU compositor process (~80MB saved)
		// Together these cut Chrome's launch footprint roughly in half, which matters
		// on memory-constrained hosts where Chrome is otherwise OOM-killed on startup.
		cmdArgs = append(cmdArgs, "--args", "--no-sandbox,--disable-gpu,--disable-blink-features=AutomationControlled")
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

	if isCdpMode {
		sharedSession := sharedCDPSessionName(e.CdpPort)
		if session != sharedSession {
			log.Printf("[BROWSER] CDP: remapped session %q -> %q for shared browser port %d", session, sharedSession, e.CdpPort)
			session = sharedSession
		}
	} else {
		resolvedSession := common.ResolveBrowserSessionID(agentSessionID, session)
		if resolvedSession != "" && resolvedSession != session {
			log.Printf("[BROWSER] remapped session %q -> %q for agent=%q", session, resolvedSession, agentSessionID)
			session = resolvedSession
		}
	}

	log.Printf("[BROWSER] session=%q agent=%q workflow=%q command=%q", session, agentSessionID, workflowSessionID, command)

	// Track headless browser sessions to prevent unbounded growth.
	// CDP mode connects to user's real browser — no tracking needed.
	isHeadless := e.CdpPort <= 0
	tracker := GetSessionTracker()

	if isHeadless {
		isOpenCommand := command == "open" || command == "goto" || command == "navigate"

		if isOpenCommand {
			// Auto-evict helper: kills runtime + removes files + unregisters from tracker.
			// Used for both per-agent and global eviction so the new session can proceed
			// without the LLM needing to close things manually.
			autoEvict := func(evictSession string) {
				log.Printf("[BROWSER_TRACKER] Auto-evicting session %q to free slot for %q", evictSession, session)
				killSessionRuntime(evictSession)
				removeSessionFiles(evictSession)
				tracker.Remove(evictSession)
			}

			// Per-agent limit: if this agent already has too many sessions, evict its
			// oldest one so the new open can proceed. Mirrors the global-evict behavior.
			if agentSessionID != "" && tracker.CountForAgent(agentSessionID) >= MaxBrowserSessionsPerAgent {
				oldest := tracker.GetOldestSessionForAgent(agentSessionID)
				if oldest != "" && oldest != session {
					autoEvict(oldest)
				}
			}

			// Per-workflow limit: evict oldest workflow session if needed.
			if workflowSessionID != "" && tracker.CountForWorkflow(workflowSessionID) >= MaxBrowserSessionsPerWorkflow {
				// Find oldest workflow session that isn't the one being opened.
				wfSessions := tracker.SessionsForWorkflow(workflowSessionID)
				var oldestWf string
				for _, s := range wfSessions {
					if s != session {
						oldestWf = s
						break
					}
				}
				if oldestWf != "" {
					autoEvict(oldestWf)
				}
			}

			// Check global limit — auto-evict oldest if needed
			if tracker.Count() >= MaxBrowserSessionsGlobal {
				oldest := tracker.GetOldestSession()
				if oldest != "" && oldest != session {
					log.Printf("[BROWSER_TRACKER] Global limit (%d) reached, auto-closing oldest: %q",
						MaxBrowserSessionsGlobal, oldest)
					autoEvict(oldest)
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

	argsArray := stringArgs(args["args"])
	tabArgs := stripCDPArgs(argsArray)
	commandArgs := argsArray
	inlineCDPTab := ""
	if isCdpMode {
		var inlineErr error
		inlineCDPTab, commandArgs, inlineErr = extractInlineCDPTab(tabArgs)
		if inlineErr != nil {
			return "", inlineErr
		}
	}

	for _, argStr := range commandArgs {
		cmdArgs = append(cmdArgs, argStr)
	}
	if isCdpMode {
		cdpURL := resolveCdpURL(e.CdpPort)
		cmdArgs = append(cmdArgs, "--cdp", cdpURL)
		log.Printf("[BROWSER] CDP: using %s", cdpURL)
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

	cdpOwner := ""
	if isCdpMode {
		cdpOwner = cdpOwnerID(workflowSessionID, agentSessionID, session)
		lock := sharedCDPLock(e.CdpPort)
		lock.Lock()
		defer lock.Unlock()

		if command == "reset" {
			clearCDPTabSelectionsForPort(e.CdpPort)
		} else if command == "tab" {
			if _, _, err := parseTabSelection(tabArgs); err != nil {
				return "", err
			}
		} else {
			if inlineCDPTab == "" {
				tabsOutput, tabsErr := e.listCDPTabs(ctx, session, opts)
				if tabsErr != nil {
					return "", fmt.Errorf("CDP shared-browser mode requires a tab in args before %q, but listing tabs failed: %w", command, tabsErr)
				}
				return "", fmt.Errorf("CDP shared-browser mode requires every page action to include a tab before %q.\n\nExisting tabs:\n%s\n\nUse args like [\"tab\", \"<tab-id-or-label>\", ...commandArgs] or [\"--tab\", \"<tab-id-or-label>\", ...commandArgs]. To inspect tabs, call agent_browser(command=\"tab\", args=[]). To create a labeled tab, call agent_browser(command=\"tab\", args=[\"new\", \"--label\", \"<label>\", \"<url>\"])", command, strings.TrimSpace(tabsOutput))
			}
			if getCDPActiveTab(e.CdpPort) == inlineCDPTab {
				log.Printf("[BROWSER] CDP: inline tab %q already active before %q; skipping select", inlineCDPTab, command)
			} else {
				if err := e.selectCDPTab(ctx, session, inlineCDPTab, opts); err != nil {
					return "", e.cdpTabSelectionError(ctx, session, opts, inlineCDPTab, command, err)
				}
				setCDPActiveTab(e.CdpPort, inlineCDPTab)
				log.Printf("[BROWSER] CDP: selected inline tab %q before %q", inlineCDPTab, command)
			}
		}
	}

	// reset: force-kill the daemon and wipe session files without touching the
	// agent-browser binary. Works even when CDP is dead. Use this when open/close
	// keep failing — it gives a completely clean slate for the next open.
	if command == "reset" {
		log.Printf("[BROWSER] Reset requested for session %q — killing daemon and clearing state", session)
		killSessionRuntime(session)
		removeSessionFiles(session)
		if isHeadless {
			tracker.Remove(session)
		}
		return `{"success":true,"message":"session reset — ready for fresh open"}`, nil
	}

	output, err := e.Client.ExecuteCommand(ctx, cmdArgs, opts)

	// After a successful open/navigate, record Chrome's PID so killSessionRuntime can
	// kill it reliably even if Chrome has been reparented (daemon auto-relaunch race).
	isOpenCommand := command == "open" || command == "goto" || command == "navigate"
	if err == nil && isOpenCommand {
		captureChromePID(session)
	}

	// isDeadSession returns true for errors that mean the browser session no longer
	// exists and we need to start fresh. Both error strings come from agent-browser itself:
	//   "CDP response channel closed"   — Chrome crashed while connected
	//   "No such file or directory"     — daemon socket was deleted (e.g. after a reset
	//                                     that ran concurrently with this command)
	isDeadSession := func(e error) bool {
		if e == nil {
			return false
		}
		msg := e.Error()
		return strings.Contains(msg, "CDP response channel closed") ||
			strings.Contains(msg, "No such file or directory")
	}

	// Auto-recover from dead sessions — Chrome crashed or the daemon socket was removed.
	// Kill the daemon process first (so it releases its port/socket), then
	// remove session files so the retry starts a completely fresh runtime + Chrome.
	if isDeadSession(err) {
		log.Printf("[BROWSER] Dead session detected for %q (%v), killing stale runtime and retrying", session, err)
		killSessionRuntime(session)
		removeSessionFiles(session)
		// If this was a close command, there's nothing left to close — declare success.
		if command == "close" || command == "quit" || command == "exit" {
			log.Printf("[BROWSER] Session %q already dead, treating close as success", session)
			return `{"success":true,"message":"session already closed"}`, nil
		}
		// Brief pause so the OS can reclaim memory from the killed Chrome/daemon
		// before we launch a new Chrome. Without this, the retry often hits OOM
		// immediately because the killed process hasn't fully released its pages yet.
		time.Sleep(2 * time.Second)
		if isCdpMode && command != "tab" && command != "reset" {
			if inlineCDPTab != "" {
				if getCDPActiveTab(e.CdpPort) == inlineCDPTab {
					log.Printf("[BROWSER] CDP: inline tab %q already active before retrying %q; skipping select", inlineCDPTab, command)
				} else {
					if selectErr := e.selectCDPTab(ctx, session, inlineCDPTab, opts); selectErr != nil {
						return "", e.cdpTabSelectionError(ctx, session, opts, inlineCDPTab, command, selectErr)
					}
					setCDPActiveTab(e.CdpPort, inlineCDPTab)
					log.Printf("[BROWSER] CDP: re-selected inline tab %q before retrying %q", inlineCDPTab, command)
				}
			}
		}
		output, err = e.Client.ExecuteCommand(ctx, cmdArgs, opts)
		// On successful retry, record Chrome PID for the fresh session.
		if err == nil && isOpenCommand {
			captureChromePID(session)
		}
	}

	// If the final error is a dead session on an open command, remove it from the
	// tracker immediately. Otherwise it sits tracked-but-dead and blocks the next
	// open attempt with LIMIT EXCEEDED until the LLM remembers to call close.
	if err != nil && isOpenCommand && isDeadSession(err) && isHeadless {
		log.Printf("[BROWSER_TRACKER] open failed with dead session for %q — removing from tracker so next open isn't blocked", session)
		tracker.Remove(session)
	}

	if err != nil {
		return "", err
	}

	if isCdpMode && command == "tab" {
		if tab, clear, parseErr := parseTabSelection(tabArgs); parseErr == nil {
			if clear {
				if tab == "" || tab == getCDPTabSelection(e.CdpPort, cdpOwner) {
					clearCDPTabSelection(e.CdpPort, cdpOwner)
					log.Printf("[BROWSER] CDP: cleared selected tab for owner=%q port=%d", cdpOwner, e.CdpPort)
				}
				clearCDPActiveTab(e.CdpPort, tab)
			} else if tab != "" {
				setCDPTabSelection(e.CdpPort, cdpOwner, tab)
				setCDPActiveTab(e.CdpPort, tab)
				log.Printf("[BROWSER] CDP: selected tab %q for owner=%q port=%d", tab, cdpOwner, e.CdpPort)
			}
		}
	}

	return output, nil
}

func (e *Executor) listCDPTabs(ctx context.Context, session string, opts *ExecuteOptions) (string, error) {
	return e.Client.ExecuteCommand(ctx, []string{
		"--session", session,
		"tab",
		"--cdp", resolveCdpURL(e.CdpPort),
		"--json",
	}, opts)
}

func (e *Executor) selectCDPTab(ctx context.Context, session, tab string, opts *ExecuteOptions) error {
	_, err := e.Client.ExecuteCommand(ctx, []string{
		"--session", session,
		"tab", tab,
		"--cdp", resolveCdpURL(e.CdpPort),
		"--json",
	}, opts)
	return err
}

func (e *Executor) cdpTabSelectionError(ctx context.Context, session string, opts *ExecuteOptions, tab, command string, selectErr error) error {
	tabsOutput, tabsErr := e.listCDPTabs(ctx, session, opts)
	if tabsErr != nil {
		return fmt.Errorf("failed to select CDP tab %q before %q: %w; listing tabs also failed: %v", tab, command, selectErr, tabsErr)
	}
	return fmt.Errorf("failed to select CDP tab %q before %q: %w\n\nExisting tabs:\n%s\n\nChoose an existing tab by id/label in args like [\"tab\", \"<tab-id-or-label>\", ...commandArgs], or create a labeled tab with agent_browser(command=\"tab\", args=[\"new\", \"--label\", \"<label>\", \"<url>\"])", tab, command, selectErr, strings.TrimSpace(tabsOutput))
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

// sessionDirs returns the directories where agent-browser stores session files.
func sessionDirs() []string {
	homeDir, _ := os.UserHomeDir()
	dirs := []string{}
	if homeDir != "" {
		dirs = append(dirs, filepath.Join(homeDir, ".agent-browser"))
	}
	tmpDir := "/tmp/.agent-browser"
	if homeDir == "" || tmpDir != filepath.Join(homeDir, ".agent-browser") {
		dirs = append(dirs, tmpDir)
	}
	return dirs
}

// captureChromePID records Chrome's PID into a .chrome-pid file right after a
// successful open command — when Chrome is guaranteed alive and still a child of
// the daemon. This lets killSessionRuntime use the stored PID later, avoiding the
// PPID timing race where Chrome has already been reparented or re-launched.
//
// Topology (verified against agent-browser v0.26): each session spawns its own
// daemon (PPID=1) which spawns exactly one Chrome child. Daemons and Chromes
// are not shared across sessions, so kids[0] is always this session's Chrome.
func captureChromePID(session string) {
	for _, dir := range sessionDirs() {
		pidFile := filepath.Join(dir, session+".pid")
		pidBytes, err := os.ReadFile(pidFile)
		if err != nil {
			continue
		}
		daemonPID, err := strconv.Atoi(strings.TrimSpace(string(pidBytes)))
		if err != nil {
			continue
		}
		kids := childPIDs(daemonPID)
		if len(kids) == 0 {
			continue
		}
		// One daemon = one Chrome child (see function doc).
		chromePID := kids[0]
		chromePIDFile := filepath.Join(dir, session+".chrome-pid")
		if err := os.WriteFile(chromePIDFile, []byte(strconv.Itoa(chromePID)), 0600); err == nil {
			log.Printf("[BROWSER] Recorded Chrome PID %d for session %q", chromePID, session)
		}
		return
	}
}

// killChromePID kills Chrome's process group using the given PID.
// Chrome's PGID equals its own PID, so killing -PGID takes the entire Chrome tree
// (GPU, renderer, network helpers) in one shot.
func killChromePID(chromePID int, session string) {
	if err := syscall.Kill(-chromePID, syscall.SIGKILL); err != nil {
		// Fallback: kill just the process if group kill fails
		if p, err2 := os.FindProcess(chromePID); err2 == nil {
			p.Signal(syscall.SIGKILL) //nolint:errcheck
		}
		log.Printf("[BROWSER] Killed Chrome PID %d (group kill failed: %v) for session %q", chromePID, err, session)
	} else {
		log.Printf("[BROWSER] Killed Chrome process group PGID=%d for session %q", chromePID, session)
	}
}

// killSessionRuntime closes the agent-browser session and kills all associated
// processes. Each session owns its own daemon + Chrome (verified 1:1:1), so
// killing this session's daemon does not affect any other session.
//
// Always runs both graceful close AND force kill:
//   - Graceful: daemon closes Chrome and exits cleanly (the happy path).
//   - Force kill: covers (a) daemon already dead but Chrome still alive as an
//     orphan (PPID=1), and (b) graceful close returning success even when the
//     daemon is dead, which means we cannot trust its return code.
func killSessionRuntime(session string) {
	// Step 1: ask the daemon to close gracefully — kills Chrome and exits cleanly
	// when the daemon is alive. NOTE: agent-browser close returns success even
	// when the daemon is already dead, so we cannot rely on this alone.
	gracefulCloseSession(session)

	// Steps 2-4 below are the force-kill safety net. They read this session's
	// daemon PID from <session>.pid and kill the daemon's process tree.
	for _, dir := range sessionDirs() {
		pidFile := filepath.Join(dir, session+".pid")
		pidBytes, err := os.ReadFile(pidFile)
		if err != nil {
			continue
		}
		daemonPID, err := strconv.Atoi(strings.TrimSpace(string(pidBytes)))
		if err != nil {
			continue
		}

		// Step 2: kill live children of the daemon first (catches daemon-restarted
		// Chrome with a new PID that differs from the stored .chrome-pid).
		// Safe even though childPIDs is unfiltered: per verified 1:1:1 topology
		// the daemon has at most one Chrome child, belonging to this session.
		for _, chromePID := range childPIDs(daemonPID) {
			killChromePID(chromePID, session)
		}

		// Step 3: kill the stored chrome-pid (original Chrome, may already be dead
		// but kills its PGID which covers any helpers in the same process group).
		chromePIDFile := filepath.Join(dir, session+".chrome-pid")
		if chromePIDBytes, err := os.ReadFile(chromePIDFile); err == nil {
			if chromePID, err := strconv.Atoi(strings.TrimSpace(string(chromePIDBytes))); err == nil && chromePID > 0 {
				killChromePID(chromePID, session)
			}
		}

		// Step 4: kill the daemon itself. Safe because daemons are per-session.
		proc, err := os.FindProcess(daemonPID)
		if err != nil {
			continue
		}
		if err := proc.Signal(syscall.SIGKILL); err != nil {
			log.Printf("[BROWSER] Could not kill runtime PID %d for session %q: %v", daemonPID, session, err)
		} else {
			log.Printf("[BROWSER] Killed stale runtime PID %d for session %q", daemonPID, session)
		}
	}

	// Step 5: kill any orphaned Chrome processes (PPID=1, agent-browser user-data-dir)
	// that slipped through — daemon may have restarted Chrome right before dying.
	killOrphanedChrome(session)
}

// gracefulCloseSession asks the agent-browser daemon to close the session cleanly.
// Returns true if the daemon acknowledged and shut down within the timeout.
// The daemon stops its Chrome restart loop and kills Chrome itself — no orphans.
func gracefulCloseSession(session string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "agent-browser", "--session", session, "close", "--json")
	if err := cmd.Run(); err != nil {
		log.Printf("[BROWSER] Graceful close failed for %q (%v) — falling back to force kill", session, err)
		return false
	}
	log.Printf("[BROWSER] Gracefully closed session %q", session)
	return true
}

// killOrphanedChrome kills Chrome for Testing processes that are reparented to PID 1
// (daemon was killed but Chrome survived). This handles the case where the daemon
// auto-restarted Chrome after the original Chrome crashed — the restarted Chrome gets
// a new PID that was never captured in .chrome-pid, so it slips past the normal kill.
//
// The pgrep pattern is global (no session filter) because agent-browser uses
// random UUIDs in its user-data-dir path, not session names — there is no way
// to scope by session from the cmdline alone. This is safe because the PPID=1
// filter only matches Chromes whose daemon is already dead: a live session's
// Chrome has PPID = daemon, so it won't match. In other words, anything this
// function kills is by definition already disowned and leaking.
//
// The session argument is logging context only — the kill is not session-scoped.
//
// Note: v0.24.1+ of agent-browser puts Chrome in its own process group and uses
// PR_SET_PDEATHSIG on Linux, so orphans are much rarer now. This remains a safety
// net for crash scenarios and for macOS (no PR_SET_PDEATHSIG equivalent).
func killOrphanedChrome(session string) {
	// Use pgrep to find all Chrome for Testing processes whose cmdline contains
	// the agent-browser temp user-data-dir marker AND whose parent is PID 1.
	out, err := runCommand("pgrep", "-f", "agent-browser-chrome-")
	if err != nil || strings.TrimSpace(out) == "" {
		return
	}
	killed := 0
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		pid, err := strconv.Atoi(strings.TrimSpace(line))
		if err != nil || pid <= 0 {
			continue
		}
		// Check if PPID is 1 (orphaned — daemon already dead)
		ppidOut, err := runCommand("ps", "-p", strconv.Itoa(pid), "-o", "ppid=")
		if err != nil {
			continue
		}
		ppid, err := strconv.Atoi(strings.TrimSpace(ppidOut))
		if err != nil || ppid != 1 {
			continue
		}
		// Orphaned agent-browser Chrome — safe to kill
		killChromePID(pid, session+"/orphan")
		killed++
	}
	if killed > 0 {
		log.Printf("[BROWSER] Killed %d orphaned Chrome process(es) for session %q", killed, session)
	}
}

// childPIDs returns the direct child PIDs of the given parent PID by scanning /proc (Linux)
// or using sysctl (macOS). Returns an empty slice if none are found or on error.
func childPIDs(parentPID int) []int {
	// Use ps to find direct children — works on both macOS and Linux.
	out, err := runCommand("ps", "-o", "pid=", "--ppid", strconv.Itoa(parentPID))
	if err != nil || strings.TrimSpace(out) == "" {
		// macOS ps uses different flags
		out, err = runCommand("pgrep", "-P", strconv.Itoa(parentPID))
		if err != nil || strings.TrimSpace(out) == "" {
			return nil
		}
	}
	var pids []int
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		pid, err := strconv.Atoi(line)
		if err == nil && pid > 0 {
			pids = append(pids, pid)
		}
	}
	return pids
}

// runCommand executes a command and returns its stdout output.
func runCommand(name string, args ...string) (string, error) {
	cmd := execCmd(name, args...)
	out, err := cmd.Output()
	return string(out), err
}

// removeSessionFiles removes agent-browser session state files (.pid, .sock, etc.)
// so the next command starts a fresh runtime + Chrome instead of connecting to a
// dead one. Call killSessionRuntime first to stop the daemon before removing its files.
func removeSessionFiles(session string) {
	for _, dir := range sessionDirs() {
		removed := false
		for _, ext := range []string{".pid", ".chrome-pid", ".sock", ".stream", ".engine", ".version"} {
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
