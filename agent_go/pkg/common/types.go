package common

import "strings"

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
)

// PerUserFolders are folders isolated per-user in the workspace.
// Must stay in sync with workspace/utils/path.go PerUserFolders.
var PerUserFolders = []string{"Chats", "Downloads", "Plans", "Projects"}

// QuoteShellArg quotes a shell argument if it contains special characters
func QuoteShellArg(arg string) string {
	if strings.ContainsAny(arg, " \t\n()[]{}|&;'\"\\$<>*?!") {
		escaped := strings.ReplaceAll(arg, "'", "'\"'\"'")
		return "'" + escaped + "'"
	}
	return arg
}
