package main

import (
	"os"
	"path/filepath"
	"strings"
)

func workspaceRoot() string { return filepath.Join(familyDataDir(), "workspace") }

// resolveWorkspacePath cleans a workspace-relative path and guarantees it stays
// inside the workspace (no traversal).
func resolveWorkspacePath(rel string) (string, bool) {
	rel = strings.TrimPrefix(strings.TrimSpace(rel), "/")
	clean := filepath.Clean(rel)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) {
		return "", false
	}
	root := workspaceRoot()
	abs := filepath.Join(root, clean)
	if abs != root && !strings.HasPrefix(abs, root+string(os.PathSeparator)) {
		return "", false
	}
	return abs, true
}
