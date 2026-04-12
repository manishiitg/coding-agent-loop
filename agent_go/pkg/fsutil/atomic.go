// Package fsutil contains small filesystem helpers shared across packages.
package fsutil

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// WriteJSONAtomic marshals v as indented JSON and writes it to path via a
// temp-file-then-rename sequence so readers never observe a partial file.
// The parent directory is created if missing.
func WriteJSONAtomic(path string, v any, perm fs.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("fsutil: marshal %s: %w", path, err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, perm); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// WorkspaceDocsRoot resolves the absolute path to the workspace-docs root
// directory. WORKSPACE_DOCS_PATH overrides the default `../workspace-docs`.
// Used by every package that needs to read or write files under
// `_users/<id>/` or `_system/`.
func WorkspaceDocsRoot() string {
	if p := os.Getenv("WORKSPACE_DOCS_PATH"); p != "" {
		return p
	}
	abs, _ := filepath.Abs("../workspace-docs")
	return abs
}

// WorkspaceShellRoot returns the workspace-docs root as seen by shell commands.
// When the workspace server runs in Docker, shell commands execute inside the
// container where docs are at /app/workspace-docs, regardless of the host path.
// WORKSPACE_SHELL_ROOT overrides this (for non-standard setups).
func WorkspaceShellRoot() string {
	if p := os.Getenv("WORKSPACE_SHELL_ROOT"); p != "" {
		return p
	}
	// If WORKSPACE_DOCS_PATH is set (Docker agent_go), use it directly —
	// both agent_go and the workspace container see the same path.
	if p := os.Getenv("WORKSPACE_DOCS_PATH"); p != "" {
		return p
	}
	// Local dev: workspace-api Docker container always mounts at /app/workspace-docs
	return "/app/workspace-docs"
}
