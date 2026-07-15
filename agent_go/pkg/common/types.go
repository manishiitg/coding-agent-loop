package common

import (
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// FolderGuardConfig represents folder access restrictions
type FolderGuardConfig struct {
	Enabled           bool     `json:"enabled"`
	ReadPaths         []string `json:"read_paths"`
	WritePaths        []string `json:"write_paths"`
	BlockedPaths      []string `json:"blocked_paths"`
	BlockedWritePaths []string `json:"blocked_write_paths,omitempty"`
}

// ContextKey is a custom type for context keys to avoid collisions
type ContextKey string

const (
	// FolderGuardReadPathsKey is the context key for folder guard read paths
	FolderGuardReadPathsKey ContextKey = "folder_guard_read_paths"
	// FolderGuardWritePathsKey is the context key for folder guard write paths
	FolderGuardWritePathsKey ContextKey = "folder_guard_write_paths"
	// FolderGuardBlockedPathsKey is the context key for blocked paths (deny list)
	FolderGuardBlockedPathsKey ContextKey = "folder_guard_blocked_paths"
	// FolderGuardBlockedWritePathsKey is the context key for write-only deny paths.
	FolderGuardBlockedWritePathsKey ContextKey = "folder_guard_blocked_write_paths"
	// FolderGuardAllowedWriteFolderKey is the context key for allowed write folders ([]string) in chat/plan mode
	FolderGuardAllowedWriteFolderKey ContextKey = "folder_guard_allowed_write_folder"
	// UserIDKey is the context key for the user ID (used for auth/database scoping)
	UserIDKey ContextKey = "user_id"
	// BrowserDownloadsPathKey is the context key for the browser downloads folder path (relative to workspace root)
	// Used by agent-browser executor to set the working directory for screenshot/download commands
	BrowserDownloadsPathKey ContextKey = "browser_downloads_path"
	// DefaultWorkingDirKey is the context key for the session-level default working directory.
	// Used by execute_shell_command as a safety-net: if the LLM passes "." it is replaced
	// with this value so commands run in the correct folder.
	// Set per mode:
	//   - plan mode  → "Chats"  (wrapExecutorsWithPlanFolderGuard)
	//   - chat mode  → "Chats"  (wrapExecutorsWithChatModeFolderGuard)
	DefaultWorkingDirKey ContextKey = "default_working_dir"
	// ChatSessionIDKey is the context key for the agent-level session ID.
	// For share_browser=false sub-agents this is the isolated session ID;
	// for share_browser=true (default), this is the parent workflow session ID.
	// Used by browser session tracker for per-agent limits.
	ChatSessionIDKey ContextKey = "chat_session_id"
	// WorkflowSessionIDKey is the context key for the root workflow/chat session ID.
	// Always the parent session — never changes for isolated sub-agents.
	// Used by browser session tracker for per-workflow limits.
	WorkflowSessionIDKey ContextKey = "workflow_session_id"
)

// WorkspaceFolders are the standard workspace folders.
var WorkspaceFolders = []string{"Chats", "Downloads"}

// HostDownloadsPath returns the host Downloads directory used by native Chrome
// in CDP mode. PI_HOST_DOWNLOADS_PATH/HOST_DOWNLOADS_PATH can override this for
// packaged or Docker deployments where the backend HOME is not the user's HOME.
func HostDownloadsPath() string {
	for _, key := range []string{"PI_HOST_DOWNLOADS_PATH", "HOST_DOWNLOADS_PATH"} {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return filepath.Clean(expandHomePath(value))
		}
	}
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return ""
	}
	return filepath.Join(home, "Downloads")
}

func expandHomePath(path string) string {
	if path == "~" || strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
			if path == "~" {
				return home
			}
			return filepath.Join(home, strings.TrimPrefix(path, "~/"))
		}
	}
	return path
}

// CDPHostDownloadsReadPath returns the host Downloads read path only for CDP
// mode. Headless browser downloads should stay inside workspace folders.
func CDPHostDownloadsReadPath(browserMode string) string {
	if strings.EqualFold(strings.TrimSpace(browserMode), "cdp") {
		return HostDownloadsPath()
	}
	return ""
}

// GrantSessionCDPHostDownloadsReadOnly adds a read-only host Downloads grant to
// a CDP session. The path is also added to BlockedWritePaths because macOS
// sandboxing allows most of the home directory by default and relies on this
// explicit deny to keep the grant read-only.
func GrantSessionCDPHostDownloadsReadOnly(sessionID, browserMode string) string {
	hostDownloads := CDPHostDownloadsReadPath(browserMode)
	if strings.TrimSpace(sessionID) == "" || hostDownloads == "" {
		return ""
	}
	updateSessionShellConfig(sessionID, func(cfg *SessionShellConfig) {
		cfg.ReadPaths = DeduplicateStrings(append(cfg.ReadPaths, hostDownloads))
		cfg.BlockedWritePaths = DeduplicateStrings(append(cfg.BlockedWritePaths, hostDownloads))
	})
	log.Printf("[SHELL] Granted read-only host Downloads for CDP session %s: %s", sessionID, hostDownloads)
	return hostDownloads
}

// PopulateMCPBridgeShortEnv derives short shell-friendly MCP bridge variables
// from MCP_API_URL and MCP_API_TOKEN. It mutates env in-place.
func PopulateMCPBridgeShortEnv(env map[string]string) {
	if env == nil {
		return
	}
	baseURL := strings.TrimRight(strings.TrimSpace(env["MCP_API_URL"]), "/")
	if baseURL == "" {
		delete(env, "MCP_MCP")
		delete(env, "MCP_CUSTOM")
		delete(env, "MCP_VIRTUAL")
	} else {
		env["MCP_MCP"] = baseURL + "/tools/mcp"
		env["MCP_CUSTOM"] = baseURL + "/tools/custom"
		env["MCP_VIRTUAL"] = baseURL + "/tools/virtual"
	}

	token := strings.TrimSpace(env["MCP_API_TOKEN"])
	if token == "" {
		delete(env, "MCP_AUTH")
		return
	}
	env["MCP_AUTH"] = "Authorization: Bearer " + token
}

// SessionShellConfig holds per-session shell execution settings.
// Shared by execute_shell_command and agent_browser to ensure identical sandboxing.
//
// BlockedWritePaths denies writes only (reads pass through). See FolderGuardConfig
// docs in pkg/workspace for the semantic distinction vs the isolator's BlockedPaths
// (which is "deny all"). Typical use: chat-agent with #workflow grants Workflow/<name>/
// as a write prefix but adds Workflow/<name>/planning/ to BlockedWritePaths so the
// agent can read plan.json but not raw-write it.
type SessionShellConfig struct {
	WorkingDir        string   // Default working directory (relative to workspace-docs)
	FolderGuardSet    bool     // An explicit guard exists; empty capabilities must fail closed
	ReadPaths         []string // Folder guard read paths for Isolator
	WritePaths        []string // Folder guard write paths for Isolator
	BlockedPaths      []string // Deny reads and writes
	BlockedWritePaths []string // Deny writes; reads allowed (flows to FolderGuardConfig.BlockedWritePaths)
	BrowserMode       string   // Resolved browser mode: "headless", "cdp", ""
	BrowserSessionID  string   // Shared browser identity for browser tools when "default" session is used
	// Env is extra environment variables exported into this session's shell
	// (bridge execute_shell_command). Lets per-step values like DB_PATH and
	// STEP_OUTPUT_DIR reach the server-side bridge shell, which — unlike the
	// in-process built-in executor — has no other channel for them.
	Env map[string]string
}

var (
	sessionShellConfigs   = make(map[string]*SessionShellConfig)
	sessionShellConfigsMu sync.RWMutex
)

func cloneStrings(values []string) []string {
	if values == nil {
		return nil
	}
	return append([]string(nil), values...)
}

func cloneStringMap(values map[string]string) map[string]string {
	if values == nil {
		return nil
	}
	out := make(map[string]string, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}

func cloneSessionShellConfig(cfg *SessionShellConfig) *SessionShellConfig {
	if cfg == nil {
		return &SessionShellConfig{}
	}
	copy := *cfg
	copy.ReadPaths = cloneStrings(cfg.ReadPaths)
	copy.WritePaths = cloneStrings(cfg.WritePaths)
	copy.BlockedPaths = cloneStrings(cfg.BlockedPaths)
	copy.BlockedWritePaths = cloneStrings(cfg.BlockedWritePaths)
	copy.Env = cloneStringMap(cfg.Env)
	return &copy
}

func updateSessionShellConfig(sessionID string, update func(*SessionShellConfig)) {
	sessionShellConfigsMu.Lock()
	defer sessionShellConfigsMu.Unlock()
	cfg := cloneSessionShellConfig(sessionShellConfigs[sessionID])
	update(cfg)
	sessionShellConfigs[sessionID] = cfg
}

// SetSessionWorkingDir sets the default working directory for a session.
func SetSessionWorkingDir(sessionID, dir string) {
	updateSessionShellConfig(sessionID, func(cfg *SessionShellConfig) {
		cfg.WorkingDir = dir
	})
	log.Printf("[SHELL] Set default working dir for session %s: %s", sessionID, dir)
}

// SetSessionFolderGuard sets the folder guard read/write paths for a session.
// Does not touch BlockedWritePaths — use SetSessionFolderGuardBlockedWritePaths
// to set those independently, so existing callers don't need updating when a
// session adds a deny prefix later.
func SetSessionFolderGuard(sessionID string, readPaths, writePaths []string) {
	updateSessionShellConfig(sessionID, func(cfg *SessionShellConfig) {
		cfg.FolderGuardSet = true
		cfg.ReadPaths = cloneStrings(readPaths)
		cfg.WritePaths = cloneStrings(writePaths)
	})
	log.Printf("[SHELL] Set folder guard for session %s: read=%v write=%v", sessionID, readPaths, writePaths)
}

// SetSessionFolderGuardBlockedPaths sets hard deny prefixes for both reads and writes.
func SetSessionFolderGuardBlockedPaths(sessionID string, blockedPaths []string) {
	updateSessionShellConfig(sessionID, func(cfg *SessionShellConfig) {
		cfg.FolderGuardSet = true
		cfg.BlockedPaths = cloneStrings(blockedPaths)
	})
	log.Printf("[SHELL] Set folder guard blocked paths for session %s: %v", sessionID, blockedPaths)
}

// SetSessionFolderGuardBlockedWritePaths sets the write-denied prefix list for
// a session. Reads stay allowed. BlockedWritePaths flow through to the isolator's
// FolderGuardConfig.BlockedWritePaths so kernel-level enforcement (sandbox-exec
// `(deny file-write*)` on macOS, read-only bind-mount on Linux) blocks writes
// even when the path is under a WritePath prefix. Used by the chat-agent
// #workflow setup to grant `Workflow/<name>/` as a broad write prefix while
// denying writes to `Workflow/<name>/planning/`.
func SetSessionFolderGuardBlockedWritePaths(sessionID string, blockedWritePaths []string) {
	updateSessionShellConfig(sessionID, func(cfg *SessionShellConfig) {
		cfg.FolderGuardSet = true
		cfg.BlockedWritePaths = cloneStrings(blockedWritePaths)
	})
	log.Printf("[SHELL] Set folder guard blocked-write paths for session %s: %v", sessionID, blockedWritePaths)
}

// SetSessionShellEnv merges extra environment variables into a session's shell
// config (later calls override earlier keys). The map is replaced wholesale
// under lock so concurrent readers always observe a complete map, never a
// partially-written one.
func SetSessionShellEnv(sessionID string, env map[string]string) {
	if len(env) == 0 {
		return
	}
	updateSessionShellConfig(sessionID, func(cfg *SessionShellConfig) {
		if cfg.Env == nil {
			cfg.Env = make(map[string]string, len(env))
		}
		for key, value := range env {
			cfg.Env[key] = value
		}
	})
	log.Printf("[SHELL] Set %d shell env var(s) for session %s", len(env), sessionID)
}

// GetSessionShellEnv returns a copy of the session's shell env vars (nil if none),
// safe to read without holding the config lock.
func GetSessionShellEnv(sessionID string) map[string]string {
	sessionShellConfigsMu.RLock()
	defer sessionShellConfigsMu.RUnlock()
	cfg := sessionShellConfigs[sessionID]
	if cfg == nil || len(cfg.Env) == 0 {
		return nil
	}
	out := make(map[string]string, len(cfg.Env))
	for k, v := range cfg.Env {
		out[k] = v
	}
	return out
}

// SetSessionBrowserMode stores the resolved browser mode for a session.
// Used by execute_shell_command to show context-aware error messages when blocking agent-browser CLI calls.
func SetSessionBrowserMode(sessionID, mode string) {
	updateSessionShellConfig(sessionID, func(cfg *SessionShellConfig) {
		cfg.BrowserMode = mode
	})
	log.Printf("[SHELL] Set browser mode for session %s: %s", sessionID, mode)
}

// GetSessionBrowserMode returns the resolved browser mode for a session, or "" if
// none was set. Used to persist a session's runtime so a restore can replay the
// same browser capability (and re-register agent_browser).
func GetSessionBrowserMode(sessionID string) string {
	sessionShellConfigsMu.RLock()
	defer sessionShellConfigsMu.RUnlock()
	if cfg := sessionShellConfigs[sessionID]; cfg != nil {
		return cfg.BrowserMode
	}
	return ""
}

// SetSessionBrowserSessionID stores the shared browser session identity for a tool/chat session.
// Browser tools can use this to converge on a stable browser state while keeping tool routing
// scoped to the original chat/workflow session.
func SetSessionBrowserSessionID(sessionID, browserSessionID string) {
	updateSessionShellConfig(sessionID, func(cfg *SessionShellConfig) {
		cfg.BrowserSessionID = browserSessionID
	})
	log.Printf("[SHELL] Set browser session ID for session %s: %s", sessionID, browserSessionID)
}

// CopySessionFolderGuard copies the folder guard (ReadPaths + WritePaths +
// BlockedWritePaths) from one session to another. Used to propagate restrictions
// from a parent HTTP session to child group sessions (batch execution, workshop
// groups) so that sub-agents running under the new session ID inherit the same
// write restrictions AND the same blocked-write exceptions. Returns true if a
// guard was copied.
func CopySessionFolderGuard(fromSessionID, toSessionID string) bool {
	src := GetSessionShellConfig(fromSessionID)
	if src == nil || (!src.FolderGuardSet && len(src.ReadPaths) == 0 && len(src.WritePaths) == 0 && len(src.BlockedPaths) == 0 && len(src.BlockedWritePaths) == 0) {
		return false
	}
	SetSessionFolderGuard(toSessionID, src.ReadPaths, src.WritePaths)
	if len(src.BlockedPaths) > 0 {
		SetSessionFolderGuardBlockedPaths(toSessionID, src.BlockedPaths)
	}
	if len(src.BlockedWritePaths) > 0 {
		SetSessionFolderGuardBlockedWritePaths(toSessionID, src.BlockedWritePaths)
	}
	log.Printf("[FOLDER_GUARD] Copied folder guard from session %s to %s: read=%v write=%v blocked-write=%v", fromSessionID, toSessionID, src.ReadPaths, src.WritePaths, src.BlockedWritePaths)
	return true
}

// ClearSessionShellConfig removes all shell config for a session.
func ClearSessionShellConfig(sessionID string) {
	sessionShellConfigsMu.Lock()
	defer sessionShellConfigsMu.Unlock()
	delete(sessionShellConfigs, sessionID)
}

// GetSessionShellConfig looks up the shell config for a session.
func GetSessionShellConfig(sessionID string) *SessionShellConfig {
	sessionShellConfigsMu.RLock()
	defer sessionShellConfigsMu.RUnlock()
	cfg := sessionShellConfigs[sessionID]
	if cfg == nil {
		return nil
	}
	return cloneSessionShellConfig(cfg)
}

// ResolveBrowserSessionID returns the effective browser session to use for browser tools.
// Explicit non-default session names win. The default session can be remapped to a stable
// shared browser identity via per-session shell config.
func ResolveBrowserSessionID(sessionID, requested string) string {
	requested = strings.TrimSpace(requested)
	if requested != "" && requested != "default" {
		return requested
	}
	cfg := GetSessionShellConfig(sessionID)
	if cfg != nil && strings.TrimSpace(cfg.BrowserSessionID) != "" {
		return strings.TrimSpace(cfg.BrowserSessionID)
	}
	return requested
}

// DeduplicateStrings removes duplicate strings from a slice.
func DeduplicateStrings(strs []string) []string {
	seen := make(map[string]bool, len(strs))
	result := make([]string, 0, len(strs))
	for _, s := range strs {
		if !seen[s] {
			seen[s] = true
			result = append(result, s)
		}
	}
	return result
}

// IsNativeWorkspace reports whether the workspace server is running natively
// on the host (not inside Docker). When true, shell commands, CDP connections,
// and MCP API URLs should use localhost/127.0.0.1 instead of host.docker.internal.
//
// Detection: NATIVE_WORKSPACE=true is set by run_server_with_logging.sh --with-workspace.
func IsNativeWorkspace() bool {
	return os.Getenv("NATIVE_WORKSPACE") == "true"
}

// QuoteShellArg quotes a shell argument if it contains special characters
func QuoteShellArg(arg string) string {
	if strings.ContainsAny(arg, " \t\n()[]{}|&;'\"\\$<>*?!") {
		escaped := strings.ReplaceAll(arg, "'", "'\"'\"'")
		return "'" + escaped + "'"
	}
	return arg
}
