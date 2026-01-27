package common

import "strings"

// FolderGuardConfig represents folder access restrictions
type FolderGuardConfig struct {
	Enabled      bool     `json:"enabled"`
	ReadPaths    []string `json:"read_paths"`
	WritePaths   []string `json:"write_paths"`
	BlockedPaths []string `json:"blocked_paths"`
}

// QuoteShellArg quotes a shell argument if it contains special characters
func QuoteShellArg(arg string) string {
	if strings.ContainsAny(arg, " \t\n()[]{}|&;'\"\\$<>*?!") {
		escaped := strings.ReplaceAll(arg, "'", "'\"'\"'")
		return "'" + escaped + "'"
	}
	return arg
}
