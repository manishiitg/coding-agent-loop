package handlers

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/manishiitg/mcp-agent-builder-go/workspace/models"
	"github.com/manishiitg/mcp-agent-builder-go/workspace/security"
	"github.com/manishiitg/mcp-agent-builder-go/workspace/utils"

	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
)

// ExecuteShellCommand handles POST /api/execute
func ExecuteShellCommand(c *gin.Context) {
	var req models.ExecuteShellRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.APIResponse[any]{
			Success: false,
			Message: "Invalid request body",
			Error:   err.Error(),
		})
		return
	}

	// Validate command is not empty
	if strings.TrimSpace(req.Command) == "" {
		c.JSON(http.StatusBadRequest, models.APIResponse[any]{
			Success: false,
			Message: "Command is required",
			Error:   "Command cannot be empty",
		})
		return
	}

	// Get docs directory
	docsDir := viper.GetString("docs-dir")

	// Log user ID from request header for debugging
	rawUserIDHeader := c.GetHeader("X-User-ID")
	resolvedUserID := getUserID(c)
	log.Printf("[USER_ID_DEBUGGING] Shell handler: X-User-ID header=%q, resolved=%q, command=%q, working_dir=%q",
		rawUserIDHeader, resolvedUserID, req.Command, req.WorkingDirectory)

	// Determine working directory
	workingDir := docsDir
	if req.WorkingDirectory != "" {
		// Sanitize and validate working directory
		sanitizedDir := utils.SanitizeInputPath(req.WorkingDirectory, docsDir)

		// When FolderGuard is enabled, do NOT resolve per-user paths for the working directory.
		// The Isolator's mount namespace handles path mapping via WritePathMappings
		// (e.g., mounts _users/default/Chats/ at logical Chats/). Resolving to the physical
		// per-user path would place the command in the read-only view of the physical path,
		// while the writable mount is at the logical path.
		if req.FolderGuard == nil || !req.FolderGuard.Enabled {
			userID := getUserID(c)
			if userID != "" && utils.IsPerUserPath(sanitizedDir) {
				sanitizedDir = filepath.Join(utils.UsersDirectory, userID, sanitizedDir)
			}
		}

		// Build full path
		fullWorkingDir := filepath.Join(docsDir, sanitizedDir)

		// Validate path is within docs-dir boundary
		if !utils.IsValidFilePath(fullWorkingDir, docsDir) {
			c.JSON(http.StatusBadRequest, models.APIResponse[any]{
				Success: false,
				Message: "Invalid working directory",
				Error:   "Working directory must be within the workspace boundary and cannot contain directory traversal",
			})
			return
		}

		// Check if directory exists (for FolderGuard mode, the logical path may exist
		// as a mount point artifact from previous isolator runs)
		if info, err := os.Stat(fullWorkingDir); os.IsNotExist(err) || !info.IsDir() {
			c.JSON(http.StatusBadRequest, models.APIResponse[any]{
				Success: false,
				Message: "Working directory does not exist",
				Error:   fmt.Sprintf("Directory does not exist: %s", req.WorkingDirectory),
			})
			return
		}

		workingDir = fullWorkingDir
	}

	// Validate and set timeout
	timeoutSeconds := 60 // Default timeout
	if req.Timeout > 0 {
		if req.Timeout > 300 {
			c.JSON(http.StatusBadRequest, models.APIResponse[any]{
				Success: false,
				Message: "Timeout too large",
				Error:   "Timeout cannot exceed 300 seconds (5 minutes)",
			})
			return
		}
		timeoutSeconds = req.Timeout
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutSeconds)*time.Second)
	defer cancel()

	// Build command with folder guard isolation
	var cmd *exec.Cmd
	var fullCommand string
	var cleanup func()

	// Check if folder guard is enabled
	if req.FolderGuard != nil && req.FolderGuard.Enabled {
		// Build per-user write path mappings for filesystem isolation.
		// Shell commands see logical paths (e.g., Plans/{planID}/),
		// but the physical location is under _users/{userID}/.
		// WritePathMappings tells the Isolator to source files from the physical per-user path
		// while mounting them at the logical path the shell command expects.
		var writePathMappings map[string]string
		userID := getUserID(c)
		if userID != "" {
			for _, wp := range req.FolderGuard.WritePaths {
				cleanWP := strings.TrimSuffix(strings.TrimPrefix(wp, "/"), "/")
				if utils.IsPerUserPath(cleanWP) {
					if writePathMappings == nil {
						writePathMappings = make(map[string]string)
					}
					writePathMappings[wp] = filepath.Join(utils.UsersDirectory, userID, wp)
				}
			}
		}

		// Use isolated execution with filesystem restrictions
		isolator := &security.Isolator{
			ReadPaths:         req.FolderGuard.ReadPaths,
			WritePaths:        req.FolderGuard.WritePaths,
			WritePathMappings: writePathMappings,
			BlockedPaths:      req.FolderGuard.BlockedPaths,
			WorkDir:           workingDir,
		}

		// Debug: log isolator configuration for troubleshooting mount namespace issues
		fmt.Printf("[SHELL ISOLATOR] WorkDir=%s ReadPaths=%v WritePaths=%v WritePathMappings=%v\n",
			workingDir, req.FolderGuard.ReadPaths, req.FolderGuard.WritePaths, writePathMappings)

		fullCommand = stripShellPrefix(req.Command)

		var err error
		// Pass the command directly - generateMountScript already wraps with "exec sh -c '...'"
		// Do NOT double-wrap with "sh -c" here, as that causes shell argument parsing issues
		cmd, cleanup, err = isolator.ExecuteIsolated(ctx, fullCommand, nil)
		if err != nil {
			c.JSON(http.StatusInternalServerError, models.APIResponse[any]{
				Success: false,
				Message: "Failed to setup isolated execution",
				Error:   err.Error(),
			})
			return
		}
		if cleanup != nil {
			defer cleanup() // Clean up script file after execution
		}

	} else {
		// Non-isolated execution
		fullCommand = stripShellPrefix(req.Command)

		cmd = exec.CommandContext(ctx, "sh", "-c", fullCommand)
		cmd.Dir = workingDir

		// CRITICAL: Always sanitize environment, even without folder guard
		cmd.Env = security.BuildSafeEnvironment()
	}

	// Capture stdout and stderr separately
	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	// Record start time
	startTime := time.Now()

	// Execute command
	err := cmd.Run()
	executionTime := time.Since(startTime)

	// Get exit code
	exitCode := 0
	if err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			exitCode = exitError.ExitCode()
		} else {
			// Handle timeout or other execution errors
			if ctx.Err() == context.DeadlineExceeded {
				// Build stderr with timeout info prepended so the LLM sees it clearly
				timeoutMsg := fmt.Sprintf("TIMEOUT: Command killed after %d seconds\n", timeoutSeconds)
				capturedStderr := stderrBuf.String()
				if capturedStderr != "" {
					timeoutMsg += capturedStderr
				}
				c.JSON(http.StatusRequestTimeout, models.APIResponse[models.ExecuteShellResponse]{
					Success: false,
					Message: "Command execution timed out",
					Error:   fmt.Sprintf("Command exceeded timeout of %d seconds", timeoutSeconds),
					Data: models.ExecuteShellResponse{
						Stdout:          stdoutBuf.String(),
						Stderr:          timeoutMsg,
						ExitCode:        -1,
						ExecutionTimeMs: int(executionTime.Milliseconds()),
						Command:         fullCommand,
					},
				})
				return
			}
			// Other execution errors (e.g., command not found)
			errorStderr := stderrBuf.String()
			if errorStderr == "" {
				errorStderr = err.Error()
			}
			c.JSON(http.StatusInternalServerError, models.APIResponse[models.ExecuteShellResponse]{
				Success: false,
				Message: "Failed to execute command",
				Error:   err.Error(),
				Data: models.ExecuteShellResponse{
					Stdout:          stdoutBuf.String(),
					Stderr:          errorStderr,
					ExitCode:        -1,
					ExecutionTimeMs: int(executionTime.Milliseconds()),
					Command:         fullCommand,
				},
			})
			return
		}
	}

	// Success response
	c.JSON(http.StatusOK, models.APIResponse[models.ExecuteShellResponse]{
		Success: true,
		Message: "Command executed successfully",
		Data: models.ExecuteShellResponse{
			Stdout:          stdoutBuf.String(),
			Stderr:          stderrBuf.String(),
			ExitCode:        exitCode,
			ExecutionTimeMs: int(executionTime.Milliseconds()),
			Command:         fullCommand,
		},
	})
}

// stripShellPrefix removes a leading "sh -c " wrapper from the command string.
// LLMs sometimes generate commands like "sh -c ls -la" which, when passed to
// exec.CommandContext(ctx, "sh", "-c", fullCommand), causes double sh -c wrapping
// and breaks argument parsing.
func stripShellPrefix(cmd string) string {
	trimmed := strings.TrimSpace(cmd)
	for _, prefix := range []string{"sh -c ", "/bin/sh -c ", "bash -c ", "/bin/bash -c "} {
		if strings.HasPrefix(trimmed, prefix) {
			return strings.TrimSpace(trimmed[len(prefix):])
		}
	}
	return cmd
}
