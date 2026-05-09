package orchestrator

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// validatePathInWorkspace validates that the input path is within the workspace boundary
func validatePathInWorkspace(workspacePath, inputPath string) error {
	if workspacePath == "" {
		return nil // No validation if workspacePath is not set
	}

	// Normalize workspace path
	workspaceAbs, err := filepath.Abs(workspacePath)
	if err != nil {
		return fmt.Errorf("failed to resolve workspace path: %w", err)
	}
	workspaceAbs = filepath.Clean(workspaceAbs)

	// Resolve input path relative to workspace if it's relative
	var inputAbs string
	if filepath.IsAbs(inputPath) {
		inputAbs, err = filepath.Abs(inputPath)
		if err != nil {
			return fmt.Errorf("failed to resolve input path: %w", err)
		}
	} else {
		// Relative path - check both workspace-relative and CWD-relative resolutions
		// First, resolve relative to workspace (standard behavior)
		inputAbsFromWorkspace := filepath.Join(workspaceAbs, inputPath)
		inputAbsFromWorkspace = filepath.Clean(inputAbsFromWorkspace)

		// Also check what it resolves to from current working directory
		// This catches cases like "Workflow/HRMS PR Review/summary.md" which is a sibling, not a child
		inputAbsFromCWD, err := filepath.Abs(inputPath)
		if err == nil {
			inputAbsFromCWD = filepath.Clean(inputAbsFromCWD)
			// Check if CWD-resolved path is outside workspace
			cwdRel, relErr := filepath.Rel(workspaceAbs, inputAbsFromCWD)
			if relErr == nil && (strings.HasPrefix(cwdRel, "..") || cwdRel == "..") {
				// CWD path is outside workspace - this is the real intent, so use CWD resolution and block it
				inputAbs = inputAbsFromCWD
			} else {
				// CWD path is inside workspace - use workspace-relative resolution
				inputAbs = inputAbsFromWorkspace
			}
		} else {
			// Fallback to workspace-relative if CWD resolution fails
			inputAbs = inputAbsFromWorkspace
		}
	}
	inputAbs = filepath.Clean(inputAbs)

	// Convert inputAbs to slash format for consistent comparison
	inputAbsSlash := filepath.ToSlash(inputAbs)

	// Check if input path is within workspace boundary
	// First, verify that inputAbs actually has workspaceAbs as a prefix with proper path separator
	// This ensures we're checking directory boundaries, not just string prefixes
	workspaceAbsSlash := filepath.ToSlash(workspaceAbs) + "/"

	// Check if paths are equal (same directory) or input is a subdirectory
	if inputAbsSlash != filepath.ToSlash(workspaceAbs) && !strings.HasPrefix(inputAbsSlash, workspaceAbsSlash) {
		return fmt.Errorf("path '%s' (resolved to '%s') is outside workspace boundary '%s'. All file operations must be within the configured workspace", inputPath, inputAbs, workspacePath)
	}

	// Additional check using relative path (catches edge cases)
	rel, err := filepath.Rel(workspaceAbs, inputAbs)
	if err != nil {
		return fmt.Errorf("path validation error: %w", err)
	}

	// Check if path escapes workspace (contains ".." or is absolute)
	if strings.HasPrefix(rel, "..") || rel == ".." {
		return fmt.Errorf("path '%s' (resolved to '%s', relative: '%s') is outside workspace boundary '%s'. All file operations must be within the configured workspace", inputPath, inputAbs, rel, workspacePath)
	}

	return nil
}

// normalizePathForAllowedPaths normalizes a path relative to the first matching allowed path
// Returns the normalized path and the matching allowed path index
func normalizePathForAllowedPaths(allowedPaths []string, inputPath string) (string, int, error) {
	if relPath, ok := normalizeAbsoluteWorkspaceDocsPath(inputPath); ok {
		inputPath = relPath
	}

	// Empty array means disable folder guard - return path as-is
	if len(allowedPaths) == 0 {
		return inputPath, -1, nil
	}

	// Find first matching allowed path
	for i, allowedPath := range allowedPaths {
		if err := validatePathInWorkspace(allowedPath, inputPath); err == nil {
			// Path matches this allowed path - normalize relative to it
			normalizedPath, err := normalizePathForWorkspace(allowedPath, inputPath)
			if err != nil {
				return "", -1, err
			}
			return normalizedPath, i, nil
		}
	}

	// No match found - use first allowed path as base for normalization
	normalizedPath, err := normalizePathForWorkspace(allowedPaths[0], inputPath)
	if err != nil {
		return "", -1, err
	}
	return normalizedPath, 0, nil
}

// normalizePathForWorkspace normalizes a path to be workspace-relative
// Returns a workspace-relative path (e.g., "." becomes "", "subfolder" stays "subfolder", absolute paths become relative)
func normalizePathForWorkspace(workspacePath, inputPath string) (string, error) {
	if workspacePath == "" {
		// No workspace - return path as-is
		return inputPath, nil
	}

	// Handle empty string or "." - normalize to "" which represents workspace root
	if inputPath == "" || inputPath == "." {
		return "", nil
	}

	// Normalize workspace path
	workspaceAbs, err := filepath.Abs(workspacePath)
	if err != nil {
		return "", fmt.Errorf("failed to resolve workspace path: %w", err)
	}
	workspaceAbs = filepath.Clean(workspaceAbs)

	// Resolve input path relative to workspace if it's relative
	var inputAbs string
	if filepath.IsAbs(inputPath) {
		inputAbs, err = filepath.Abs(inputPath)
		if err != nil {
			return "", fmt.Errorf("failed to resolve input path: %w", err)
		}
	} else {
		// Relative path - resolve relative to workspace
		inputAbs = filepath.Join(workspaceAbs, inputPath)
	}
	inputAbs = filepath.Clean(inputAbs)

	// Convert to workspace-relative path
	rel, err := filepath.Rel(workspaceAbs, inputAbs)
	if err != nil {
		return "", fmt.Errorf("path normalization error: %w", err)
	}

	// Handle edge cases
	if rel == "." || rel == "" {
		return "", nil // Workspace root
	}

	return rel, nil
}

func normalizeAbsoluteWorkspaceDocsPath(inputPath string) (string, bool) {
	if !filepath.IsAbs(inputPath) {
		return "", false
	}
	cleanPath := filepath.Clean(inputPath)
	for _, root := range workspaceDocsRootsForPathNormalization() {
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

func workspaceDocsRootsForPathNormalization() []string {
	var roots []string
	if envRoot := os.Getenv("WORKSPACE_DOCS_PATH"); envRoot != "" {
		roots = append(roots, envRoot)
	}
	if cwd, err := os.Getwd(); err == nil {
		for dir := filepath.Clean(cwd); ; dir = filepath.Dir(dir) {
			roots = append(roots, filepath.Join(dir, "workspace-docs"))
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
		}
	}
	roots = append(roots, "/app/workspace-docs", "/workspace-docs")
	return deduplicateNormalizedPaths(roots)
}

func deduplicateNormalizedPaths(paths []string) []string {
	seen := make(map[string]struct{}, len(paths))
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		p = filepath.Clean(strings.TrimSpace(p))
		if p == "." || p == "" {
			continue
		}
		if _, exists := seen[p]; exists {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	return out
}
