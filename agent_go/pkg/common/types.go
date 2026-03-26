package common

import (
	"log"
	"strings"
	"sync"
)

// FolderGuardConfig represents folder access restrictions
type FolderGuardConfig struct {
	Enabled      bool     `json:"enabled"`
	ReadPaths    []string `json:"read_paths"`
	WritePaths   []string `json:"write_paths"`
	BlockedPaths []string `json:"blocked_paths"`
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
	// FolderGuardAllowedWriteFolderKey is the context key for allowed write folders ([]string) in chat/plan mode
	FolderGuardAllowedWriteFolderKey ContextKey = "folder_guard_allowed_write_folder"
	// UserIDKey is the context key for the user ID (used for per-user workspace isolation)
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
	// ChatSessionIDKey is the context key for the chat/workflow session ID.
	// Used by browser session tracker to limit concurrent browsers per session.
	ChatSessionIDKey ContextKey = "chat_session_id"
)

// PerUserFolders are folders isolated per-user in the workspace.
// Must stay in sync with workspace/utils/path.go PerUserFolders.
var PerUserFolders = []string{"Chats", "Downloads"}

// SessionShellConfig holds per-session shell execution settings.
// Shared by execute_shell_command and agent_browser to ensure identical sandboxing.
type SessionShellConfig struct {
	WorkingDir         string   // Default working directory (relative to workspace-docs)
	ReadPaths          []string // Folder guard read paths for Isolator
	WritePaths         []string // Folder guard write paths for Isolator
	GeminiProjectDirID string   // Active Gemini CLI project dir for this session
}

var (
	sessionShellConfigs   = make(map[string]*SessionShellConfig)
	sessionShellConfigsMu sync.RWMutex
)

// SetSessionWorkingDir sets the default working directory for a session.
func SetSessionWorkingDir(sessionID, dir string) {
	sessionShellConfigsMu.Lock()
	defer sessionShellConfigsMu.Unlock()
	cfg := sessionShellConfigs[sessionID]
	if cfg == nil {
		cfg = &SessionShellConfig{}
		sessionShellConfigs[sessionID] = cfg
	}
	cfg.WorkingDir = dir
	log.Printf("[SHELL] Set default working dir for session %s: %s", sessionID, dir)
}

// SetSessionFolderGuard sets the folder guard read/write paths for a session.
func SetSessionFolderGuard(sessionID string, readPaths, writePaths []string) {
	sessionShellConfigsMu.Lock()
	defer sessionShellConfigsMu.Unlock()
	cfg := sessionShellConfigs[sessionID]
	if cfg == nil {
		cfg = &SessionShellConfig{}
		sessionShellConfigs[sessionID] = cfg
	}
	cfg.ReadPaths = readPaths
	cfg.WritePaths = writePaths
	log.Printf("[SHELL] Set folder guard for session %s: read=%v write=%v", sessionID, readPaths, writePaths)
}

// SetSessionGeminiProjectDirID stores the active Gemini CLI project dir ID for a session.
// This lets workspace shell execution resolve Gemini-managed files like .gemini/policies/*
// correctly across resumed CLI turns.
func SetSessionGeminiProjectDirID(sessionID, dirID string) {
	sessionShellConfigsMu.Lock()
	defer sessionShellConfigsMu.Unlock()
	cfg := sessionShellConfigs[sessionID]
	if cfg == nil {
		cfg = &SessionShellConfig{}
		sessionShellConfigs[sessionID] = cfg
	}
	cfg.GeminiProjectDirID = dirID
	log.Printf("[SHELL] Set Gemini project dir ID for session %s: %s", sessionID, dirID)
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
	return sessionShellConfigs[sessionID]
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

// QuoteShellArg quotes a shell argument if it contains special characters
func QuoteShellArg(arg string) string {
	if strings.ContainsAny(arg, " \t\n()[]{}|&;'\"\\$<>*?!") {
		escaped := strings.ReplaceAll(arg, "'", "'\"'\"'")
		return "'" + escaped + "'"
	}
	return arg
}
