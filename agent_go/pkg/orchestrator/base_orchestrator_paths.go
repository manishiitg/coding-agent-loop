package orchestrator

import (
	"fmt"
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

	// Special exception: Allow Downloads directory to bypass workspace boundary check
	// Downloads is a common directory that should be accessible regardless of workspace restrictions
	// Check if the path contains "Downloads" as a directory component (not just as part of a filename)
	inputAbsSlash := filepath.ToSlash(inputAbs)
	inputPathSlash := filepath.ToSlash(inputPath)

	// Check if path is in Downloads directory (allow "Downloads", "Downloads/...", or paths containing "/Downloads/")
	// But still prevent directory traversal attacks like "../../Downloads"
	isDownloadsPath := false
	if strings.HasPrefix(inputPathSlash, "Downloads/") || inputPathSlash == "Downloads" {
		// Direct Downloads path - allow it (no directory traversal)
		isDownloadsPath = true
	} else if strings.Contains(inputAbsSlash, "/Downloads/") || strings.HasSuffix(inputAbsSlash, "/Downloads") {
		// Path contains Downloads directory - check it's not a directory traversal attack
		if !strings.Contains(inputPathSlash, "../") && !strings.Contains(inputPathSlash, "..\\") {
			isDownloadsPath = true
		}
	}

	// If it's a Downloads path, skip workspace boundary validation
	if isDownloadsPath {
		// Final safety check: prevent any directory traversal attempts
		if strings.Contains(inputPathSlash, "../") || strings.Contains(inputPathSlash, "..\\") {
			return fmt.Errorf("path '%s' contains directory traversal and cannot be used even for Downloads directory", inputPath)
		}
		// Allow Downloads paths
		return nil
	}

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

// validatePathInAllowedPaths validates that the input path is within any of the allowed paths
// If allowedPaths is empty/nil, returns nil (allows all paths)
func validatePathInAllowedPaths(allowedPaths []string, inputPath string) error {
	// Empty array means disable folder guard - allow all paths
	if len(allowedPaths) == 0 {
		return nil
	}

	// Check against each allowed path
	for _, allowedPath := range allowedPaths {
		if err := validatePathInWorkspace(allowedPath, inputPath); err == nil {
			// Path is valid within this allowed path
			return nil
		}
	}

	// Path is not valid within any allowed path
	return fmt.Errorf("path '%s' is not within any of the allowed paths: %v", inputPath, allowedPaths)
}

// normalizePathForAllowedPaths normalizes a path relative to the first matching allowed path
// Returns the normalized path and the matching allowed path index
func normalizePathForAllowedPaths(allowedPaths []string, inputPath string) (string, int, error) {
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
