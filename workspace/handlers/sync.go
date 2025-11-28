package handlers

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"workspace/models"
	"workspace/utils"

	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
)

// SyncWithGitHub handles POST /api/sync/github
func SyncWithGitHub(c *gin.Context) {
	var req models.SyncRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.APIResponse[any]{
			Success: false,
			Message: "Invalid request body",
			Error:   err.Error(),
		})
		return
	}

	// Always pull first - no need to set defaults

	// Get GitHub configuration
	githubToken := viper.GetString("github-token")
	githubRepo := viper.GetString("github-repo")
	githubBranch := viper.GetString("github-branch")

	if githubToken == "" || githubRepo == "" {
		c.JSON(http.StatusBadRequest, models.APIResponse[any]{
			Success: false,
			Message: "GitHub configuration missing",
			Error:   "GitHub token and repository must be configured",
		})
		return
	}

	docsDir := viper.GetString("docs-dir")

	// Initialize git repository if it doesn't exist
	if err := initGitRepo(docsDir, githubRepo, githubToken); err != nil {
		c.JSON(http.StatusInternalServerError, models.APIResponse[any]{
			Success: false,
			Message: "Failed to initialize git repository",
			Error:   err.Error(),
		})
		return
	}

	// Ensure we're on the correct branch
	if err := exec.Command("git", "-C", docsDir, "checkout", "-B", githubBranch).Run(); err != nil {
		c.JSON(http.StatusInternalServerError, models.APIResponse[any]{
			Success: false,
			Message: "Failed to switch to branch",
			Error:   err.Error(),
		})
		return
	}

	// Check initial status
	status, err := utils.GetGitStatus(docsDir)
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.APIResponse[any]{
			Success: false,
			Message: "Failed to check git status",
			Error:   err.Error(),
		})
		return
	}

	// Check for unpushed commits
	hasUnpushedCommits := false
	if githubBranch != "" {
		aheadCmd := exec.Command("git", "-C", docsDir, "rev-list", "--count", fmt.Sprintf("origin/%s..HEAD", githubBranch))
		aheadOutput, err := aheadCmd.Output()
		if err == nil {
			if count, err := strconv.Atoi(strings.TrimSpace(string(aheadOutput))); err == nil && count > 0 {
				hasUnpushedCommits = true
			}
		}
	}

	// If no local changes and no unpushed commits and not forcing, return early
	if !status.HasChanges && !hasUnpushedCommits && !req.Force {
		c.JSON(http.StatusOK, models.APIResponse[map[string]interface{}]{
			Success: true,
			Message: "No changes to sync",
			Data: map[string]interface{}{
				"status": "up_to_date",
			},
		})
		return
	}

	// Determine operation type
	operation := req.Operation
	if operation == "" {
		operation = "sync" // Default to normal sync
	}

	var syncErr error
	var operationMessage string

	switch operation {
	case "force_push_local":
		syncErr = utils.ForcePushLocal(docsDir, githubBranch, req.CommitMessage)
		operationMessage = "Force push local changes completed"
	case "force_pull_remote":
		syncErr = utils.ForcePullRemote(docsDir, githubBranch)
		operationMessage = "Force pull remote changes completed"
	default: // "sync"
		syncErr = utils.SyncWithGitHub(docsDir, githubBranch, req.CommitMessage)
		operationMessage = "Sync completed"
	}

	if syncErr != nil {
		// Check if it's a conflict error (only for normal sync)
		if operation == "sync" && strings.Contains(syncErr.Error(), "merge conflicts detected") {
			c.JSON(http.StatusConflict, models.APIResponse[models.SyncResponse]{
				Success: false,
				Message: "Merge conflicts detected",
				Error:   syncErr.Error(),
				Data: models.SyncResponse{
					Message: "Please resolve conflicts manually or use force operations",
					ConflictOptions: []string{
						"force_push_local - Overwrite GitHub with local changes",
						"force_pull_remote - Overwrite local with GitHub changes",
					},
				},
			})
			return
		}

		c.JSON(http.StatusInternalServerError, models.APIResponse[any]{
			Success: false,
			Message: fmt.Sprintf("Failed to %s with GitHub", operation),
			Error:   syncErr.Error(),
		})
		return
	}

	responseData := models.SyncResponse{
		Status:        "synced",
		Operation:     operation,
		CommitMessage: req.CommitMessage,
		Repository:    githubRepo,
		Branch:        githubBranch,
		Timestamp:     time.Now(),
	}

	c.JSON(http.StatusOK, models.APIResponse[models.SyncResponse]{
		Success: true,
		Message: operationMessage,
		Data:    responseData,
	})
}

// GetSyncStatus handles GET /api/sync/status
func GetSyncStatus(c *gin.Context) {
	var req models.SyncStatusRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.APIResponse[any]{
			Success: false,
			Message: "Invalid query parameters",
			Error:   err.Error(),
		})
		return
	}

	githubRepo := viper.GetString("github-repo")
	githubBranch := viper.GetString("github-branch")
	docsDir := viper.GetString("docs-dir")

	// Check if git repository exists
	if _, err := os.Stat(filepath.Join(docsDir, ".git")); os.IsNotExist(err) {
		c.JSON(http.StatusOK, models.APIResponse[models.SyncStatus]{
			Success: true,
			Message: "Git repository not initialized",
			Data: models.SyncStatus{
				IsConnected:    false,
				Repository:     githubRepo,
				Branch:         githubBranch,
				PendingChanges: 0,
				PendingFiles:   []string{},
				FileStatuses:   []models.FileStatus{},
				Conflicts:      []models.Conflict{},
			},
		})
		return
	}

	// Check git status
	statusCmd := exec.Command("git", "-C", docsDir, "status", "--porcelain")
	output, err := statusCmd.Output()
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.APIResponse[any]{
			Success: false,
			Message: "Failed to check git status",
			Error:   err.Error(),
		})
		return
	}

	statusLines := strings.Split(strings.TrimSpace(string(output)), "\n")
	pendingChanges := 0
	var pendingFiles []string
	var fileStatuses []models.FileStatus

	if len(statusLines) > 0 && statusLines[0] != "" {
		pendingChanges = len(statusLines)
		// Parse git status output to extract file names and status codes
		for _, line := range statusLines {
			if line != "" {
				// Git status format: "XY filename" where XY is the status code
				// X = staged status, Y = unstaged status
				parts := strings.Fields(line)
				if len(parts) >= 2 {
					statusCode := parts[0]
					filename := strings.Join(parts[1:], " ")

					// Add to simple pending files list
					pendingFiles = append(pendingFiles, filename)

					// Parse status code
					stagedStatus := string(statusCode[0])
					unstagedStatus := " "
					if len(statusCode) > 1 {
						unstagedStatus = string(statusCode[1])
					}

					// Determine if file is staged or unstaged
					isStaged := stagedStatus != " " && stagedStatus != "?"
					status := unstagedStatus
					if isStaged {
						status = stagedStatus
					}

					// Map Git status codes to human-readable format
					statusMap := map[string]string{
						"M": "Modified",
						"A": "Added",
						"D": "Deleted",
						"R": "Renamed",
						"C": "Copied",
						"U": "Unmerged",
						"?": "Untracked",
						"!": "Ignored",
						" ": "Unchanged",
					}

					displayStatus := statusMap[status]
					if displayStatus == "" {
						displayStatus = status
					}

					fileStatuses = append(fileStatuses, models.FileStatus{
						File:   filename,
						Status: displayStatus,
						Staged: isStaged,
					})
				}
			}
		}
	}

	// Check for unpushed commits (ahead of origin)
	unpushedCommits := 0
	if githubBranch != "" {
		// Check how many commits we're ahead of origin
		aheadCmd := exec.Command("git", "-C", docsDir, "rev-list", "--count", fmt.Sprintf("origin/%s..HEAD", githubBranch))
		aheadOutput, err := aheadCmd.Output()
		if err == nil {
			if count, err := strconv.Atoi(strings.TrimSpace(string(aheadOutput))); err == nil {
				unpushedCommits = count
			}
		}
	}

	// Total pending changes = local changes + unpushed commits
	totalPendingChanges := pendingChanges + unpushedCommits

	// Check if we're connected to remote
	remoteCmd := exec.Command("git", "-C", docsDir, "remote", "get-url", "origin")
	remoteOutput, err := remoteCmd.Output()
	isConnected := err == nil && strings.Contains(string(remoteOutput), githubRepo)

	// Get last sync time
	var lastSync time.Time
	if isConnected {
		logCmd := exec.Command("git", "-C", docsDir, "log", "-1", "--format=%cd", "--date=iso")
		logOutput, err := logCmd.Output()
		if err == nil {
			if parsedTime, err := time.Parse("2006-01-02 15:04:05 -0700", strings.TrimSpace(string(logOutput))); err == nil {
				lastSync = parsedTime
			}
		}
	}

	responseData := models.SyncStatus{
		IsConnected:    isConnected,
		LastSync:       lastSync,
		PendingChanges: totalPendingChanges,
		PendingFiles:   pendingFiles,
		FileStatuses:   fileStatuses,
		Conflicts:      []models.Conflict{},
		Repository:     githubRepo,
		Branch:         githubBranch,
	}

	c.JSON(http.StatusOK, models.APIResponse[models.SyncStatus]{
		Success: true,
		Message: "Sync status retrieved successfully",
		Data:    responseData,
	})
}

// initGitRepo initializes a git repository and sets up the remote
func initGitRepo(docsDir, githubRepo, githubToken string) error {
	// Check if .git directory exists
	if _, err := os.Stat(filepath.Join(docsDir, ".git")); os.IsNotExist(err) {
		// Initialize git repository with main branch
		if err := exec.Command("git", "-C", docsDir, "init", "--initial-branch=main").Run(); err != nil {
			return fmt.Errorf("failed to initialize git repository: %v", err)
		}

		// Set up git config
		if err := exec.Command("git", "-C", docsDir, "config", "user.name", "Planner API").Run(); err != nil {
			return fmt.Errorf("failed to set git user name: %v", err)
		}

		if err := exec.Command("git", "-C", docsDir, "config", "user.email", "planner@api.local").Run(); err != nil {
			return fmt.Errorf("failed to set git user email: %v", err)
		}

		// Add remote origin
		remoteURL := fmt.Sprintf("https://%s@github.com/%s.git", githubToken, githubRepo)
		if err := exec.Command("git", "-C", docsDir, "remote", "add", "origin", remoteURL).Run(); err != nil {
			return fmt.Errorf("failed to add remote origin: %v", err)
		}

		// Create initial README if docs directory is empty
		if isEmpty, _ := isDirEmpty(docsDir); isEmpty {
			readmeContent := `# Planner Documents

This repository contains markdown documents managed by the Planner API.

## Getting Started

Documents are automatically synced from the Planner API. You can:

- View documents directly in this repository
- Make changes via the API
- Track changes through git history

## API Endpoints

- **Health**: GET /health
- **Documents**: GET /api/documents
- **Search**: GET /api/documents/search
- **Sync**: POST /api/sync/github

Happy planning! 🚀
`
			readmePath := filepath.Join(docsDir, "README.md")
			if err := os.WriteFile(readmePath, []byte(readmeContent), 0644); err != nil {
				return fmt.Errorf("failed to create initial README: %v", err)
			}
		}

		// Add all files and make initial commit
		if err := exec.Command("git", "-C", docsDir, "add", ".").Run(); err != nil {
			return fmt.Errorf("failed to add files to git: %v", err)
		}

		// Check if there are any changes to commit
		statusCmd := exec.Command("git", "-C", docsDir, "status", "--porcelain")
		output, err := statusCmd.Output()
		if err != nil {
			return fmt.Errorf("failed to check git status: %v", err)
		}

		if len(strings.TrimSpace(string(output))) > 0 {
			// Make initial commit
			if err := exec.Command("git", "-C", docsDir, "commit", "-m", "Initial commit: Planner API setup").Run(); err != nil {
				return fmt.Errorf("failed to make initial commit: %v", err)
			}
		}
	}

	return nil
}

// isDirEmpty checks if a directory is empty
func isDirEmpty(dir string) (bool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false, err
	}
	return len(entries) == 0, nil
}
