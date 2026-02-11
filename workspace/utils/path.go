package utils

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
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

// --- Per-User Folder Isolation ---
// These utilities support hybrid user/shared folder routing:
// - Per-user folders (Chats/, Downloads/, Plans/) are stored under /_users/{userID}/
// - Shared folders (skills/, Workflow/) remain at root level

// PerUserFolders defines folders that are isolated per-user
var PerUserFolders = []string{"Chats", "Downloads", "Plans"}

// SharedFolders defines folders that are shared across all users
var SharedFolders = []string{"skills", "Workflow"}

// UsersDirectory is the directory under which per-user folders are stored
const UsersDirectory = "_users"

// DefaultUserID is used for single-user mode or when no user ID is provided
const DefaultUserID = "default"

// validUserIDRegex matches allowed user ID characters (alphanumeric, hyphens, underscores)
var validUserIDRegex = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// IsValidUserID checks if a user ID contains only allowed characters
func IsValidUserID(userID string) bool {
	if userID == "" {
		return false
	}
	// Max length check
	if len(userID) > 128 {
		return false
	}
	return validUserIDRegex.MatchString(userID)
}

// SanitizeUserID returns a sanitized user ID or the default user ID if invalid
func SanitizeUserID(userID string) string {
	if userID == "" || !IsValidUserID(userID) {
		return DefaultUserID
	}
	return userID
}

// IsPerUserPath checks if a path belongs to a per-user folder
func IsPerUserPath(requestedPath string) bool {
	// Clean and normalize the path
	cleanPath := strings.TrimPrefix(filepath.Clean(requestedPath), string(filepath.Separator))

	for _, folder := range PerUserFolders {
		// Check if path starts with per-user folder name
		if cleanPath == folder || strings.HasPrefix(cleanPath, folder+string(filepath.Separator)) || strings.HasPrefix(cleanPath, folder+"/") {
			return true
		}
	}
	return false
}

// ResolveUserPath resolves a requested path to the actual filesystem path
// considering per-user folder isolation.
//
// For per-user folders (Chats/, Downloads/):
//   - Input: "Chats/session.json", userID: "user123"
//   - Output: "/_users/user123/Chats/session.json"
//
// For shared folders (skills/, Workflow/) and other paths:
//   - Input: "skills/my-skill.json", userID: "user123"
//   - Output: "/skills/my-skill.json" (unchanged)
//
// Returns the resolved path and any error (e.g., invalid user ID)
func ResolveUserPath(docsDir, requestedPath, userID string) (string, error) {
	// Sanitize user ID
	safeUserID := SanitizeUserID(userID)

	// Clean the requested path
	cleanPath := SanitizeInputPath(requestedPath, docsDir)

	// Block direct access to _users/ directory — prevents cross-user data access.
	// Per-user folders (Chats/, Downloads/, Plans/) are the correct access paths;
	// they get routed to _users/{userID}/ by this function.
	if cleanPath == UsersDirectory || strings.HasPrefix(cleanPath, UsersDirectory+"/") || strings.HasPrefix(cleanPath, UsersDirectory+string(filepath.Separator)) {
		return "", fmt.Errorf("direct access to %s/ directory is not allowed; use Chats/, Downloads/, or Plans/ instead", UsersDirectory)
	}

	// Check if this is a per-user path
	if IsPerUserPath(cleanPath) {
		// Route to /_users/{userID}/{path}
		userDir := filepath.Join(docsDir, UsersDirectory, safeUserID)

		// Create user directory if it doesn't exist
		if err := os.MkdirAll(userDir, 0755); err != nil {
			return "", fmt.Errorf("failed to create user directory: %w", err)
		}

		// Also ensure the per-user folders (Chats/, Downloads/) exist
		// This ensures the folder shows up even if empty
		for _, folder := range PerUserFolders {
			folderPath := filepath.Join(userDir, folder)
			if err := os.MkdirAll(folderPath, 0755); err != nil {
				return "", fmt.Errorf("failed to create user folder %s: %w", folder, err)
			}
		}

		return filepath.Join(userDir, cleanPath), nil
	}

	// Shared folder - use root level
	return filepath.Join(docsDir, cleanPath), nil
}

// ConvertToUserRelativePath converts an absolute path back to a user-relative path
// for API responses. This strips the /_users/{userID}/ prefix if present.
//
// For per-user paths:
//   - Input: "/app/workspace-docs/_users/user123/Chats/session.json"
//   - Output: "Chats/session.json"
//
// For shared paths:
//   - Input: "/app/workspace-docs/skills/my-skill.json"
//   - Output: "skills/my-skill.json"
func ConvertToUserRelativePath(fullPath, docsDir string) (string, error) {
	// Get relative path from docsDir
	relPath, err := filepath.Rel(docsDir, fullPath)
	if err != nil {
		return "", err
	}

	// Check if path is under _users directory
	if strings.HasPrefix(relPath, UsersDirectory+string(filepath.Separator)) || strings.HasPrefix(relPath, UsersDirectory+"/") {
		// Strip _users/{userID}/ prefix
		parts := strings.SplitN(relPath, string(filepath.Separator), 3)
		if len(parts) >= 3 {
			// Return the path after _users/{userID}/
			return parts[2], nil
		}
		// Path is just _users/{userID} with no sub-path
		return "", nil
	}

	return relPath, nil
}

// EnsureUserDirectories creates the per-user folder structure for a user
func EnsureUserDirectories(docsDir, userID string) error {
	safeUserID := SanitizeUserID(userID)
	userDir := filepath.Join(docsDir, UsersDirectory, safeUserID)

	for _, folder := range PerUserFolders {
		folderPath := filepath.Join(userDir, folder)
		if err := os.MkdirAll(folderPath, 0755); err != nil {
			return fmt.Errorf("failed to create user folder %s: %w", folder, err)
		}
	}

	return nil
}

// EnsurePerUserSymlinks creates root-level symlinks for per-user folders pointing
// to the given user's directories. This allows shell commands to access per-user
// folders (Chats/, Plans/, Downloads/) via their logical paths, since the physical
// files live under _users/{userID}/.
func EnsurePerUserSymlinks(docsDir, userID string) error {
	safeUserID := SanitizeUserID(userID)

	for _, folder := range PerUserFolders {
		rootPath := filepath.Join(docsDir, folder)
		targetPath := filepath.Join(UsersDirectory, safeUserID, folder)

		// Check what currently exists at root path
		info, err := os.Lstat(rootPath)
		if err == nil {
			// Something exists — check if it's already a correct symlink
			if info.Mode()&os.ModeSymlink != 0 {
				existingTarget, readErr := os.Readlink(rootPath)
				if readErr == nil && existingTarget == targetPath {
					continue // Already correct
				}
				// Wrong symlink target — remove and recreate
				os.Remove(rootPath)
			} else if info.IsDir() {
				// Real directory exists (not a symlink)
				// If empty (e.g. created by Dockerfile), safe to replace with symlink
				entries, readErr := os.ReadDir(rootPath)
				if readErr != nil || len(entries) > 0 {
					// Has content — skip to avoid data loss (needs migration first)
					fmt.Printf("Warning: real directory %s/ exists with content, skipping symlink (run migration first)\n", folder)
					continue
				}
				// Empty directory — remove it so we can create the symlink
				if removeErr := os.Remove(rootPath); removeErr != nil {
					fmt.Printf("Warning: failed to remove empty directory %s/: %v\n", folder, removeErr)
					continue
				}
			}
		}

		// Create symlink: rootPath -> _users/{userID}/{folder}
		if err := os.Symlink(targetPath, rootPath); err != nil {
			return fmt.Errorf("failed to create symlink %s -> %s: %w", folder, targetPath, err)
		}
	}

	return nil
}

// MigratePerUserFolders migrates existing per-user folders from root level to /_users/default/
// This is a one-time migration for backwards compatibility with existing workspaces.
// Also handles partial migrations where _users/default/ exists but root-level folders still have content.
// Returns the number of folders migrated and any error.
func MigratePerUserFolders(docsDir string) (int, error) {
	migratedCount := 0

	// Check each per-user folder at root level
	for _, folder := range PerUserFolders {
		rootFolderPath := filepath.Join(docsDir, folder)

		// Check if root path is a symlink — already migrated
		if linfo, err := os.Lstat(rootFolderPath); err == nil && linfo.Mode()&os.ModeSymlink != 0 {
			continue
		}

		// Check if folder exists at root level and has content
		info, err := os.Stat(rootFolderPath)
		if err != nil {
			if os.IsNotExist(err) {
				// Folder doesn't exist at root, nothing to migrate
				continue
			}
			return migratedCount, fmt.Errorf("failed to check folder %s: %w", folder, err)
		}

		if !info.IsDir() {
			// Not a directory, skip
			continue
		}

		// Check if folder has content
		entries, err := os.ReadDir(rootFolderPath)
		if err != nil {
			return migratedCount, fmt.Errorf("failed to read folder %s: %w", folder, err)
		}

		if len(entries) == 0 {
			// Empty folder, nothing to migrate
			continue
		}

		// Create destination path under /_users/default/
		destFolderPath := filepath.Join(docsDir, UsersDirectory, DefaultUserID, folder)

		// Create parent directory
		if err := os.MkdirAll(filepath.Dir(destFolderPath), 0755); err != nil {
			return migratedCount, fmt.Errorf("failed to create user directory: %w", err)
		}

		// Check if destination already exists (partial migration scenario)
		if _, destErr := os.Stat(destFolderPath); destErr == nil {
			// Destination exists — merge content (copy without overwriting existing files)
			if copyErr := copyDirMerge(rootFolderPath, destFolderPath); copyErr != nil {
				return migratedCount, fmt.Errorf("failed to merge folder %s: %w", folder, copyErr)
			}
			// Remove original after successful merge
			if removeErr := os.RemoveAll(rootFolderPath); removeErr != nil {
				fmt.Printf("Warning: failed to remove original folder %s after merge: %v\n", folder, removeErr)
			}
		} else {
			// Destination doesn't exist — move the folder
			// First, try a simple rename (works if on same filesystem)
			if err := os.Rename(rootFolderPath, destFolderPath); err != nil {
				// Rename failed (possibly cross-filesystem), try copy and delete
				if copyErr := copyDir(rootFolderPath, destFolderPath); copyErr != nil {
					return migratedCount, fmt.Errorf("failed to migrate folder %s: %w", folder, copyErr)
				}
				// Remove original after successful copy
				if removeErr := os.RemoveAll(rootFolderPath); removeErr != nil {
					fmt.Printf("Warning: failed to remove original folder %s after migration: %v\n", folder, removeErr)
				}
			}
		}

		fmt.Printf("Migrated per-user folder '%s' to /_users/%s/%s\n", folder, DefaultUserID, folder)
		migratedCount++
	}

	// Ensure the default user directories exist (creates empty folders for non-migrated ones)
	if err := EnsureUserDirectories(docsDir, DefaultUserID); err != nil {
		return migratedCount, fmt.Errorf("failed to ensure user directories: %w", err)
	}

	return migratedCount, nil
}

// copyDirMerge copies files from src into dst without overwriting existing files.
// Used for partial migration scenarios where some files already exist in dst.
func copyDirMerge(src, dst string) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(dst, 0755); err != nil {
		return err
	}

	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())

		if entry.IsDir() {
			if err := copyDirMerge(srcPath, dstPath); err != nil {
				return err
			}
		} else {
			// Only copy if destination file doesn't exist
			if _, err := os.Stat(dstPath); os.IsNotExist(err) {
				if err := copyFile(srcPath, dstPath); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

// copyDir recursively copies a directory from src to dst
func copyDir(src, dst string) error {
	// Get source info
	srcInfo, err := os.Stat(src)
	if err != nil {
		return err
	}

	// Create destination directory
	if err := os.MkdirAll(dst, srcInfo.Mode()); err != nil {
		return err
	}

	// Read source directory contents
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())

		if entry.IsDir() {
			// Recursively copy subdirectory
			if err := copyDir(srcPath, dstPath); err != nil {
				return err
			}
		} else {
			// Copy file
			if err := copyFile(srcPath, dstPath); err != nil {
				return err
			}
		}
	}

	return nil
}

// copyFile copies a single file from src to dst
func copyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	srcInfo, err := srcFile.Stat()
	if err != nil {
		return err
	}

	dstFile, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, srcInfo.Mode())
	if err != nil {
		return err
	}
	defer dstFile.Close()

	if _, err := dstFile.ReadFrom(srcFile); err != nil {
		return err
	}

	return nil
}
