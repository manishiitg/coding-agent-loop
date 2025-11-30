package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// ReadWorkspaceFile reads a file from the workspace using MCP tools
func (bo *BaseOrchestrator) ReadWorkspaceFile(ctx context.Context, filePath string) (string, error) {
	bo.GetLogger().Infof("📖 Reading workspace file: %s", filePath)

	// Prepare tool call parameters
	readArgs := map[string]interface{}{
		"filepath": filePath,
	}

	// Get the tool executor
	readExecutorInterface, exists := bo.WorkspaceToolExecutors["read_workspace_file"]
	if !exists {
		return "", fmt.Errorf("read_workspace_file tool executor not found")
	}

	readExecutor, ok := readExecutorInterface.(func(context.Context, map[string]interface{}) (string, error))
	if !ok {
		return "", fmt.Errorf("read_workspace_file tool executor has wrong type")
	}

	// Inject event emitter into context before calling executor
	ctx = context.WithValue(ctx, "workspace_event_emitter", bo.contextAwareBridge)

	// Execute the tool call using existing workspace tool logic
	readResult, err := readExecutor(ctx, readArgs)
	if err != nil {
		return "", fmt.Errorf("failed to read file %s: %w", filePath, err)
	}

	// Parse the response - handleReadWorkspaceFile returns only the Data field from API response
	var fileData struct {
		Filepath string `json:"filepath"`
		Content  string `json:"content"`
	}

	if err := json.Unmarshal([]byte(readResult), &fileData); err != nil {
		return "", fmt.Errorf("failed to parse workspace response: %w", err)
	}

	// Extract content directly from the parsed data
	fileContent := fileData.Content

	if fileContent == "" {
		return "", fmt.Errorf("no content found in workspace response")
	}

	bo.GetLogger().Infof("✅ Successfully read file: %s (%d characters)", filePath, len(fileContent))
	return fileContent, nil
}

// CheckWorkspaceFileExists checks if a file exists in the workspace
// Uses ReadWorkspaceFile internally but returns a boolean instead of content
func (bo *BaseOrchestrator) CheckWorkspaceFileExists(ctx context.Context, filePath string) (bool, error) {
	bo.GetLogger().Infof("🔍 Checking if workspace file exists: %s", filePath)

	_, err := bo.ReadWorkspaceFile(ctx, filePath)
	if err != nil {
		// Check if it's a "file not found" error vs other errors
		if strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "no such file") {
			bo.GetLogger().Infof("📋 File does not exist: %s", filePath)
			return false, nil
		}
		// Other errors should be returned
		return false, err
	}

	bo.GetLogger().Infof("✅ File exists: %s", filePath)
	return true, nil
}

// WriteWorkspaceFile writes content to a file in the workspace using MCP tools
func (bo *BaseOrchestrator) WriteWorkspaceFile(ctx context.Context, filePath string, content string) error {
	bo.GetLogger().Infof("📝 Writing workspace file: %s (%d characters)", filePath, len(content))

	// Prepare tool call parameters
	writeArgs := map[string]interface{}{
		"filepath": filePath,
		"content":  content,
	}

	// Get the tool executor
	writeExecutorInterface, exists := bo.WorkspaceToolExecutors["update_workspace_file"]
	if !exists {
		return fmt.Errorf("update_workspace_file tool executor not found")
	}

	writeExecutor, ok := writeExecutorInterface.(func(context.Context, map[string]interface{}) (string, error))
	if !ok {
		return fmt.Errorf("update_workspace_file tool executor has wrong type")
	}

	// Inject event emitter into context before calling executor
	ctx = context.WithValue(ctx, "workspace_event_emitter", bo.contextAwareBridge)

	// Execute the tool call using existing workspace tool logic
	_, err := writeExecutor(ctx, writeArgs)
	if err != nil {
		return fmt.Errorf("failed to write file %s: %w", filePath, err)
	}

	bo.GetLogger().Infof("✅ Successfully wrote file: %s (%d characters)", filePath, len(content))
	return nil
}

// DeleteWorkspaceFile deletes a file from the workspace using MCP tools
func (bo *BaseOrchestrator) DeleteWorkspaceFile(ctx context.Context, filePath string) error {
	bo.GetLogger().Infof("🗑️ Deleting workspace file: %s", filePath)

	// Prepare tool call parameters
	deleteArgs := map[string]interface{}{
		"filepath": filePath,
	}

	// Get the tool executor
	deleteExecutorInterface, exists := bo.WorkspaceToolExecutors["delete_workspace_file"]
	if !exists {
		return fmt.Errorf("delete_workspace_file tool executor not found")
	}

	deleteExecutor, ok := deleteExecutorInterface.(func(context.Context, map[string]interface{}) (string, error))
	if !ok {
		return fmt.Errorf("delete_workspace_file tool executor has wrong type")
	}

	// Inject event emitter into context before calling executor
	ctx = context.WithValue(ctx, "workspace_event_emitter", bo.contextAwareBridge)

	// Execute the tool call using existing workspace tool logic
	_, err := deleteExecutor(ctx, deleteArgs)
	if err != nil {
		return fmt.Errorf("failed to delete file %s: %w", filePath, err)
	}

	bo.GetLogger().Infof("✅ Successfully deleted file: %s", filePath)
	return nil
}

// CleanupDirectory recursively deletes all files and directories in a directory using list_workspace_files
// to enumerate files recursively, then deletes all files first, then directories (deepest first)
func (bo *BaseOrchestrator) CleanupDirectory(ctx context.Context, dirPath string, dirName string) error {
	bo.GetLogger().Infof("🧹 Cleaning up %s directory recursively: %s", dirName, dirPath)

	// Use list_workspace_files to enumerate all files in the directory recursively, then delete them
	listExecutorInterface, exists := bo.WorkspaceToolExecutors["list_workspace_files"]
	if !exists {
		bo.GetLogger().Warnf("⚠️ list_workspace_files executor not found, skipping directory cleanup")
		return nil
	}

	listExecutor, ok := listExecutorInterface.(func(context.Context, map[string]interface{}) (string, error))
	if !ok {
		bo.GetLogger().Warnf("⚠️ list_workspace_files executor has wrong type, skipping directory cleanup")
		return nil
	}

	// Inject event emitter into context before calling executor
	ctx = context.WithValue(ctx, "workspace_event_emitter", bo.contextAwareBridge)

	// Call list_workspace_files to get all files recursively (use high max_depth for recursive listing)
	listArgs := map[string]interface{}{
		"folder":    dirPath,
		"max_depth": 100, // High depth to list all files and directories recursively
	}

	fileListJSON, err := listExecutor(ctx, listArgs)
	if err != nil {
		bo.GetLogger().Warnf("⚠️ Failed to list files in %s directory: %v (directory may not exist or be empty)", dirPath, err)
		return nil // Don't fail - directory may be empty or not exist
	}

	// Parse the JSON response to extract file paths
	var filesList []map[string]interface{}
	if err := json.Unmarshal([]byte(fileListJSON), &filesList); err != nil {
		bo.GetLogger().Warnf("⚠️ Failed to parse file list JSON from %s directory: %v", dirPath, err)
		// Try alternative format - might be a single object with a "files" array
		var altFormat map[string]interface{}
		if err2 := json.Unmarshal([]byte(fileListJSON), &altFormat); err2 == nil {
			if filesArray, ok := altFormat["files"].([]interface{}); ok {
				for _, fileInterface := range filesArray {
					if fileMap, ok := fileInterface.(map[string]interface{}); ok {
						filesList = append(filesList, fileMap)
					}
				}
			}
		}
		if len(filesList) == 0 {
			bo.GetLogger().Infof("ℹ️ No files found in %s directory (may be empty)", dirName)
			return nil
		}
	}

	// Separate files and directories for proper deletion order
	var filesToDelete []string
	var dirsToDelete []string

	for _, fileInfo := range filesList {
		filepath, ok := fileInfo["filepath"].(string)
		if !ok || filepath == "" {
			continue
		}

		// Skip the root directory itself
		if filepath == dirPath {
			continue
		}

		// Check if it's a directory
		if isDirectory, ok := fileInfo["is_directory"].(bool); ok && isDirectory {
			dirsToDelete = append(dirsToDelete, filepath)
		} else {
			filesToDelete = append(filesToDelete, filepath)
		}
	}

	// Delete all files first
	deletedFileCount := 0
	for _, filepath := range filesToDelete {
		if err := bo.DeleteWorkspaceFile(ctx, filepath); err == nil {
			deletedFileCount++
			bo.GetLogger().Infof("🗑️ Deleted file: %s", filepath)
		} else {
			// Log but don't fail - some files might already be deleted or have other issues
			bo.GetLogger().Warnf("⚠️ Failed to delete file %s: %v", filepath, err)
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
			bo.GetLogger().Infof("🗑️ Deleted directory: %s", dirpath)
		} else {
			// Log but don't fail - some directories might already be deleted or have other issues
			bo.GetLogger().Warnf("⚠️ Failed to delete directory %s: %v", dirpath, err)
		}
	}

	totalDeleted := deletedFileCount + deletedDirCount
	if totalDeleted > 0 {
		bo.GetLogger().Infof("✅ Cleaned up %d files and %d directories from %s directory (total: %d)", deletedFileCount, deletedDirCount, dirName, totalDeleted)
	} else {
		bo.GetLogger().Infof("ℹ️ No files or directories found to delete in %s directory (may have been empty)", dirName)
	}

	return nil
}

// ListWorkspaceDirectories lists all directories in a given path
// Returns a slice of directory names (not full paths)
func (bo *BaseOrchestrator) ListWorkspaceDirectories(ctx context.Context, dirPath string) ([]string, error) {
	bo.GetLogger().Infof("📁 Listing directories in: %s", dirPath)

	// Use list_workspace_files to enumerate directories
	listExecutorInterface, exists := bo.WorkspaceToolExecutors["list_workspace_files"]
	if !exists {
		bo.GetLogger().Warnf("⚠️ list_workspace_files executor not found, returning empty list")
		return []string{}, nil
	}

	listExecutor, ok := listExecutorInterface.(func(context.Context, map[string]interface{}) (string, error))
	if !ok {
		bo.GetLogger().Warnf("⚠️ list_workspace_files executor has wrong type, returning empty list")
		return []string{}, nil
	}

	// Call list_workspace_files with max_depth: 1 to only get immediate children
	// Inject event emitter into context before calling executor
	ctx = context.WithValue(ctx, "workspace_event_emitter", bo.contextAwareBridge)

	listArgs := map[string]interface{}{
		"folder":    dirPath,
		"max_depth": 1, // Only list immediate children (directories)
	}

	bo.GetLogger().Infof("🔍 DEBUG ListWorkspaceDirectories: Calling list_workspace_files with folder=%s, max_depth=1", dirPath)
	fileListJSON, err := listExecutor(ctx, listArgs)
	bo.GetLogger().Infof("🔍 DEBUG ListWorkspaceDirectories: list_workspace_files returned, error=%v, response_length=%d", err, len(fileListJSON))
	if err != nil {
		bo.GetLogger().Warnf("⚠️ Failed to list files in %s directory: %v (directory may not exist or be empty)", dirPath, err)
		return []string{}, nil // Don't fail - directory may be empty or not exist
	}

	// Parse the JSON response to extract file paths
	var filesList []map[string]interface{}
	if err := json.Unmarshal([]byte(fileListJSON), &filesList); err != nil {
		bo.GetLogger().Warnf("⚠️ Failed to parse file list JSON from %s directory: %v", dirPath, err)
		// Try alternative format - might be a single object with a "files" array
		var altFormat map[string]interface{}
		if err2 := json.Unmarshal([]byte(fileListJSON), &altFormat); err2 == nil {
			if filesArray, ok := altFormat["files"].([]interface{}); ok {
				for _, fileInterface := range filesArray {
					if fileMap, ok := fileInterface.(map[string]interface{}); ok {
						filesList = append(filesList, fileMap)
					}
				}
			}
		}
		if len(filesList) == 0 {
			bo.GetLogger().Infof("ℹ️ No files found in %s directory (may be empty)", dirPath)
			return []string{}, nil
		}
	}

	// Extract only directories (folders) from the list
	var directoryNames []string
	for _, fileInfo := range filesList {
		filepath, ok := fileInfo["filepath"].(string)
		if !ok || filepath == "" {
			continue
		}

		// Check if it's a directory
		isDirectory, ok := fileInfo["is_directory"].(bool)
		if !ok || !isDirectory {
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

	bo.GetLogger().Infof("📁 Found %d directories: %v", len(directoryNames), directoryNames)
	return directoryNames, nil
}

// ListWorkspaceFiles lists all files and directories in a given path
// Returns a slice of file/directory names (not full paths)
func (bo *BaseOrchestrator) ListWorkspaceFiles(ctx context.Context, dirPath string) ([]string, error) {
	bo.GetLogger().Infof("📁 Listing files and directories in: %s", dirPath)

	// Use list_workspace_files to enumerate files and directories
	listExecutorInterface, exists := bo.WorkspaceToolExecutors["list_workspace_files"]
	if !exists {
		bo.GetLogger().Warnf("⚠️ list_workspace_files executor not found, returning empty list")
		return []string{}, nil
	}

	listExecutor, ok := listExecutorInterface.(func(context.Context, map[string]interface{}) (string, error))
	if !ok {
		bo.GetLogger().Warnf("⚠️ list_workspace_files executor has wrong type, returning empty list")
		return []string{}, nil
	}

	// Inject event emitter into context before calling executor
	ctx = context.WithValue(ctx, "workspace_event_emitter", bo.contextAwareBridge)

	// Call list_workspace_files with max_depth: 1 to only get immediate children
	listArgs := map[string]interface{}{
		"folder":    dirPath,
		"max_depth": 1, // Only list immediate children
	}

	fileListJSON, err := listExecutor(ctx, listArgs)
	if err != nil {
		bo.GetLogger().Warnf("⚠️ Failed to list files in %s directory: %v (directory may not exist or be empty)", dirPath, err)
		return []string{}, nil // Don't fail - directory may be empty or not exist
	}

	// Parse the JSON response to extract file paths
	var filesList []map[string]interface{}
	if err := json.Unmarshal([]byte(fileListJSON), &filesList); err != nil {
		bo.GetLogger().Warnf("⚠️ Failed to parse file list JSON from %s directory: %v", dirPath, err)
		// Try alternative format - might be a single object with a "files" array
		var altFormat map[string]interface{}
		if err2 := json.Unmarshal([]byte(fileListJSON), &altFormat); err2 == nil {
			if filesArray, ok := altFormat["files"].([]interface{}); ok {
				for _, fileInterface := range filesArray {
					if fileMap, ok := fileInterface.(map[string]interface{}); ok {
						filesList = append(filesList, fileMap)
					}
				}
			}
		}
		if len(filesList) == 0 {
			bo.GetLogger().Infof("ℹ️ No files found in %s directory (may be empty)", dirPath)
			return []string{}, nil
		}
	}

	// Extract file and directory names (last part of path)
	var names []string
	for _, fileInfo := range filesList {
		filepath, ok := fileInfo["filepath"].(string)
		if !ok || filepath == "" {
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

	bo.GetLogger().Infof("📁 Found %d files/directories: %v", len(names), names)
	return names, nil
}
