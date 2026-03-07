package handlers

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/manishiitg/mcp-agent-builder-go/workspace/models"
	"github.com/manishiitg/mcp-agent-builder-go/workspace/utils"

	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
)

// maskToken returns a masked version of a token showing only first 8 and last 8 characters
func maskToken(token string) string {
	if len(token) <= 16 {
		return "****"
	}
	return token[:8] + "..." + token[len(token)-8:]
}

// isAuthenticationError checks if a git error output indicates an authentication failure
func isAuthenticationError(output string) bool {
	outputLower := strings.ToLower(output)
	return strings.Contains(outputLower, "authentication failed") ||
		strings.Contains(outputLower, "permission denied") ||
		strings.Contains(outputLower, "invalid username or token") ||
		strings.Contains(outputLower, "could not read password") ||
		strings.Contains(outputLower, "bad credentials") ||
		strings.Contains(outputLower, "unauthorized")
}

// SyncWithGitHub handles POST /api/sync/github
func SyncWithGitHub(c *gin.Context) {
	if !viper.GetBool("enable-github-sync") {
		c.JSON(http.StatusBadRequest, models.APIResponse[any]{
			Success: false,
			Message: "GitHub sync is disabled",
			Error:   "WORKSPACE_ENABLE_GITHUB_SYNC is not set to true",
		})
		return
	}

	log.Printf("[SYNC] ===== Starting GitHub sync request =====")
	var req models.SyncRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Printf("[SYNC] ERROR: Invalid request body: %v", err)
		c.JSON(http.StatusBadRequest, models.APIResponse[any]{
			Success: false,
			Message: "Invalid request body",
			Error:   err.Error(),
		})
		return
	}

	log.Printf("[SYNC] Request parameters: Force=%v, Operation='%s', CommitMessage='%s'",
		req.Force, req.Operation, req.CommitMessage)

	// Always pull first - no need to set defaults

	// Get GitHub configuration
	githubToken := viper.GetString("github-token")
	githubRepo := viper.GetString("github-repo")
	githubBranch := viper.GetString("github-branch")

	if githubToken != "" {
		log.Printf("[SYNC] GitHub configuration: Repo=%s, Branch=%s, Token=%s",
			githubRepo, githubBranch, maskToken(githubToken))
	} else {
		log.Printf("[SYNC] GitHub configuration: Repo=%s, Branch=%s, Token=not configured",
			githubRepo, githubBranch)
	}

	if githubToken == "" || githubRepo == "" {
		c.JSON(http.StatusBadRequest, models.APIResponse[any]{
			Success: false,
			Message: "GitHub configuration missing",
			Error:   "GitHub token and repository must be configured",
		})
		return
	}

	docsDir := viper.GetString("docs-dir")

	// Validate branch name
	if githubBranch == "" {
		c.JSON(http.StatusBadRequest, models.APIResponse[any]{
			Success: false,
			Message: "GitHub branch not configured",
			Error:   "github-branch configuration is required",
		})
		return
	}

	// Initialize git repository if it doesn't exist
	if err := initGitRepo(docsDir, githubRepo, githubToken); err != nil {
		c.JSON(http.StatusInternalServerError, models.APIResponse[any]{
			Success: false,
			Message: "Failed to initialize git repository",
			Error:   err.Error(),
		})
		return
	}

	// Check what branch we're currently on
	log.Printf("[SYNC] Checking current branch...")
	currentBranchCmd := exec.Command("git", "-C", docsDir, "rev-parse", "--abbrev-ref", "HEAD")
	currentBranchOutput, currentBranchErr := currentBranchCmd.Output()
	currentBranch := ""
	if currentBranchErr == nil {
		currentBranch = strings.TrimSpace(string(currentBranchOutput))
		log.Printf("[SYNC] Current branch: %s", currentBranch)
	} else {
		log.Printf("[SYNC] WARNING: Failed to get current branch: %v", currentBranchErr)
	}

	// Check initial status
	log.Printf("[SYNC] Checking git status in directory: %s", docsDir)
	status, err := utils.GetGitStatus(docsDir)
	if err != nil {
		log.Printf("[SYNC] ERROR: Failed to check git status: %v", err)
		c.JSON(http.StatusInternalServerError, models.APIResponse[any]{
			Success: false,
			Message: "Failed to check git status",
			Error:   err.Error(),
		})
		return
	}

	// Only checkout if we're not already on the target branch
	needsCheckout := currentBranch != githubBranch
	hasUncommittedChanges := status.HasChanges
	stashedChanges := false

	if needsCheckout {
		log.Printf("[SYNC] Need to switch from branch '%s' to '%s'", currentBranch, githubBranch)

		// Handle uncommitted changes before checkout (only if switching branches)
		if hasUncommittedChanges {
			log.Printf("[SYNC] Uncommitted changes detected, stashing before branch switch...")
			stashCmd := exec.Command("git", "-C", docsDir, "stash", "push", "-m", "Auto-stash before branch checkout")
			stashOutput, stashErr := stashCmd.CombinedOutput()
			if stashErr != nil {
				log.Printf("[SYNC] ERROR: Failed to stash changes: %v", stashErr)
				log.Printf("[SYNC] Stash error output: %s", string(stashOutput))
				// Check if stash failed because there's nothing to stash (empty working directory)
				if strings.Contains(strings.ToLower(string(stashOutput)), "no local changes") {
					log.Printf("[SYNC] No local changes to stash, proceeding with checkout")
					stashedChanges = false
				} else {
					c.JSON(http.StatusInternalServerError, models.APIResponse[any]{
						Success: false,
						Message: "Failed to stash uncommitted changes",
						Error:   fmt.Sprintf("Cannot switch branches with uncommitted changes. Stash failed: %s", strings.TrimSpace(string(stashOutput))),
					})
					return
				}
			} else {
				log.Printf("[SYNC] Successfully stashed uncommitted changes")
				stashedChanges = true
			}
		}

		// Checkout the target branch
		log.Printf("[SYNC] Checking out branch: %s", githubBranch)
		checkoutCmd := exec.Command("git", "-C", docsDir, "checkout", "-B", githubBranch)
		checkoutOutput, checkoutErr := checkoutCmd.CombinedOutput()
		if checkoutErr != nil {
			// Restore stashed changes if checkout failed
			if stashedChanges {
				log.Printf("[SYNC] Checkout failed, attempting to restore stashed changes...")
				restoreCmd := exec.Command("git", "-C", docsDir, "stash", "pop")
				restoreCmd.Run() // Best effort to restore, log but don't fail on restore error
			}

			log.Printf("[SYNC] ERROR: Failed to checkout branch: %v", checkoutErr)
			log.Printf("[SYNC] Checkout error output: %s", string(checkoutOutput))

			errorMsg := string(checkoutOutput)
			if errorMsg == "" {
				errorMsg = checkoutErr.Error()
			}

			c.JSON(http.StatusInternalServerError, models.APIResponse[any]{
				Success: false,
				Message: "Failed to switch to branch",
				Error:   fmt.Sprintf("Failed to checkout branch '%s': %s", githubBranch, strings.TrimSpace(errorMsg)),
			})
			return
		}
		log.Printf("[SYNC] Successfully checked out branch: %s", githubBranch)
		log.Printf("[SYNC] Checkout output: %s", string(checkoutOutput))

		// Restore stashed changes if we stashed them
		if stashedChanges {
			log.Printf("[SYNC] Restoring stashed changes...")
			restoreCmd := exec.Command("git", "-C", docsDir, "stash", "pop")
			restoreOutput, restoreErr := restoreCmd.CombinedOutput()
			if restoreErr != nil {
				log.Printf("[SYNC] WARNING: Failed to restore stashed changes: %v", restoreErr)
				log.Printf("[SYNC] Restore error output: %s", string(restoreOutput))
				// Don't fail the sync operation if stash restore fails, but log it
			} else {
				log.Printf("[SYNC] Successfully restored stashed changes")
				// Re-check status after restoring stash
				status, err = utils.GetGitStatus(docsDir)
				if err != nil {
					log.Printf("[SYNC] WARNING: Failed to re-check git status after stash restore: %v", err)
				}
			}
		}
	} else {
		log.Printf("[SYNC] Already on target branch '%s', skipping checkout", githubBranch)
	}
	if err != nil {
		log.Printf("[SYNC] ERROR: Failed to check git status: %v", err)
		c.JSON(http.StatusInternalServerError, models.APIResponse[any]{
			Success: false,
			Message: "Failed to check git status",
			Error:   err.Error(),
		})
		return
	}

	log.Printf("[SYNC] Git status check results:")
	log.Printf("[SYNC]   - HasChanges: %v", status.HasChanges)
	log.Printf("[SYNC]   - HasConflicts: %v", status.HasConflicts)
	log.Printf("[SYNC]   - StagedFiles: %d files: %v", len(status.StagedFiles), status.StagedFiles)
	log.Printf("[SYNC]   - UnstagedFiles: %d files: %v", len(status.UnstagedFiles), status.UnstagedFiles)
	log.Printf("[SYNC]   - UntrackedFiles: %d files: %v", len(status.UntrackedFiles), status.UntrackedFiles)

	// Fetch from remote first to ensure we have up-to-date remote branch info
	log.Printf("[SYNC] Fetching from remote origin...")
	fetchCmd := exec.Command("git", "-C", docsDir, "fetch", "origin")
	fetchCmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	fetchOutput, fetchErr := fetchCmd.CombinedOutput()
	if fetchErr != nil {
		log.Printf("[SYNC] WARNING: Failed to fetch from origin: %v", fetchErr)
		log.Printf("[SYNC] Fetch error output: %s", string(fetchOutput))

		// Check if it's an authentication error and return early
		if isAuthenticationError(string(fetchOutput)) {
			tokenMask := "not configured"
			if githubToken != "" {
				tokenMask = maskToken(githubToken)
			}
			log.Printf("[SYNC] ERROR: Authentication failure detected (Token: %s)", tokenMask)
			c.JSON(http.StatusUnauthorized, models.APIResponse[any]{
				Success: false,
				Message: "GitHub authentication failed",
				Error:   fmt.Sprintf("Invalid or expired GitHub token (Token: %s). Please check your GITHUB_TOKEN configuration.", tokenMask),
			})
			return
		}
	} else {
		log.Printf("[SYNC] Successfully fetched from origin")
		log.Printf("[SYNC] Fetch output: %s", string(fetchOutput))
	}

	// Check for unpushed commits
	hasUnpushedCommits := false
	unpushedCount := 0
	if githubBranch != "" {
		log.Printf("[SYNC] Checking for unpushed commits on branch: %s", githubBranch)
		remoteBranch := fmt.Sprintf("origin/%s", githubBranch)

		// First check if remote branch exists locally (after fetch)
		remoteRef := fmt.Sprintf("origin/%s", githubBranch)
		checkLocalCmd := exec.Command("git", "-C", docsDir, "show-ref", "--verify", "--quiet", remoteRef)
		remoteBranchExists := checkLocalCmd.Run() == nil

		if !remoteBranchExists {
			// Fallback to ls-remote if local ref doesn't exist
			log.Printf("[SYNC] Remote tracking branch not found locally, trying ls-remote...")
			checkRemoteCmd := exec.Command("git", "-C", docsDir, "ls-remote", "--heads", "origin", githubBranch)
			remoteCheckOutput, remoteCheckErr := checkRemoteCmd.Output()
			remoteBranchExists = remoteCheckErr == nil && len(strings.TrimSpace(string(remoteCheckOutput))) > 0
		}

		log.Printf("[SYNC] Remote branch '%s' exists: %v", remoteBranch, remoteBranchExists)

		if remoteBranchExists {
			aheadCmd := exec.Command("git", "-C", docsDir, "rev-list", "--count", fmt.Sprintf("%s..HEAD", remoteBranch))
			aheadOutput, err := aheadCmd.Output()
			if err != nil {
				log.Printf("[SYNC] ERROR: Failed to check unpushed commits: %v", err)
				log.Printf("[SYNC] Command output (stderr): %s", err.Error())
			} else {
				outputStr := strings.TrimSpace(string(aheadOutput))
				log.Printf("[SYNC] Unpushed commits check output: '%s'", outputStr)
				if count, err := strconv.Atoi(outputStr); err == nil {
					unpushedCount = count
					if count > 0 {
						hasUnpushedCommits = true
						log.Printf("[SYNC] Found %d unpushed commits", count)
					} else {
						log.Printf("[SYNC] No unpushed commits (local is up to date with remote)")
					}
				} else {
					log.Printf("[SYNC] ERROR: Failed to parse unpushed commit count: %v (output: '%s')", err, outputStr)
				}
			}
		} else {
			// Remote branch doesn't exist - check if we have any commits at all
			logCmd := exec.Command("git", "-C", docsDir, "rev-list", "--count", "HEAD")
			logOutput, logErr := logCmd.Output()
			if logErr == nil {
				if totalCommits, err := strconv.Atoi(strings.TrimSpace(string(logOutput))); err == nil && totalCommits > 0 {
					log.Printf("[SYNC] Remote branch doesn't exist, but local has %d commits - these need to be pushed", totalCommits)
					hasUnpushedCommits = true
					unpushedCount = totalCommits
				} else {
					log.Printf("[SYNC] Remote branch doesn't exist and no local commits")
				}
			}
		}
	} else {
		log.Printf("[SYNC] WARNING: githubBranch is empty, skipping unpushed commits check")
	}

	log.Printf("[SYNC] Sync decision factors:")
	log.Printf("[SYNC]   - status.HasChanges: %v", status.HasChanges)
	log.Printf("[SYNC]   - hasUnpushedCommits: %v (count: %d)", hasUnpushedCommits, unpushedCount)
	log.Printf("[SYNC]   - req.Force: %v", req.Force)

	// If no local changes and no unpushed commits and not forcing, return early
	if !status.HasChanges && !hasUnpushedCommits && !req.Force {
		log.Printf("[SYNC] Returning early: No changes to sync (HasChanges=%v, hasUnpushedCommits=%v, Force=%v)",
			status.HasChanges, hasUnpushedCommits, req.Force)
		c.JSON(http.StatusOK, models.APIResponse[map[string]interface{}]{
			Success: true,
			Message: "No changes to sync",
			Data: map[string]interface{}{
				"status": "up_to_date",
			},
		})
		return
	}

	log.Printf("[SYNC] Proceeding with sync operation...")

	// Determine operation type
	operation := req.Operation
	if operation == "" {
		operation = "sync" // Default to normal sync
	}

	log.Printf("[SYNC] Executing operation: %s", operation)

	var syncErr error
	var operationMessage string

	switch operation {
	case "force_push_local":
		log.Printf("[SYNC] Starting force push local operation...")
		syncErr = utils.ForcePushLocal(docsDir, githubBranch, req.CommitMessage)
		operationMessage = "Force push local changes completed"
		if syncErr != nil {
			log.Printf("[SYNC] ERROR: Force push failed: %v", syncErr)
		} else {
			log.Printf("[SYNC] Force push completed successfully")
		}
	case "force_pull_remote":
		log.Printf("[SYNC] Starting force pull remote operation...")
		syncErr = utils.ForcePullRemote(docsDir, githubBranch)
		operationMessage = "Force pull remote changes completed"
		if syncErr != nil {
			log.Printf("[SYNC] ERROR: Force pull failed: %v", syncErr)
		} else {
			log.Printf("[SYNC] Force pull completed successfully")
		}
	default: // "sync"
		log.Printf("[SYNC] Starting normal sync operation...")
		syncErr = utils.SyncWithGitHub(docsDir, githubBranch, req.CommitMessage)
		operationMessage = "Sync completed"
		if syncErr != nil {
			log.Printf("[SYNC] ERROR: Sync failed: %v", syncErr)
		} else {
			log.Printf("[SYNC] Sync completed successfully")
		}
	}

	if syncErr != nil {
		// Check if it's an authentication error
		if isAuthenticationError(syncErr.Error()) {
			tokenMask := "not configured"
			if githubToken != "" {
				tokenMask = maskToken(githubToken)
			}
			log.Printf("[SYNC] ERROR: Authentication failure in sync operation (Token: %s)", tokenMask)
			c.JSON(http.StatusUnauthorized, models.APIResponse[any]{
				Success: false,
				Message: "GitHub authentication failed",
				Error:   fmt.Sprintf("Invalid or expired GitHub token (Token: %s). Please check your GITHUB_TOKEN configuration.", tokenMask),
			})
			return
		}

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

	log.Printf("[SYNC] Sync operation completed successfully")
	c.JSON(http.StatusOK, models.APIResponse[models.SyncResponse]{
		Success: true,
		Message: operationMessage,
		Data:    responseData,
	})
}

// GetSyncStatus handles GET /api/sync/status
func GetSyncStatus(c *gin.Context) {
	if !viper.GetBool("enable-github-sync") {
		c.JSON(http.StatusOK, models.APIResponse[models.SyncStatus]{
			Success: true,
			Message: "GitHub sync is disabled",
			Data: models.SyncStatus{
				IsConnected:    false,
				Repository:     "",
				Branch:         "",
				PendingChanges: 0,
				PendingFiles:   []string{},
				FileStatuses:   []models.FileStatus{},
				Conflicts:      []models.Conflict{},
			},
		})
		return
	}

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
		// Parse git status output to extract file names and status codes
		for _, line := range statusLines {
			if line != "" {
				// Git status format: "XY filename" where XY is the status code
				// X = staged status, Y = unstaged status
				parts := strings.Fields(line)
				if len(parts) >= 2 {
					statusCode := parts[0]
					filename := strings.Join(parts[1:], " ")

					// Skip files that exceed the size limit — they get auto-unstaged
					// during sync and will never be committed, so don't show them as pending
					absPath := filepath.Join(docsDir, filename)
					if info, err := os.Stat(absPath); err == nil && info.Size() > utils.MaxGitFileSize {
						continue
					}

					// Add to simple pending files list
					pendingFiles = append(pendingFiles, filename)
					pendingChanges++

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
		IsConnected:     isConnected,
		LastSync:        lastSync,
		PendingChanges:  totalPendingChanges,
		UnpushedCommits: unpushedCommits,
		PendingFiles:    pendingFiles,
		FileStatuses:    fileStatuses,
		Conflicts:       []models.Conflict{},
		Repository:      githubRepo,
		Branch:          githubBranch,
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

		// Configure git for non-interactive use (disable credential prompts)
		if err := exec.Command("git", "-C", docsDir, "config", "credential.helper", "").Run(); err != nil {
			return fmt.Errorf("failed to disable credential helper: %v", err)
		}
	}

	// Always update remote URL to ensure it uses the current token
	// This handles cases where the token has been updated
	remoteURL := fmt.Sprintf("https://%s@github.com/%s.git", githubToken, githubRepo)

	// Check if remote exists
	checkRemoteCmd := exec.Command("git", "-C", docsDir, "remote", "get-url", "origin")
	if err := checkRemoteCmd.Run(); err != nil {
		// Remote doesn't exist, add it
		log.Printf("[GIT] initGitRepo: Adding remote origin with token %s", maskToken(githubToken))
		if err := exec.Command("git", "-C", docsDir, "remote", "add", "origin", remoteURL).Run(); err != nil {
			return fmt.Errorf("failed to add remote origin: %v", err)
		}
	} else {
		// Remote exists, update it
		log.Printf("[GIT] initGitRepo: Updating remote origin URL with new token %s", maskToken(githubToken))
		if err := exec.Command("git", "-C", docsDir, "remote", "set-url", "origin", remoteURL).Run(); err != nil {
			return fmt.Errorf("failed to update remote origin: %v", err)
		}
	}

	// Ensure credential helper is disabled (in case repo already existed)
	if err := exec.Command("git", "-C", docsDir, "config", "credential.helper", "").Run(); err != nil {
		log.Printf("[GIT] WARNING: Failed to disable credential helper: %v", err)
	}

	// Ensure pull strategy is configured (in case repo already existed)
	if err := exec.Command("git", "-C", docsDir, "config", "pull.rebase", "false").Run(); err != nil {
		log.Printf("[GIT] WARNING: Failed to set pull.rebase: %v", err)
	}

	// Only do initial setup if this was a new repository
	if _, err := os.Stat(filepath.Join(docsDir, ".git")); os.IsNotExist(err) {
		// This shouldn't happen since we already initialized above, but just in case
		return fmt.Errorf("git repository was not initialized")
	} else {
		// Repository already existed, check if we need to set up config
		// Check if user.name is set
		checkUserCmd := exec.Command("git", "-C", docsDir, "config", "user.name")
		if err := checkUserCmd.Run(); err != nil {
			// Set up git config if not already set
			if err := exec.Command("git", "-C", docsDir, "config", "user.name", "Planner API").Run(); err != nil {
				return fmt.Errorf("failed to set git user name: %v", err)
			}

			if err := exec.Command("git", "-C", docsDir, "config", "user.email", "planner@api.local").Run(); err != nil {
				return fmt.Errorf("failed to set git user email: %v", err)
			}
		}
	}

	// Only create initial README and commit if this was a new repository
	// Check if this is a fresh repository (no commits)
	checkCommitsCmd := exec.Command("git", "-C", docsDir, "rev-list", "--count", "HEAD")
	commitCount, err := checkCommitsCmd.Output()
	if err == nil && (len(strings.TrimSpace(string(commitCount))) == 0 || strings.TrimSpace(string(commitCount)) == "0") {
		// Fresh repository, create initial README if directory is empty
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
