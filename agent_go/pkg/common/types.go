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
	// FolderGuardAllowedWriteFolderKey is the context key for the only folder allowed for writes (chat mode)
	FolderGuardAllowedWriteFolderKey ContextKey = "folder_guard_allowed_write_folder"
)

// QuoteShellArg quotes a shell argument if it contains special characters
func QuoteShellArg(arg string) string {
	if strings.ContainsAny(arg, " \t\n()[]{}|&;'\"\\$<>*?!") {
		escaped := strings.ReplaceAll(arg, "'", "'\"'\"'")
		return "'" + escaped + "'"
	}
	return arg
}
