package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	virtualtools "mcp-agent-builder-go/agent_go/cmd/server/virtual-tools"
)

// ReadWorkspaceFile reads a file from the workspace using MCP tools
// If filePath is relative, it will be resolved relative to the workspace path
// IMPORTANT: Callers should pass RELATIVE paths (e.g., "planning/plan.json").
// The workspacePath is automatically prepended for relative paths.
func (bo *BaseOrchestrator) ReadWorkspaceFile(ctx context.Context, filePath string) (string, error) {
	// If path is relative and we have a workspace path, prepend it
	workspacePath := bo.GetWorkspacePath()
	if workspacePath != "" && !filepath.IsAbs(filePath) {
		// DEFENSIVE FIX: Check if the relative path already starts with workspacePath
		// This prevents double-prepending bugs (e.g., "ws/ws/file.json")
		if strings.HasPrefix(filePath, workspacePath+"/") || filePath == workspacePath {
			bo.GetLogger().Warn(fmt.Sprintf("⚠️ ReadWorkspaceFile: path '%s' already contains workspace path '%s' - using as-is to prevent double-prepend. Please fix caller to use relative path only.", filePath, workspacePath))
			// Don't prepend - use path as-is
		} else {
			// Join workspace path with relative file path
			filePath = filepath.Join(workspacePath, filePath)
		}
	}

	// Prepare tool call parameters (MCP tools expect map[string]interface{})
	readArgs := map[string]interface{}{
		"filepath": filePath,
	}

	// Get the tool executor
	readExecutorInterface, exists := bo.WorkspaceToolExecutors["read_workspace_file"]
	if !exists {
		bo.GetLogger().Warn(fmt.Sprintf("ReadWorkspaceFile(%s) failed: executor not found", filePath))
		return "", fmt.Errorf("read_workspace_file tool executor not found")
	}

	readExecutor, ok := readExecutorInterface.(func(context.Context, map[string]interface{}) (string, error))
	if !ok {
		bo.GetLogger().Warn(fmt.Sprintf("ReadWorkspaceFile(%s) failed: executor wrong type", filePath))
		return "", fmt.Errorf("read_workspace_file tool executor has wrong type")
	}

	// Inject event emitter into context before calling executor
	ctx = context.WithValue(ctx, virtualtools.WorkspaceEventEmitterKey, bo.contextAwareBridge)

	// Execute the tool call using existing workspace tool logic
	readResult, err := readExecutor(ctx, readArgs)
	if err != nil {
		bo.GetLogger().Warn(fmt.Sprintf("ReadWorkspaceFile(%s) failed: %v", filePath, err))
		return "", fmt.Errorf("failed to read file %s: %w", filePath, err)
	}

	// Parse the response using proper type from virtualtools
	var fileData virtualtools.WorkspaceFileContent
	if err := json.Unmarshal([]byte(readResult), &fileData); err != nil {
		bo.GetLogger().Warn(fmt.Sprintf("ReadWorkspaceFile(%s) failed: parse error %v", filePath, err))
		return "", fmt.Errorf("failed to parse workspace response: %w", err)
	}

	// Extract content directly from the parsed data
	fileContent := fileData.Content

	if fileContent == "" {
		bo.GetLogger().Warn(fmt.Sprintf("ReadWorkspaceFile(%s) failed: no content found", filePath))
		return "", fmt.Errorf("no content found in workspace response")
	}

	return fileContent, nil
}

// CheckWorkspaceFileExists checks if a file exists in the workspace
// Uses ReadWorkspaceFile internally but returns a boolean instead of content
func (bo *BaseOrchestrator) CheckWorkspaceFileExists(ctx context.Context, filePath string) (bool, error) {
	_, err := bo.ReadWorkspaceFile(ctx, filePath)
	if err != nil {
		errStr := err.Error()
		// Check if it's a "file not found" error
		if strings.Contains(errStr, "not found") || strings.Contains(errStr, "no such file") {
			return false, nil
		}
		// For any other error (e.g. "is a directory", HTTP 500, parse errors),
		// fall back to ListWorkspaceFiles which handles both files and folders.
		// This is critical because ReadWorkspaceFile fails on directories.
		_, listErr := bo.ListWorkspaceFiles(ctx, filePath)
		if listErr != nil {
			// ListWorkspaceFiles returns error for non-existent folders
			if strings.Contains(listErr.Error(), "does not exist") {
				return false, nil
			}
			// Both methods failed - return original error
			return false, err
		}
		// ListWorkspaceFiles succeeded (folder exists, even if empty)
		return true, nil
	}

	return true, nil
}

// WriteWorkspaceFile writes content to a file in the workspace using MCP tools
// If filePath is relative, it will be resolved relative to the workspace path
// IMPORTANT: Callers should pass RELATIVE paths (e.g., "planning/plan.json").
// The workspacePath is automatically prepended for relative paths.
func (bo *BaseOrchestrator) WriteWorkspaceFile(ctx context.Context, filePath string, content string) error {
	// If path is relative and we have a workspace path, prepend it
	workspacePath := bo.GetWorkspacePath()
	if workspacePath != "" && !filepath.IsAbs(filePath) {
		// DEFENSIVE FIX: Check if the relative path already starts with workspacePath
		// This prevents double-prepending bugs (e.g., "ws/ws/file.json")
		if strings.HasPrefix(filePath, workspacePath+"/") || filePath == workspacePath {
			bo.GetLogger().Warn(fmt.Sprintf("⚠️ WriteWorkspaceFile: path '%s' already contains workspace path '%s' - using as-is to prevent double-prepend. Please fix caller to use relative path only.", filePath, workspacePath))
			// Don't prepend - use path as-is
		} else {
			// Join workspace path with relative file path
			filePath = filepath.Join(workspacePath, filePath)
		}
	}
	startTime := time.Now()
	contentSize := len(content)

	// Prepare tool call parameters (MCP tools expect map[string]interface{})
	writeArgs := map[string]interface{}{
		"filepath": filePath,
		"content":  content,
	}

	// Get the tool executor
	writeExecutorInterface, exists := bo.WorkspaceToolExecutors["update_workspace_file"]
	if !exists {
		duration := time.Since(startTime)
		bo.GetLogger().Warn(fmt.Sprintf("⏱️ [WORKSPACE] WriteWorkspaceFile(%s, %d bytes) failed: executor not found (took %v)", filePath, contentSize, duration))
		return fmt.Errorf("update_workspace_file tool executor not found")
	}

	writeExecutor, ok := writeExecutorInterface.(func(context.Context, map[string]interface{}) (string, error))
	if !ok {
		duration := time.Since(startTime)
		bo.GetLogger().Warn(fmt.Sprintf("⏱️ [WORKSPACE] WriteWorkspaceFile(%s, %d bytes) failed: executor wrong type (took %v)", filePath, contentSize, duration))
		return fmt.Errorf("update_workspace_file tool executor has wrong type")
	}

	// Inject event emitter into context before calling executor
	ctx = context.WithValue(ctx, virtualtools.WorkspaceEventEmitterKey, bo.contextAwareBridge)

	// Execute the tool call using existing workspace tool logic
	_, err := writeExecutor(ctx, writeArgs)
	if err != nil {
		duration := time.Since(startTime)
		bo.GetLogger().Warn(fmt.Sprintf("⏱️ [WORKSPACE] WriteWorkspaceFile(%s, %d bytes) failed: %v (took %v)", filePath, contentSize, err, duration))
		return fmt.Errorf("failed to write file %s: %w", filePath, err)
	}

	duration := time.Since(startTime)
	bo.GetLogger().Debug(fmt.Sprintf("⏱️ [WORKSPACE] WriteWorkspaceFile(%s, %d bytes) completed successfully (took %v)", filePath, contentSize, duration))
	return nil
}

// DeleteWorkspaceFile deletes a file from the workspace using MCP tools
func (bo *BaseOrchestrator) DeleteWorkspaceFile(ctx context.Context, filePath string) error {
	startTime := time.Now()

	// Prepare tool call parameters (MCP tools expect map[string]interface{})
	deleteArgs := map[string]interface{}{
		"filepath": filePath,
	}

	// Get the tool executor
	deleteExecutorInterface, exists := bo.WorkspaceToolExecutors["delete_workspace_file"]
	if !exists {
		duration := time.Since(startTime)
		bo.GetLogger().Warn(fmt.Sprintf("⏱️ [WORKSPACE] DeleteWorkspaceFile(%s) failed: executor not found (took %v)", filePath, duration))
		return fmt.Errorf("delete_workspace_file tool executor not found")
	}

	deleteExecutor, ok := deleteExecutorInterface.(func(context.Context, map[string]interface{}) (string, error))
	if !ok {
		duration := time.Since(startTime)
		bo.GetLogger().Warn(fmt.Sprintf("⏱️ [WORKSPACE] DeleteWorkspaceFile(%s) failed: executor wrong type (took %v)", filePath, duration))
		return fmt.Errorf("delete_workspace_file tool executor has wrong type")
	}

	// Inject event emitter into context before calling executor
	ctx = context.WithValue(ctx, virtualtools.WorkspaceEventEmitterKey, bo.contextAwareBridge)

	// Execute the tool call using existing workspace tool logic
	_, err := deleteExecutor(ctx, deleteArgs)
	if err != nil {
		duration := time.Since(startTime)
		bo.GetLogger().Warn(fmt.Sprintf("⏱️ [WORKSPACE] DeleteWorkspaceFile(%s) failed: %v (took %v)", filePath, err, duration))
		return fmt.Errorf("failed to delete file %s: %w", filePath, err)
	}

	duration := time.Since(startTime)
	bo.GetLogger().Debug(fmt.Sprintf("⏱️ [WORKSPACE] DeleteWorkspaceFile(%s) completed successfully (took %v)", filePath, duration))
	return nil
}

// MoveWorkspaceFile moves a file or directory from one location to another in the workspace using MCP tools
func (bo *BaseOrchestrator) MoveWorkspaceFile(ctx context.Context, sourcePath string, destinationPath string) error {
	startTime := time.Now()

	// Prepare tool call parameters (MCP tools expect map[string]interface{})
	moveArgs := map[string]interface{}{
		"source_filepath":      sourcePath,
		"destination_filepath": destinationPath,
	}

	// Get the tool executor
	moveExecutorInterface, exists := bo.WorkspaceToolExecutors["move_workspace_file"]
	if !exists {
		duration := time.Since(startTime)
		bo.GetLogger().Warn(fmt.Sprintf("⏱️ [WORKSPACE] MoveWorkspaceFile(%s -> %s) failed: executor not found (took %v)", sourcePath, destinationPath, duration))
		return fmt.Errorf("move_workspace_file tool executor not found")
	}

	moveExecutor, ok := moveExecutorInterface.(func(context.Context, map[string]interface{}) (string, error))
	if !ok {
		duration := time.Since(startTime)
		bo.GetLogger().Warn(fmt.Sprintf("⏱️ [WORKSPACE] MoveWorkspaceFile(%s -> %s) failed: executor wrong type (took %v)", sourcePath, destinationPath, duration))
		return fmt.Errorf("move_workspace_file tool executor has wrong type")
	}

	// Inject event emitter into context before calling executor
	ctx = context.WithValue(ctx, virtualtools.WorkspaceEventEmitterKey, bo.contextAwareBridge)

	// Execute the tool call using existing workspace tool logic
	_, err := moveExecutor(ctx, moveArgs)
	if err != nil {
		duration := time.Since(startTime)
		bo.GetLogger().Warn(fmt.Sprintf("⏱️ [WORKSPACE] MoveWorkspaceFile(%s -> %s) failed: %v (took %v)", sourcePath, destinationPath, err, duration))
		return fmt.Errorf("failed to move %s to %s: %w", sourcePath, destinationPath, err)
	}

	duration := time.Since(startTime)
	bo.GetLogger().Debug(fmt.Sprintf("⏱️ [WORKSPACE] MoveWorkspaceFile(%s -> %s) completed successfully (took %v)", sourcePath, destinationPath, duration))
	return nil
}

// CleanupDirectory recursively deletes all files and directories in a directory using list_workspace_files
// to enumerate files recursively, then deletes all files first, then directories (deepest first)
func (bo *BaseOrchestrator) CleanupDirectory(ctx context.Context, dirPath string, dirName string) error {
	// Removed verbose logging

	// Use list_workspace_files to enumerate all files in the directory recursively, then delete them
	listExecutorInterface, exists := bo.WorkspaceToolExecutors["list_workspace_files"]
	if !exists {
		bo.GetLogger().Warn("⚠️ list_workspace_files executor not found, skipping directory cleanup")
		return nil
	}

	listExecutor, ok := listExecutorInterface.(func(context.Context, map[string]interface{}) (string, error))
	if !ok {
		bo.GetLogger().Warn("⚠️ list_workspace_files executor has wrong type, skipping directory cleanup")
		return nil
	}

	// Inject event emitter into context before calling executor
	ctx = context.WithValue(ctx, virtualtools.WorkspaceEventEmitterKey, bo.contextAwareBridge)

	// Call list_workspace_files to get all files recursively (use high max_depth for recursive listing)
	listArgs := map[string]interface{}{
		"folder":    dirPath,
		"max_depth": 100, // High depth to list all files and directories recursively
	}

	// Removed verbose logging
	fileListJSON, err := listExecutor(ctx, listArgs)
	if err != nil {
		bo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to list files in %s directory: %v (directory may not exist or be empty)", dirPath, err))
		return nil // Don't fail - directory may be empty or not exist
	}

	// Removed verbose logging

	// Parse the JSON response using shared helper that handles all known formats
	filesList, parseErr := virtualtools.ParseWorkspaceFilesList(fileListJSON)
	if parseErr != nil {
		bo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to parse file list JSON from %s directory: %v", dirPath, parseErr))
		return nil
	}

	// Flatten nested workspace API response: the API returns a tree structure
	// but CleanupDirectory needs a flat list of all files and folders at all depths
	filesList = flattenWorkspaceFiles(filesList)

	if len(filesList) == 0 {
		bo.GetLogger().Info(fmt.Sprintf("ℹ️ No files found in %s directory (may be empty)", dirName))
		return nil
	}

	// Separate files and directories for proper deletion order
	var filesToDelete []string
	var dirsToDelete []string

	// Removed verbose logging

	for _, fileInfo := range filesList {
		filepath := fileInfo.FilePath
		if filepath == "" {
			// Removed verbose logging
			continue
		}

		// Skip the root directory itself (normalize paths for comparison)
		// Normalize both paths by removing trailing slashes and comparing
		normalizedFilePath := strings.TrimRight(filepath, "/")
		normalizedDirPath := strings.TrimRight(dirPath, "/")
		if normalizedFilePath == normalizedDirPath {
			// This is the root directory itself - skip it to avoid deleting the Downloads folder
			bo.GetLogger().Info(fmt.Sprintf("⏭️ Skipping root directory itself: %s", filepath))
			continue
		}

		// Skip knowledgebase folder - it should never be deleted during cleanup
		// Check if the filepath contains "/knowledgebase" (case-insensitive)
		if strings.Contains(strings.ToLower(normalizedFilePath), "/knowledgebase") {
			bo.GetLogger().Info(fmt.Sprintf("🔒 Skipping protected knowledgebase folder: %s", filepath))
			continue
		}

		// Check if it's a directory
		if fileInfo.Type == "folder" {
			dirsToDelete = append(dirsToDelete, filepath)
			bo.GetLogger().Info(fmt.Sprintf("📁 Found directory to delete: %s", filepath))
		} else {
			filesToDelete = append(filesToDelete, filepath)
			bo.GetLogger().Info(fmt.Sprintf("📄 Found file to delete: %s", filepath))
		}
	}

	bo.GetLogger().Info(fmt.Sprintf("📊 Summary: %d files and %d directories to delete from %s", len(filesToDelete), len(dirsToDelete), dirName))

	// Delete all files first
	deletedFileCount := 0
	if len(filesToDelete) > 0 {
		bo.GetLogger().Info(fmt.Sprintf("🗑️ Starting to delete %d files from %s", len(filesToDelete), dirName))
	}
	for _, filepath := range filesToDelete {
		bo.GetLogger().Info(fmt.Sprintf("🗑️ Attempting to delete file: %s", filepath))
		if err := bo.DeleteWorkspaceFile(ctx, filepath); err == nil {
			deletedFileCount++
			bo.GetLogger().Info(fmt.Sprintf("✅ Successfully deleted file: %s", filepath))
		} else {
			// Log but don't fail - some files might already be deleted or have other issues
			bo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to delete file %s: %v", filepath, err))
		}
	}

	// Delete directories (deepest first - sort by path length descending)
	// This ensures child directories are deleted before parent directories
	sortKey := func(path string) int {
		// Count path separators to determine depth
		count := 0
		for _, char := range path {
			if char == '/' || char == '\\' {
				count++
			}
		}
		return count
	}

	// Sort directories by depth (deepest first)
	for i := 0; i < len(dirsToDelete)-1; i++ {
		for j := i + 1; j < len(dirsToDelete); j++ {
			if sortKey(dirsToDelete[i]) < sortKey(dirsToDelete[j]) {
				dirsToDelete[i], dirsToDelete[j] = dirsToDelete[j], dirsToDelete[i]
			}
		}
	}

	deletedDirCount := 0
	for _, dirpath := range dirsToDelete {
		// Delete directory using DeleteWorkspaceFile (workspace tool should handle directories)
		if err := bo.DeleteWorkspaceFile(ctx, dirpath); err == nil {
			deletedDirCount++
			bo.GetLogger().Info(fmt.Sprintf("🗑️ Deleted directory: %s", dirpath))
		} else {
			// Check if error is because directory is not empty
			errStr := err.Error()
			if strings.Contains(errStr, "directory not empty") {
				// Directory still has contents - recursively clean it first
				// Extract directory name for logging
				dirName := filepath.Base(dirpath)
				bo.GetLogger().Info(fmt.Sprintf("🔄 Directory %s not empty, recursively cleaning contents first", dirpath))
				// Recursively clean the directory to ensure all contents are deleted
				if err2 := bo.CleanupDirectory(ctx, dirpath, dirName); err2 == nil {
					// After recursive cleanup, try to delete the directory itself again
					if err3 := bo.DeleteWorkspaceFile(ctx, dirpath); err3 == nil {
						deletedDirCount++
						bo.GetLogger().Info(fmt.Sprintf("🗑️ Deleted directory after recursive cleanup: %s", dirpath))
					} else {
						bo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to delete directory %s even after recursive cleanup: %v", dirpath, err3))
					}
				} else {
					bo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to recursively cleanup directory %s: %v", dirpath, err2))
				}
			} else if strings.Contains(errStr, "not found") || strings.Contains(errStr, "no such file") {
				// Directory already deleted or doesn't exist - that's okay
				bo.GetLogger().Info(fmt.Sprintf("ℹ️ Directory %s already deleted or doesn't exist", dirpath))
			} else {
				// Other error - log but don't fail
				bo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to delete directory %s: %v", dirpath, err))
			}
		}
	}

	totalDeleted := deletedFileCount + deletedDirCount
	if totalDeleted > 0 {
		bo.GetLogger().Info(fmt.Sprintf("✅ Cleaned up %d files and %d directories from %s directory (total: %d)", deletedFileCount, deletedDirCount, dirName, totalDeleted))
	} else {
		bo.GetLogger().Info(fmt.Sprintf("ℹ️ No files or directories found to delete in %s directory (may have been empty)", dirName))
	}

	return nil
}

// ListWorkspaceDirectories lists all directories in a given path
// Returns a slice of directory names (not full paths)
// IMPORTANT: Callers should pass RELATIVE paths (e.g., "runs").
// The workspacePath is automatically prepended for relative paths.
func (bo *BaseOrchestrator) ListWorkspaceDirectories(ctx context.Context, dirPath string) ([]string, error) {
	startTime := time.Now()

	// If path is relative and we have a workspace path, prepend it
	workspacePath := bo.GetWorkspacePath()
	if workspacePath != "" && !filepath.IsAbs(dirPath) {
		// DEFENSIVE FIX: Check if the relative path already starts with workspacePath
		// This prevents double-prepending bugs (e.g., "ws/ws/folder")
		if strings.HasPrefix(dirPath, workspacePath+"/") || dirPath == workspacePath {
			bo.GetLogger().Warn(fmt.Sprintf("⚠️ ListWorkspaceDirectories: path '%s' already contains workspace path '%s' - using as-is to prevent double-prepend. Please fix caller to use relative path only.", dirPath, workspacePath))
			// Don't prepend - use path as-is
		} else {
			// Join workspace path with relative dir path
			dirPath = filepath.Join(workspacePath, dirPath)
		}
	}

	bo.GetLogger().Info(fmt.Sprintf("📁 Listing directories in: %s", dirPath))

	// Use list_workspace_files to enumerate directories
	listExecutorInterface, exists := bo.WorkspaceToolExecutors["list_workspace_files"]
	if !exists {
		duration := time.Since(startTime)
		bo.GetLogger().Warn(fmt.Sprintf("⏱️ [WORKSPACE] ListWorkspaceDirectories(%s) failed: executor not found (took %v)", dirPath, duration))
		return []string{}, nil
	}

	listExecutor, ok := listExecutorInterface.(func(context.Context, map[string]interface{}) (string, error))
	if !ok {
		duration := time.Since(startTime)
		bo.GetLogger().Warn(fmt.Sprintf("⏱️ [WORKSPACE] ListWorkspaceDirectories(%s) failed: executor wrong type (took %v)", dirPath, duration))
		return []string{}, nil
	}

	// Call list_workspace_files with max_depth: 1 to only get immediate children
	// Inject event emitter into context before calling executor
	ctx = context.WithValue(ctx, virtualtools.WorkspaceEventEmitterKey, bo.contextAwareBridge)

	// Call list_workspace_files with max_depth: 1 to only get immediate children
	listArgs := map[string]interface{}{
		"folder":    dirPath,
		"max_depth": 1, // Only list immediate children (directories)
	}

	fileListJSON, err := listExecutor(ctx, listArgs)
	if err != nil {
		duration := time.Since(startTime)
		bo.GetLogger().Warn(fmt.Sprintf("⏱️ [WORKSPACE] ListWorkspaceDirectories(%s) failed: %v (took %v)", dirPath, err, duration))
		return []string{}, nil // Don't fail - directory may be empty or not exist
	}

	// Parse the JSON response using shared helper that handles all known formats
	filesList, parseErr := virtualtools.ParseWorkspaceFilesList(fileListJSON)
	if parseErr != nil {
		duration := time.Since(startTime)
		bo.GetLogger().Info(fmt.Sprintf("⏱️ [WORKSPACE] ListWorkspaceDirectories(%s) completed: no directories found (took %v)", dirPath, duration))
		return []string{}, nil
	}

	// Extract only directories (folders) from the list
	var directoryNames []string
	for _, fileInfo := range filesList {
		filepath := fileInfo.FilePath
		if filepath == "" {
			continue
		}

		// Check if it's a directory
		if fileInfo.Type != "folder" {
			continue
		}

		// Skip the directory itself (if filepath equals dirPath)
		if filepath == dirPath {
			continue
		}

		// Extract directory name (last part of path)
		// filepath will be like "workspace/runs/initial" or "runs/initial"
		// We want just "initial"
		dirName := filepath
		if strings.Contains(dirName, "/") {
			parts := strings.Split(dirName, "/")
			dirName = parts[len(parts)-1]
		}

		// Skip if it's empty
		if dirName != "" {
			directoryNames = append(directoryNames, dirName)
		}
	}

	duration := time.Since(startTime)
	bo.GetLogger().Info(fmt.Sprintf("⏱️ [WORKSPACE] ListWorkspaceDirectories(%s) completed: found %d directories (took %v)", dirPath, len(directoryNames), duration))
	return directoryNames, nil
}

// ListWorkspaceFiles lists all files and directories in a given path
// Returns a slice of file/directory names (not full paths)
// IMPORTANT: Callers should pass RELATIVE paths (e.g., "learnings/step-1").
// The workspacePath is automatically prepended for relative paths.
func (bo *BaseOrchestrator) ListWorkspaceFiles(ctx context.Context, dirPath string) ([]string, error) {
	startTime := time.Now()

	// If path is relative and we have a workspace path, prepend it
	workspacePath := bo.GetWorkspacePath()
	if workspacePath != "" && !filepath.IsAbs(dirPath) {
		// DEFENSIVE FIX: Check if the relative path already starts with workspacePath
		// This prevents double-prepending bugs (e.g., "ws/ws/folder")
		if strings.HasPrefix(dirPath, workspacePath+"/") || dirPath == workspacePath {
			bo.GetLogger().Warn(fmt.Sprintf("⚠️ ListWorkspaceFiles: path '%s' already contains workspace path '%s' - using as-is to prevent double-prepend. Please fix caller to use relative path only.", dirPath, workspacePath))
			// Don't prepend - use path as-is
		} else {
			// Join workspace path with relative dir path
			dirPath = filepath.Join(workspacePath, dirPath)
		}
	}

	bo.GetLogger().Info(fmt.Sprintf("📁 Listing files and directories in: %s", dirPath))

	// Use list_workspace_files to enumerate files and directories
	listExecutorInterface, exists := bo.WorkspaceToolExecutors["list_workspace_files"]
	if !exists {
		duration := time.Since(startTime)
		bo.GetLogger().Warn(fmt.Sprintf("⏱️ [WORKSPACE] ListWorkspaceFiles(%s) failed: executor not found (took %v)", dirPath, duration))
		return []string{}, nil
	}

	listExecutor, ok := listExecutorInterface.(func(context.Context, map[string]interface{}) (string, error))
	if !ok {
		duration := time.Since(startTime)
		bo.GetLogger().Warn(fmt.Sprintf("⏱️ [WORKSPACE] ListWorkspaceFiles(%s) failed: executor wrong type (took %v)", dirPath, duration))
		return []string{}, nil
	}

	// Inject event emitter into context before calling executor
	ctx = context.WithValue(ctx, virtualtools.WorkspaceEventEmitterKey, bo.contextAwareBridge)

	// Call list_workspace_files with max_depth: 1 to only get immediate children
	listArgs := map[string]interface{}{
		"folder":    dirPath,
		"max_depth": 1, // Only list immediate children
	}

	fileListJSON, err := listExecutor(ctx, listArgs)
	if err != nil {
		duration := time.Since(startTime)
		// Check if error indicates folder doesn't exist
		errStr := err.Error()
		if strings.Contains(errStr, "does not exist") ||
			strings.Contains(errStr, "not found") ||
			strings.Contains(errStr, "Folder does not exist") ||
			strings.Contains(errStr, "Folder not found") {
			// Return error for non-existent folders
			bo.GetLogger().Warn(fmt.Sprintf("⏱️ [WORKSPACE] ListWorkspaceFiles(%s) failed: folder does not exist (took %v)", dirPath, duration))
			return nil, fmt.Errorf("folder does not exist: %s", dirPath)
		}
		// For other errors, log and return empty (backward compatibility)
		bo.GetLogger().Warn(fmt.Sprintf("⏱️ [WORKSPACE] ListWorkspaceFiles(%s) failed: %v (took %v)", dirPath, err, duration))
		return []string{}, nil
	}

	// Check response string for "does not exist" messages (in case error wasn't returned)
	// But exclude "exists but contains no files" which is a valid empty folder case
	if (strings.Contains(fileListJSON, "Folder does not exist") ||
		strings.Contains(fileListJSON, "does not exist")) &&
		!strings.Contains(fileListJSON, "exists but contains no files") {
		duration := time.Since(startTime)
		bo.GetLogger().Warn(fmt.Sprintf("⏱️ [WORKSPACE] ListWorkspaceFiles(%s) failed: folder does not exist (took %v)", dirPath, duration))
		return nil, fmt.Errorf("folder does not exist: %s", dirPath)
	}

	// Handle empty folder case (executor returns a message string, not JSON)
	if strings.Contains(fileListJSON, "exists but contains no files") {
		duration := time.Since(startTime)
		bo.GetLogger().Info(fmt.Sprintf("⏱️ [WORKSPACE] ListWorkspaceFiles(%s) completed: folder exists but is empty (took %v)", dirPath, duration))
		return []string{}, nil
	}

	// Parse the JSON response using shared helper that handles all known formats
	filesList, parseErr := virtualtools.ParseWorkspaceFilesList(fileListJSON)
	if parseErr != nil {
		duration := time.Since(startTime)
		bo.GetLogger().Info(fmt.Sprintf("⏱️ [WORKSPACE] ListWorkspaceFiles(%s) completed: no files found (took %v)", dirPath, duration))
		return []string{}, nil
	}

	// Unwrap nested workspace API response: if the response is a single root entry
	// matching the requested folder with children, return the children instead.
	// The workspace API returns {data: [{filepath: "folder", type: "folder", children: [...]}]}
	// and we need the children, not the wrapper entry.
	filesList = unwrapWorkspaceFilesList(filesList, dirPath)

	// Extract file and directory names (last part of path)
	var names []string
	for _, fileInfo := range filesList {
		filepath := fileInfo.FilePath
		if filepath == "" {
			continue
		}

		// Skip the directory itself (if filepath equals dirPath)
		if filepath == dirPath {
			continue
		}

		// Extract name (last part of path)
		name := filepath
		if strings.Contains(name, "/") {
			parts := strings.Split(name, "/")
			name = parts[len(parts)-1]
		}

		// Skip if it's empty
		if name != "" {
			names = append(names, name)
		}
	}

	duration := time.Since(startTime)
	bo.GetLogger().Info(fmt.Sprintf("⏱️ [WORKSPACE] ListWorkspaceFiles(%s) completed: found %d files/directories (took %v)", dirPath, len(names), duration))
	return names, nil
}

// unwrapWorkspaceFilesList handles the nested workspace API response format.
// The workspace API returns data like [{filepath: "folder", type: "folder", children: [...]}]
// where the root entry is a wrapper around the actual children. This function unwraps
// the children so callers get the actual folder contents.
func unwrapWorkspaceFilesList(filesList []virtualtools.WorkspaceFile, dirPath string) []virtualtools.WorkspaceFile {
	// If the response is a single root entry matching the requested folder with children, return children
	if len(filesList) == 1 && filesList[0].Type == "folder" && filesList[0].FilePath == dirPath && len(filesList[0].Children) > 0 {
		return filesList[0].Children
	}

	// Also handle multi-entry responses where some entries are wrappers
	var flattened []virtualtools.WorkspaceFile
	for _, entry := range filesList {
		if entry.Type == "folder" && entry.FilePath == dirPath && len(entry.Children) > 0 {
			flattened = append(flattened, entry.Children...)
		} else {
			flattened = append(flattened, entry)
		}
	}
	if len(flattened) > 0 {
		return flattened
	}

	return filesList
}

// flattenWorkspaceFiles recursively flattens a nested tree of WorkspaceFile entries
// into a flat list. The workspace API returns nested trees with Children fields,
// but callers like CleanupDirectory need a flat list of all files and folders.
func flattenWorkspaceFiles(files []virtualtools.WorkspaceFile) []virtualtools.WorkspaceFile {
	var result []virtualtools.WorkspaceFile
	for _, f := range files {
		result = append(result, f)
		if len(f.Children) > 0 {
			result = append(result, flattenWorkspaceFiles(f.Children)...)
		}
	}
	return result
}

