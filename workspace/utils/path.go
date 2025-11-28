package utils

import (
	"path/filepath"
	"strings"
)

// IsValidFilePath validates that the file path is safe and within the docs directory
func IsValidFilePath(filePath, docsDir string) bool {
	// Clean the path to resolve any .. or . components
	cleanPath := filepath.Clean(filePath)
	cleanDocsDir := filepath.Clean(docsDir)

	// Check if the file path is within the docs directory
	relPath, err := filepath.Rel(cleanDocsDir, cleanPath)
	if err != nil {
		return false
	}

	// Check for directory traversal attempts
	if strings.HasPrefix(relPath, "..") {
		return false
	}

	// Check for invalid characters
	if strings.Contains(relPath, "..") {
		return false
	}

	return true
}

// GetRelativePath converts a full internal path to a relative path for API responses
// This ensures that internal directory structure (like /app/workspace-docs) is never exposed
func GetRelativePath(fullPath, docsDir string) (string, error) {
	return filepath.Rel(docsDir, fullPath)
}

// SanitizeInputPath sanitizes input filepaths by stripping internal directory prefixes
// This ensures that users can pass either relative paths or full paths, and we always get clean relative paths
func SanitizeInputPath(inputPath, docsDir string) string {
	// Clean the input path
	cleanInput := filepath.Clean(inputPath)
	cleanDocsDir := filepath.Clean(docsDir)

	// If the input path starts with the docs directory, strip it
	if strings.HasPrefix(cleanInput, cleanDocsDir) {
		// Remove the docs directory prefix and any leading path separators
		relativePath := strings.TrimPrefix(cleanInput, cleanDocsDir)
		relativePath = strings.TrimPrefix(relativePath, string(filepath.Separator))
		return relativePath
	}

	// If it's already a relative path, return it as is
	return cleanInput
}
