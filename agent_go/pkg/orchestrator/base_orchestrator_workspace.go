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

	// If path is relative and we have a workspace path, prepend it (same logic as Read/Write).
	workspacePath := bo.GetWorkspacePath()
	if workspacePath != "" && !filepath.IsAbs(filePath) {
		if strings.HasPrefix(filePath, workspacePath+"/") || filePath == workspacePath {
			bo.GetLogger().Warn(fmt.Sprintf("⚠️ DeleteWorkspaceFile: path '%s' already contains workspace path '%s' - using as-is to prevent double-prepend.", filePath, workspacePath))
		} else {
			filePath = filepath.Join(workspacePath, filePath)
		}
	}

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

// CleanupDirectory deletes all contents of a directory via the workspace API.
// The workspace API uses os.RemoveAll for directories, so this is a single delete call.
// NOTE: This deletes the directory itself. Callers that need the empty folder to remain
// should re-create it afterwards.
func (bo *BaseOrchestrator) CleanupDirectory(ctx context.Context, dirPath string, dirName string) error {
	if err := bo.DeleteWorkspaceFile(ctx, dirPath); err != nil {
		errStr := err.Error()
		if strings.Contains(errStr, "not found") || strings.Contains(errStr, "no such file") {
			bo.GetLogger().Info(fmt.Sprintf("ℹ️ Directory %s already deleted or doesn't exist", dirPath))
			return nil
		}
		bo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to cleanup directory %s: %v", dirPath, err))
		return err
	}
	bo.GetLogger().Info(fmt.Sprintf("✅ Cleaned up %s directory", dirName))
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

