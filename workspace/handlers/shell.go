package handlers

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"workspace/models"
	"workspace/security"
	"workspace/utils"

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

	// Determine working directory
	workingDir := docsDir
	if req.WorkingDirectory != "" {
		// Sanitize and validate working directory
		sanitizedDir := utils.SanitizeInputPath(req.WorkingDirectory, docsDir)

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

		// Check if directory exists
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
		// Use isolated execution with filesystem restrictions
		isolator := &security.Isolator{
			ReadPaths:  req.FolderGuard.ReadPaths,
			WritePaths: req.FolderGuard.WritePaths,
			WorkDir:    workingDir,
		}

		if len(req.Args) > 0 {
			fullCommand = req.Command + " " + strings.Join(req.Args, " ")
		} else {
			fullCommand = req.Command
		}

		var err error
		cmd, cleanup, err = isolator.ExecuteIsolated(ctx, "sh", []string{"-c", fullCommand})
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
		// Non-isolated execution (backward compatibility)
		// BUT STILL sanitize environment for security!
		
		// Respect UseShell if provided, but original logic hardcoded useShell=true.
		// I will check if I should follow original logic or the snippet.
		// Snippet for non-isolated:
		// if len(req.Args) > 0 { fullCommand = ... } else { fullCommand = req.Command }
		// cmd = exec.CommandContext(ctx, "sh", "-c", fullCommand)
		// This forces shell execution, same as original.

		if len(req.Args) > 0 {
			fullCommand = req.Command + " " + strings.Join(req.Args, " ")
		} else {
			fullCommand = req.Command
		}

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
				c.JSON(http.StatusRequestTimeout, models.APIResponse[models.ExecuteShellResponse]{
					Success: false,
					Message: "Command execution timed out",
					Error:   fmt.Sprintf("Command exceeded timeout of %d seconds", timeoutSeconds),
					Data: models.ExecuteShellResponse{
						Stdout:          stdoutBuf.String(),
						Stderr:          stderrBuf.String(),
						ExitCode:        -1,
						ExecutionTimeMs: int(executionTime.Milliseconds()),
						Command:         fullCommand,
					},
				})
				return
			}
			// Other execution errors (e.g., command not found)
			c.JSON(http.StatusInternalServerError, models.APIResponse[models.ExecuteShellResponse]{
				Success: false,
				Message: "Failed to execute command",
				Error:   err.Error(),
				Data: models.ExecuteShellResponse{
					Stdout:          stdoutBuf.String(),
					Stderr:          stderrBuf.String(),
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
