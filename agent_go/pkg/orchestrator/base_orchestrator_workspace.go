package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	virtualtools "mcp-agent/agent_go/cmd/server/virtual-tools"
)

// ReadWorkspaceFile reads a file from the workspace using MCP tools
func (bo *BaseOrchestrator) ReadWorkspaceFile(ctx context.Context, filePath string) (string, error) {
	// Removed verbose logging

	// Prepare tool call parameters (MCP tools expect map[string]interface{})
	readArgs := map[string]interface{}{
		"filepath": filePath,
	}

	// Get the tool executor
	readExecutorInterface, exists := bo.WorkspaceToolExecutors["read_workspace_file"]
	if !exists {
		return "", fmt.Errorf(fmt.Sprintf("read_workspace_file tool executor not found"), nil)
	}

	readExecutor, ok := readExecutorInterface.(func(context.Context, map[string]interface{}) (string, error))
	if !ok {
		return "", fmt.Errorf(fmt.Sprintf("read_workspace_file tool executor has wrong type"), nil)
	}

	// Inject event emitter into context before calling executor
	ctx = context.WithValue(ctx, virtualtools.WorkspaceEventEmitterKey, bo.contextAwareBridge)

	// Execute the tool call using existing workspace tool logic
	readResult, err := readExecutor(ctx, readArgs)
	if err != nil {
		return "", fmt.Errorf(fmt.Sprintf("failed to read file %s: %w", filePath, err), nil)
	}

	// Parse the response using proper type from virtualtools
	var fileData virtualtools.WorkspaceFileContent
	if err := json.Unmarshal([]byte(readResult), &fileData); err != nil {
		return "", fmt.Errorf(fmt.Sprintf("failed to parse workspace response: %w", err), nil)
	}

	// Extract content directly from the parsed data
	fileContent := fileData.Content

	if fileContent == "" {
		return "", fmt.Errorf(fmt.Sprintf("no content found in workspace response"), nil)
	}

	// Removed verbose logging
	return fileContent, nil
}

// CheckWorkspaceFileExists checks if a file exists in the workspace
// Uses ReadWorkspaceFile internally but returns a boolean instead of content
func (bo *BaseOrchestrator) CheckWorkspaceFileExists(ctx context.Context, filePath string) (bool, error) {
	// Removed verbose logging

	_, err := bo.ReadWorkspaceFile(ctx, filePath)
	if err != nil {
		// Check if it's a "file not found" error vs other errors
		if strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "no such file") {
			// Removed verbose logging
			return false, nil
		}
		// Other errors should be returned
		return false, err
	}

	// Removed verbose logging
	return true, nil
}

// WriteWorkspaceFile writes content to a file in the workspace using MCP tools
func (bo *BaseOrchestrator) WriteWorkspaceFile(ctx context.Context, filePath string, content string) error {
	// Removed verbose logging

	// Prepare tool call parameters (MCP tools expect map[string]interface{})
	writeArgs := map[string]interface{}{
		"filepath": filePath,
		"content":  content,
	}

	// Get the tool executor
	writeExecutorInterface, exists := bo.WorkspaceToolExecutors["update_workspace_file"]
	if !exists {
		return fmt.Errorf(fmt.Sprintf("update_workspace_file tool executor not found"), nil)
	}

	writeExecutor, ok := writeExecutorInterface.(func(context.Context, map[string]interface{}) (string, error))
	if !ok {
		return fmt.Errorf(fmt.Sprintf("update_workspace_file tool executor has wrong type"), nil)
	}

	// Inject event emitter into context before calling executor
	ctx = context.WithValue(ctx, virtualtools.WorkspaceEventEmitterKey, bo.contextAwareBridge)

	// Execute the tool call using existing workspace tool logic
	_, err := writeExecutor(ctx, writeArgs)
	if err != nil {
		return fmt.Errorf(fmt.Sprintf("failed to write file %s: %w", filePath, err), nil)
	}

	// Removed verbose logging
	return nil
}

// DeleteWorkspaceFile deletes a file from the workspace using MCP tools
func (bo *BaseOrchestrator) DeleteWorkspaceFile(ctx context.Context, filePath string) error {
	// Removed verbose logging

	// Prepare tool call parameters (MCP tools expect map[string]interface{})
	deleteArgs := map[string]interface{}{
		"filepath": filePath,
	}

	// Get the tool executor
	deleteExecutorInterface, exists := bo.WorkspaceToolExecutors["delete_workspace_file"]
	if !exists {
		return fmt.Errorf(fmt.Sprintf("delete_workspace_file tool executor not found"), nil)
	}

	deleteExecutor, ok := deleteExecutorInterface.(func(context.Context, map[string]interface{}) (string, error))
	if !ok {
		return fmt.Errorf(fmt.Sprintf("delete_workspace_file tool executor has wrong type"), nil)
	}

	// Inject event emitter into context before calling executor
	ctx = context.WithValue(ctx, virtualtools.WorkspaceEventEmitterKey, bo.contextAwareBridge)

	// Execute the tool call using existing workspace tool logic
	_, err := deleteExecutor(ctx, deleteArgs)
	if err != nil {
		return fmt.Errorf(fmt.Sprintf("failed to delete file %s: %w", filePath, err), nil)
	}

	// Removed verbose logging
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

	// Parse the JSON response using proper WorkspaceFile type from virtualtools
	var filesList []virtualtools.WorkspaceFile
	if err := json.Unmarshal([]byte(fileListJSON), &filesList); err != nil {
		bo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to parse file list JSON from %s directory: %v", dirPath, err))
		// Try alternative format - might be a single object with a "files" array
		var altFormat struct {
			Files []virtualtools.WorkspaceFile `json:"files"`
		}
		if err2 := json.Unmarshal([]byte(fileListJSON), &altFormat); err2 == nil && len(altFormat.Files) > 0 {
			filesList = altFormat.Files
		}
		if len(filesList) == 0 {
			// Removed verbose logging
			return nil
		}
	}

	if len(filesList) == 0 {
		bo.GetLogger().Info(fmt.Sprintf("ℹ️ No files found in %s directory (may be empty)", dirName))
		return nil
	}

	// Separate files and directories for proper deletion order
	var filesToDelete []string
	var dirsToDelete []string

	// Removed verbose logging

	for _, fileInfo := range filesList {
		filepath := fileInfo.Filepath
		if filepath == "" {
			// Removed verbose logging
			continue
		}

		// Skip the root directory itself (exact match)
		if filepath == dirPath {
			// Removed verbose logging
			continue
		}

		// Check if it's a directory
		if fileInfo.IsDirectory {
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
func (bo *BaseOrchestrator) ListWorkspaceDirectories(ctx context.Context, dirPath string) ([]string, error) {
	bo.GetLogger().Info(fmt.Sprintf("📁 Listing directories in: %s", dirPath))

	// Use list_workspace_files to enumerate directories
	listExecutorInterface, exists := bo.WorkspaceToolExecutors["list_workspace_files"]
	if !exists {
		bo.GetLogger().Warn(fmt.Sprintf("⚠️ list_workspace_files executor not found, returning empty list"))
		return []string{}, nil
	}

	listExecutor, ok := listExecutorInterface.(func(context.Context, map[string]interface{}) (string, error))
	if !ok {
		bo.GetLogger().Warn(fmt.Sprintf("⚠️ list_workspace_files executor has wrong type, returning empty list"))
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

	bo.GetLogger().Info(fmt.Sprintf("🔍 DEBUG ListWorkspaceDirectories: Calling list_workspace_files with folder=%s, max_depth=1", dirPath))
	fileListJSON, err := listExecutor(ctx, listArgs)
	bo.GetLogger().Info(fmt.Sprintf("🔍 DEBUG ListWorkspaceDirectories: list_workspace_files returned, error=%v, response_length=%d", err, len(fileListJSON)))
	if err != nil {
		bo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to list files in %s directory: %v (directory may not exist or be empty)", dirPath, err))
		return []string{}, nil // Don't fail - directory may be empty or not exist
	}

	// Parse the JSON response using proper WorkspaceFile type from virtualtools
	var filesList []virtualtools.WorkspaceFile
	if err := json.Unmarshal([]byte(fileListJSON), &filesList); err != nil {
		bo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to parse file list JSON from %s directory: %v", dirPath, err))
		// Try alternative format - might be a single object with a "files" array
		var altFormat struct {
			Files []virtualtools.WorkspaceFile `json:"files"`
		}
		if err2 := json.Unmarshal([]byte(fileListJSON), &altFormat); err2 == nil && len(altFormat.Files) > 0 {
			filesList = altFormat.Files
		}
		if len(filesList) == 0 {
			bo.GetLogger().Info(fmt.Sprintf("ℹ️ No files found in %s directory (may be empty)", dirPath))
			return []string{}, nil
		}
	}

	// Extract only directories (folders) from the list
	var directoryNames []string
	for _, fileInfo := range filesList {
		filepath := fileInfo.Filepath
		if filepath == "" {
			continue
		}

		// Check if it's a directory
		if !fileInfo.IsDirectory {
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

	bo.GetLogger().Info(fmt.Sprintf("📁 Found %d directories: %v", len(directoryNames), directoryNames))
	return directoryNames, nil
}

// ListWorkspaceFiles lists all files and directories in a given path
// Returns a slice of file/directory names (not full paths)
func (bo *BaseOrchestrator) ListWorkspaceFiles(ctx context.Context, dirPath string) ([]string, error) {
	bo.GetLogger().Info(fmt.Sprintf("📁 Listing files and directories in: %s", dirPath))

	// Use list_workspace_files to enumerate files and directories
	listExecutorInterface, exists := bo.WorkspaceToolExecutors["list_workspace_files"]
	if !exists {
		bo.GetLogger().Warn(fmt.Sprintf("⚠️ list_workspace_files executor not found, returning empty list"))
		return []string{}, nil
	}

	listExecutor, ok := listExecutorInterface.(func(context.Context, map[string]interface{}) (string, error))
	if !ok {
		bo.GetLogger().Warn(fmt.Sprintf("⚠️ list_workspace_files executor has wrong type, returning empty list"))
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
		bo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to list files in %s directory: %v (directory may not exist or be empty)", dirPath, err))
		return []string{}, nil // Don't fail - directory may be empty or not exist
	}

	// Parse the JSON response using proper WorkspaceFile type from virtualtools
	var filesList []virtualtools.WorkspaceFile
	if err := json.Unmarshal([]byte(fileListJSON), &filesList); err != nil {
		bo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to parse file list JSON from %s directory: %v", dirPath, err))
		// Try alternative format - might be a single object with a "files" array
		var altFormat struct {
			Files []virtualtools.WorkspaceFile `json:"files"`
		}
		if err2 := json.Unmarshal([]byte(fileListJSON), &altFormat); err2 == nil && len(altFormat.Files) > 0 {
			filesList = altFormat.Files
		}
		if len(filesList) == 0 {
			bo.GetLogger().Info(fmt.Sprintf("ℹ️ No files found in %s directory (may be empty)", dirPath))
			return []string{}, nil
		}
	}

	// Extract file and directory names (last part of path)
	var names []string
	for _, fileInfo := range filesList {
		filepath := fileInfo.Filepath
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

	bo.GetLogger().Info(fmt.Sprintf("📁 Found %d files/directories: %v", len(names), names))
	return names, nil
}
