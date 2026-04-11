package orchestrator

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"mcp-agent-builder-go/agent_go/pkg/workspace"

	virtualtools "mcp-agent-builder-go/agent_go/cmd/server/virtual-tools"
)

// resolveWorkspacePath prepends the workspace path to relative paths, with double-prepend protection.
func (bo *BaseOrchestrator) resolveWorkspacePath(filePath string) string {
	workspacePath := bo.GetWorkspacePath()
	if workspacePath == "" || filepath.IsAbs(filePath) {
		return filePath
	}
	if strings.HasPrefix(filePath, workspacePath+"/") || filePath == workspacePath {
		bo.GetLogger().Warn(fmt.Sprintf("⚠️ resolveWorkspacePath: path '%s' already contains workspace path '%s' - using as-is to prevent double-prepend.", filePath, workspacePath))
		return filePath
	}
	return filepath.Join(workspacePath, filePath)
}

// ReadWorkspaceFile reads a file from the workspace and returns its content.
func (bo *BaseOrchestrator) ReadWorkspaceFile(ctx context.Context, filePath string) (string, error) {
	filePath = bo.resolveWorkspacePath(filePath)

	result, err := bo.WorkspaceClient.ReadWorkspaceFile(ctx, workspace.ReadWorkspaceFileParams{
		Filepath: filePath,
	})
	if err != nil {
		bo.GetLogger().Warn(fmt.Sprintf("ReadWorkspaceFile(%s) failed: %v", filePath, err))
		return "", fmt.Errorf("failed to read file %s: %w", filePath, err)
	}

	if result.Content == "" {
		bo.GetLogger().Warn(fmt.Sprintf("ReadWorkspaceFile(%s) failed: no content found", filePath))
		return "", fmt.Errorf("no content found in workspace response")
	}

	return result.Content, nil
}

// CheckWorkspaceFileExists checks if a file or directory exists in the workspace
func (bo *BaseOrchestrator) CheckWorkspaceFileExists(ctx context.Context, filePath string) (bool, error) {
	_, err := bo.ReadWorkspaceFile(ctx, filePath)
	if err != nil {
		errStr := err.Error()
		if strings.Contains(errStr, "not found") || strings.Contains(errStr, "no such file") {
			return false, nil
		}
		// ReadWorkspaceFile fails on directories — fall back to ListWorkspaceFiles
		_, listErr := bo.ListWorkspaceFiles(ctx, filePath)
		if listErr != nil {
			if strings.Contains(listErr.Error(), "does not exist") {
				return false, nil
			}
			return false, err
		}
		return true, nil
	}
	return true, nil
}

// WriteWorkspaceFile writes content to a file in the workspace.
func (bo *BaseOrchestrator) WriteWorkspaceFile(ctx context.Context, filePath string, content string) error {
	filePath = bo.resolveWorkspacePath(filePath)
	startTime := time.Now()
	contentSize := len(content)

	_, err := bo.WorkspaceClient.UpdateWorkspaceFile(ctx, workspace.UpdateWorkspaceFileParams{
		Filepath: filePath,
		Content:  content,
	})
	if err != nil {
		duration := time.Since(startTime)
		bo.GetLogger().Warn(fmt.Sprintf("⏱️ [WORKSPACE] WriteWorkspaceFile(%s, %d bytes) failed: %v (took %v)", filePath, contentSize, err, duration))
		return fmt.Errorf("failed to write file %s: %w", filePath, err)
	}

	duration := time.Since(startTime)
	bo.GetLogger().Debug(fmt.Sprintf("⏱️ [WORKSPACE] WriteWorkspaceFile(%s, %d bytes) completed successfully (took %v)", filePath, contentSize, duration))
	return nil
}

// DeleteWorkspaceFile deletes a file or directory from the workspace.
func (bo *BaseOrchestrator) DeleteWorkspaceFile(ctx context.Context, filePath string) error {
	filePath = bo.resolveWorkspacePath(filePath)
	startTime := time.Now()

	_, err := bo.WorkspaceClient.DeleteWorkspaceFile(ctx, workspace.DeleteWorkspaceFileParams{
		Filepath: filePath,
	})
	if err != nil {
		duration := time.Since(startTime)
		bo.GetLogger().Warn(fmt.Sprintf("⏱️ [WORKSPACE] DeleteWorkspaceFile(%s) failed: %v (took %v)", filePath, err, duration))
		return fmt.Errorf("failed to delete file %s: %w", filePath, err)
	}

	duration := time.Since(startTime)
	bo.GetLogger().Debug(fmt.Sprintf("⏱️ [WORKSPACE] DeleteWorkspaceFile(%s) completed successfully (took %v)", filePath, duration))
	return nil
}

// MoveWorkspaceFile moves a file or directory from one location to another.
func (bo *BaseOrchestrator) MoveWorkspaceFile(ctx context.Context, sourcePath string, destinationPath string) error {
	startTime := time.Now()

	_, err := bo.WorkspaceClient.MoveWorkspaceFile(ctx, workspace.MoveWorkspaceFileParams{
		SourceFilepath:      sourcePath,
		DestinationFilepath: destinationPath,
	})
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

// ListWorkspaceDirectories lists all directories (folders) in a given path.
// Returns a slice of directory names (not full paths).
func (bo *BaseOrchestrator) ListWorkspaceDirectories(ctx context.Context, dirPath string) ([]string, error) {
	dirPath = bo.resolveWorkspacePath(dirPath)
	startTime := time.Now()

	bo.GetLogger().Info(fmt.Sprintf("📁 Listing directories in: %s", dirPath))

	maxDepth := 1
	result, err := bo.WorkspaceClient.ListWorkspaceFiles(ctx, workspace.ListWorkspaceFilesParams{
		Folder:   dirPath,
		MaxDepth: &maxDepth,
	})
	if err != nil {
		duration := time.Since(startTime)
		bo.GetLogger().Warn(fmt.Sprintf("⏱️ [WORKSPACE] ListWorkspaceDirectories(%s) failed: %v (took %v)", dirPath, err, duration))
		return []string{}, nil
	}

	// Parse the raw JSON response using shared helper
	filesList, parseErr := virtualtools.ParseWorkspaceFilesList(string(result.Raw))
	if parseErr != nil {
		duration := time.Since(startTime)
		bo.GetLogger().Info(fmt.Sprintf("⏱️ [WORKSPACE] ListWorkspaceDirectories(%s) completed: no directories found (took %v)", dirPath, duration))
		return []string{}, nil
	}

	// Extract only directories
	var directoryNames []string
	for _, fileInfo := range filesList {
		if fileInfo.FilePath == "" || fileInfo.Type != "folder" || fileInfo.FilePath == dirPath {
			continue
		}
		name := fileInfo.FilePath
		if strings.Contains(name, "/") {
			parts := strings.Split(name, "/")
			name = parts[len(parts)-1]
		}
		if name != "" {
			directoryNames = append(directoryNames, name)
		}
	}

	duration := time.Since(startTime)
	bo.GetLogger().Info(fmt.Sprintf("⏱️ [WORKSPACE] ListWorkspaceDirectories(%s) completed: found %d directories (took %v)", dirPath, len(directoryNames), duration))
	return directoryNames, nil
}

// ListWorkspaceFiles lists all files and directories in a given path.
// Returns a slice of file/directory names (not full paths).
func (bo *BaseOrchestrator) ListWorkspaceFiles(ctx context.Context, dirPath string) ([]string, error) {
	dirPath = bo.resolveWorkspacePath(dirPath)
	startTime := time.Now()

	bo.GetLogger().Info(fmt.Sprintf("📁 Listing files and directories in: %s", dirPath))

	maxDepth := 1
	result, err := bo.WorkspaceClient.ListWorkspaceFiles(ctx, workspace.ListWorkspaceFilesParams{
		Folder:   dirPath,
		MaxDepth: &maxDepth,
	})
	if err != nil {
		duration := time.Since(startTime)
		errStr := err.Error()
		if strings.Contains(errStr, "does not exist") ||
			strings.Contains(errStr, "not found") ||
			strings.Contains(errStr, "Folder does not exist") ||
			strings.Contains(errStr, "Folder not found") {
			bo.GetLogger().Warn(fmt.Sprintf("⏱️ [WORKSPACE] ListWorkspaceFiles(%s) failed: folder does not exist (took %v)", dirPath, duration))
			return nil, fmt.Errorf("folder does not exist: %s", dirPath)
		}
		bo.GetLogger().Warn(fmt.Sprintf("⏱️ [WORKSPACE] ListWorkspaceFiles(%s) failed: %v (took %v)", dirPath, err, duration))
		return []string{}, nil
	}

	rawStr := string(result.Raw)

	// Check for "does not exist" in the raw response
	if (strings.Contains(rawStr, "Folder does not exist") ||
		strings.Contains(rawStr, "does not exist")) &&
		!strings.Contains(rawStr, "exists but contains no files") {
		duration := time.Since(startTime)
		bo.GetLogger().Warn(fmt.Sprintf("⏱️ [WORKSPACE] ListWorkspaceFiles(%s) failed: folder does not exist (took %v)", dirPath, duration))
		return nil, fmt.Errorf("folder does not exist: %s", dirPath)
	}

	// Handle empty folder case
	if strings.Contains(rawStr, "exists but contains no files") {
		duration := time.Since(startTime)
		bo.GetLogger().Info(fmt.Sprintf("⏱️ [WORKSPACE] ListWorkspaceFiles(%s) completed: folder exists but is empty (took %v)", dirPath, duration))
		return []string{}, nil
	}

	// Parse the raw JSON response using shared helper
	filesList, parseErr := virtualtools.ParseWorkspaceFilesList(rawStr)
	if parseErr != nil {
		duration := time.Since(startTime)
		bo.GetLogger().Info(fmt.Sprintf("⏱️ [WORKSPACE] ListWorkspaceFiles(%s) completed: no files found (took %v)", dirPath, duration))
		return []string{}, nil
	}

	// Unwrap nested workspace API response
	filesList = unwrapWorkspaceFilesList(filesList, dirPath)

	// Extract file and directory names (last part of path)
	var names []string
	for _, fileInfo := range filesList {
		if fileInfo.FilePath == "" || fileInfo.FilePath == dirPath {
			continue
		}
		name := fileInfo.FilePath
		if strings.Contains(name, "/") {
			parts := strings.Split(name, "/")
			name = parts[len(parts)-1]
		}
		if name != "" {
			names = append(names, name)
		}
	}

	duration := time.Since(startTime)
	bo.GetLogger().Info(fmt.Sprintf("⏱️ [WORKSPACE] ListWorkspaceFiles(%s) completed: found %d files/directories (took %v)", dirPath, len(names), duration))
	return names, nil
}

// unwrapWorkspaceFilesList handles the nested workspace API response format.
func unwrapWorkspaceFilesList(filesList []virtualtools.WorkspaceFile, dirPath string) []virtualtools.WorkspaceFile {
	if len(filesList) == 1 && filesList[0].Type == "folder" && filesList[0].FilePath == dirPath && len(filesList[0].Children) > 0 {
		return filesList[0].Children
	}

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
