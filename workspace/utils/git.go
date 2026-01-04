package utils

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// GitStatus represents the status of a git repository
type GitStatus struct {
	HasChanges     bool
	HasConflicts   bool
	Conflicts      []string
	StagedFiles    []string
	UnstagedFiles  []string
	UntrackedFiles []string
}

// isAuthenticationError checks if a git error output indicates an authentication failure
func isAuthenticationError(output string) bool {
	outputLower := strings.ToLower(output)
	return strings.Contains(outputLower, "authentication failed") ||
		strings.Contains(outputLower, "permission denied") ||
		strings.Contains(outputLower, "invalid username or token") ||
		strings.Contains(outputLower, "could not read password") ||
		strings.Contains(outputLower, "could not read username") ||
		strings.Contains(outputLower, "bad credentials") ||
		strings.Contains(outputLower, "unauthorized") ||
		strings.Contains(outputLower, "terminal prompts disabled")
}

// PullFromGitHub pulls the latest changes from GitHub
func PullFromGitHub(docsDir, githubBranch string) error {
	log.Printf("[GIT] PullFromGitHub: Fetching from origin...")
	// Fetch latest changes with GIT_TERMINAL_PROMPT=0 to disable password prompts
	fetchCmd := exec.Command("git", "-C", docsDir, "fetch", "origin")
	fetchCmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	fetchOutput, fetchErr := fetchCmd.CombinedOutput()
	if fetchErr != nil {
		log.Printf("[GIT] ERROR: Failed to fetch from origin: %v", fetchErr)
		log.Printf("[GIT] Fetch error output: %s", string(fetchOutput))

		// Check if it's an authentication error
		if isAuthenticationError(string(fetchOutput)) {
			rawError := strings.TrimSpace(string(fetchOutput))
			return fmt.Errorf("authentication failed: invalid or expired GitHub token\nRaw error: %s", rawError)
		}

		rawError := strings.TrimSpace(string(fetchOutput))
		return fmt.Errorf("failed to fetch from origin: %v\nRaw error: %s", fetchErr, rawError)
	}

	log.Printf("[GIT] PullFromGitHub: Checking out branch %s...", githubBranch)
	// Checkout the correct branch
	if err := exec.Command("git", "-C", docsDir, "checkout", githubBranch).Run(); err != nil {
		log.Printf("[GIT] ERROR: Failed to checkout branch: %v", err)
		return fmt.Errorf("failed to checkout branch %s: %v", githubBranch, err)
	}

	log.Printf("[GIT] PullFromGitHub: Pulling from origin/%s...", githubBranch)
	// Pull latest changes with merge strategy (handles divergent branches)
	// Use --no-edit and set GIT_MERGE_AUTOEDIT=no to prevent editor prompts
	// Set GIT_EDITOR=true to use a no-op editor if git still tries to open one
	// Use --allow-unrelated-histories to handle cases where local and remote have different histories
	pullCmd := exec.Command("git", "-C", docsDir, "pull", "--no-edit", "--allow-unrelated-histories", "origin", githubBranch)
	pullCmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_MERGE_AUTOEDIT=no", "GIT_EDITOR=true")
	pullOutput, pullErr := pullCmd.CombinedOutput()
	if pullErr != nil {
		log.Printf("[GIT] ERROR: Failed to pull from origin: %v", pullErr)
		log.Printf("[GIT] Pull error output: %s", string(pullOutput))

		// Check if it's an authentication error
		if isAuthenticationError(string(pullOutput)) {
			rawError := strings.TrimSpace(string(pullOutput))
			return fmt.Errorf("authentication failed: invalid or expired GitHub token\nRaw error: %s", rawError)
		}

		// Check if it's a merge conflict
		outputStr := string(pullOutput)
		if strings.Contains(outputStr, "CONFLICT") || strings.Contains(outputStr, "conflict") || strings.Contains(outputStr, "Automatic merge failed") {
			return fmt.Errorf("merge conflict: local and remote branches have conflicting changes")
		}

		// Check if it's the unrelated histories error
		if strings.Contains(outputStr, "refusing to merge unrelated histories") {
			log.Printf("[GIT] WARNING: Unrelated histories detected, retrying with --allow-unrelated-histories")
			// Retry the pull with --allow-unrelated-histories
			retryCmd := exec.Command("git", "-C", docsDir, "pull", "--no-edit", "--allow-unrelated-histories", "origin", githubBranch)
			retryCmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_MERGE_AUTOEDIT=no", "GIT_EDITOR=true")
			retryOutput, retryErr := retryCmd.CombinedOutput()
			if retryErr != nil {
				log.Printf("[GIT] ERROR: Retry pull with --allow-unrelated-histories also failed: %v", retryErr)
				log.Printf("[GIT] Retry pull error output: %s", string(retryOutput))
				return fmt.Errorf("failed to pull from origin after retry: %v", retryErr)
			}
			log.Printf("[GIT] PullFromGitHub: Successfully pulled from origin after retry with --allow-unrelated-histories")
			return nil
		}

		// Check if it's the divergent branches hint (shouldn't happen with pull.rebase=false, but just in case)
		if strings.Contains(outputStr, "divergent branches") || strings.Contains(outputStr, "need to specify how to reconcile") {
			// Try to configure and retry
			log.Printf("[GIT] WARNING: Divergent branches detected, ensuring pull.rebase=false is set")
			exec.Command("git", "-C", docsDir, "config", "pull.rebase", "false").Run()
			// Retry the pull
			retryCmd := exec.Command("git", "-C", docsDir, "pull", "--no-edit", "--allow-unrelated-histories", "origin", githubBranch)
			retryCmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_MERGE_AUTOEDIT=no", "GIT_EDITOR=true")
			retryOutput, retryErr := retryCmd.CombinedOutput()
			if retryErr != nil {
				log.Printf("[GIT] ERROR: Retry pull also failed: %v", retryErr)
				log.Printf("[GIT] Retry pull error output: %s", string(retryOutput))
				return fmt.Errorf("failed to pull from origin after retry: %v", retryErr)
			}
			log.Printf("[GIT] PullFromGitHub: Successfully pulled from origin after retry")
			return nil
		}

		// If pull failed, try manual merge as fallback
		log.Printf("[GIT] PullFromGitHub: Pull failed, attempting manual merge...")

		// Check if we're behind the remote
		behindCmd := exec.Command("git", "-C", docsDir, "rev-list", "--left-right", "--count", fmt.Sprintf("HEAD...origin/%s", githubBranch))
		behindOutput, behindErr := behindCmd.Output()
		if behindErr == nil {
			behindParts := strings.Fields(string(behindOutput))
			if len(behindParts) == 2 {
				behindCount := strings.TrimSpace(behindParts[1])
				if behindCount != "0" {
					log.Printf("[GIT] PullFromGitHub: Local branch is %s commits behind remote, attempting merge...", behindCount)
					// Try to merge the remote branch with --allow-unrelated-histories
					mergeCmd := exec.Command("git", "-C", docsDir, "merge", "--allow-unrelated-histories", fmt.Sprintf("origin/%s", githubBranch), "-m", "Merge remote changes")
					mergeCmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_MERGE_AUTOEDIT=no", "GIT_EDITOR=true")
					mergeOutput, mergeErr := mergeCmd.CombinedOutput()
					if mergeErr != nil {
						log.Printf("[GIT] ERROR: Manual merge also failed: %v", mergeErr)
						log.Printf("[GIT] Merge error output: %s", string(mergeOutput))

						// Check for conflicts
						if strings.Contains(string(mergeOutput), "CONFLICT") || strings.Contains(string(mergeOutput), "conflict") {
							rawError := strings.TrimSpace(string(mergeOutput))
							return fmt.Errorf("merge conflict: local and remote branches have conflicting changes\nRaw error: %s", rawError)
						}
						rawError := strings.TrimSpace(string(mergeOutput))
						return fmt.Errorf("failed to merge remote changes: %v\nRaw error: %s", mergeErr, rawError)
					}
					log.Printf("[GIT] PullFromGitHub: Successfully merged remote changes")
					return nil
				}
			}
		}

		return fmt.Errorf("failed to pull from origin: %v", pullErr)
	}

	log.Printf("[GIT] PullFromGitHub: Successfully pulled from origin")
	return nil
}

// PushToGitHub pushes local changes to GitHub
func PushToGitHub(docsDir, githubBranch string) error {
	log.Printf("[GIT] PushToGitHub: Starting push operation for branch: %s", githubBranch)

	// Check if remote tracking branch exists locally first (more reliable)
	remoteRef := fmt.Sprintf("origin/%s", githubBranch)
	checkLocalCmd := exec.Command("git", "-C", docsDir, "show-ref", "--verify", "--quiet", remoteRef)
	remoteBranchExists := checkLocalCmd.Run() == nil

	if !remoteBranchExists {
		// Fallback to ls-remote if local ref doesn't exist
		log.Printf("[GIT] PushToGitHub: Remote tracking branch not found locally, trying ls-remote...")
		checkRemoteCmd := exec.Command("git", "-C", docsDir, "ls-remote", "--heads", "origin", githubBranch)
		remoteCheckOutput, remoteCheckErr := checkRemoteCmd.Output()
		remoteBranchExists = remoteCheckErr == nil && len(strings.TrimSpace(string(remoteCheckOutput))) > 0
	}

	log.Printf("[GIT] PushToGitHub: Remote branch exists: %v", remoteBranchExists)

	// Push changes to GitHub
	// Use --set-upstream if remote branch doesn't exist (first push)
	var pushCmd *exec.Cmd
	if remoteBranchExists {
		log.Printf("[GIT] PushToGitHub: Pushing to existing remote branch...")
		pushCmd = exec.Command("git", "-C", docsDir, "push", "origin", githubBranch)
	} else {
		log.Printf("[GIT] PushToGitHub: First push - setting upstream...")
		pushCmd = exec.Command("git", "-C", docsDir, "push", "--set-upstream", "origin", githubBranch)
	}
	pushCmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")

	pushOutput, pushErr := pushCmd.CombinedOutput()
	if pushErr != nil {
		log.Printf("[GIT] ERROR: Failed to push to GitHub: %v", pushErr)
		log.Printf("[GIT] Push error output: %s", string(pushOutput))

		// Check if it's an authentication error
		if isAuthenticationError(string(pushOutput)) {
			return fmt.Errorf("authentication failed: invalid or expired GitHub token")
		}

		return fmt.Errorf("failed to push to GitHub: %v", pushErr)
	}

	log.Printf("[GIT] PushToGitHub: Successfully pushed to GitHub")
	return nil
}

// GetGitStatus returns the current git status
func GetGitStatus(docsDir string) (*GitStatus, error) {
	log.Printf("[GIT] Getting git status for directory: %s", docsDir)
	statusCmd := exec.Command("git", "-C", docsDir, "status", "--porcelain")
	output, err := statusCmd.Output()
	if err != nil {
		log.Printf("[GIT] ERROR: Failed to execute git status: %v", err)
		return nil, fmt.Errorf("failed to check git status: %v", err)
	}

	outputStr := strings.TrimSpace(string(output))
	log.Printf("[GIT] Git status --porcelain output: '%s'", outputStr)

	status := &GitStatus{
		HasChanges:     false,
		HasConflicts:   false,
		Conflicts:      []string{},
		StagedFiles:    []string{},
		UnstagedFiles:  []string{},
		UntrackedFiles: []string{},
	}

	lines := strings.Split(outputStr, "\n")
	if len(lines) == 0 || (len(lines) == 1 && lines[0] == "") {
		log.Printf("[GIT] No changes detected (empty git status output)")
		return status, nil
	}

	log.Printf("[GIT] Found %d lines in git status output", len(lines))
	status.HasChanges = true

	for _, line := range lines {
		if line == "" {
			continue
		}

		parts := strings.Fields(line)
		if len(parts) < 2 {
			log.Printf("[GIT] WARNING: Skipping malformed git status line: '%s'", line)
			continue
		}

		statusCode := parts[0]
		filename := strings.Join(parts[1:], " ")

		log.Printf("[GIT] Processing file: %s (status code: '%s')", filename, statusCode)

		// Check for merge conflicts
		if strings.Contains(statusCode, "U") || strings.Contains(statusCode, "A") {
			status.HasConflicts = true
			status.Conflicts = append(status.Conflicts, filename)
			log.Printf("[GIT] Conflict detected in file: %s", filename)
		}

		// Categorize files
		if statusCode[0] != ' ' && statusCode[0] != '?' {
			status.StagedFiles = append(status.StagedFiles, filename)
			log.Printf("[GIT] Staged file: %s", filename)
		}
		if len(statusCode) > 1 && statusCode[1] != ' ' {
			status.UnstagedFiles = append(status.UnstagedFiles, filename)
			log.Printf("[GIT] Unstaged file: %s", filename)
		}
		if statusCode[0] == '?' {
			status.UntrackedFiles = append(status.UntrackedFiles, filename)
			log.Printf("[GIT] Untracked file: %s", filename)
		}
	}

	log.Printf("[GIT] Git status summary: HasChanges=%v, HasConflicts=%v, Staged=%d, Unstaged=%d, Untracked=%d",
		status.HasChanges, status.HasConflicts, len(status.StagedFiles), len(status.UnstagedFiles), len(status.UntrackedFiles))

	return status, nil
}

// CheckForConflicts checks if there are merge conflicts and returns an error if found
func CheckForConflicts(docsDir string) error {
	status, err := GetGitStatus(docsDir)
	if err != nil {
		return fmt.Errorf("failed to get git status: %v", err)
	}

	if status.HasConflicts {
		return fmt.Errorf("merge conflicts detected in files: %v. Please resolve conflicts manually before syncing", status.Conflicts)
	}

	return nil
}

// removeStaleLockFile removes a stale git index lock file if it exists
func removeStaleLockFile(docsDir string) error {
	lockFile := filepath.Join(docsDir, ".git", "index.lock")
	if _, err := os.Stat(lockFile); err == nil {
		log.Printf("[GIT] WARNING: Found stale git index lock file, removing it...")
		if err := os.Remove(lockFile); err != nil {
			return fmt.Errorf("failed to remove stale lock file: %v", err)
		}
		log.Printf("[GIT] Successfully removed stale lock file")
	}
	return nil
}

// gitAddWithRetry performs git add with automatic retry on lock file errors
func gitAddWithRetry(docsDir string) error {
	// Remove any stale lock files before attempting add
	if err := removeStaleLockFile(docsDir); err != nil {
		return err
	}

	log.Printf("[GIT] Adding all files to staging...")
	addCmd := exec.Command("git", "-C", docsDir, "add", ".")
	addOutput, addErr := addCmd.CombinedOutput()
	if addErr != nil {
		errorMsg := strings.TrimSpace(string(addOutput))
		log.Printf("[GIT] ERROR: Failed to add files: %v", addErr)
		log.Printf("[GIT] Git add error output: %s", errorMsg)

		// Check if it's a lock file error and retry once
		if strings.Contains(errorMsg, "index.lock") || strings.Contains(errorMsg, "File exists") {
			log.Printf("[GIT] Detected lock file error, attempting to remove and retry...")
			if err := removeStaleLockFile(docsDir); err != nil {
				return fmt.Errorf("failed to remove lock file: %v", err)
			}

			// Retry the add operation
			log.Printf("[GIT] Retrying git add after removing lock file...")
			retryCmd := exec.Command("git", "-C", docsDir, "add", ".")
			retryOutput, retryErr := retryCmd.CombinedOutput()
			if retryErr != nil {
				retryErrorMsg := strings.TrimSpace(string(retryOutput))
				if retryErrorMsg == "" {
					retryErrorMsg = retryErr.Error()
				}
				return fmt.Errorf("failed to add files (after retry): %s", retryErrorMsg)
			}
			log.Printf("[GIT] Successfully added files to staging after retry")
			return nil
		}

		if errorMsg == "" {
			errorMsg = addErr.Error()
		}
		return fmt.Errorf("failed to add files: %s", errorMsg)
	}
	log.Printf("[GIT] Successfully added files to staging")
	return nil
}

// SyncWithGitHub performs a complete sync: commit → pull → push
func SyncWithGitHub(docsDir, githubBranch string, commitMessage string) error {
	log.Printf("[GIT] Starting SyncWithGitHub for branch: %s", githubBranch)

	// First, add and commit any local changes
	if err := gitAddWithRetry(docsDir); err != nil {
		return err
	}

	// Check if there are changes to commit
	statusCmd := exec.Command("git", "-C", docsDir, "status", "--porcelain")
	output, err := statusCmd.Output()
	if err != nil {
		return fmt.Errorf("failed to check git status: %v", err)
	}

	// Commit changes if any
	if len(strings.TrimSpace(string(output))) > 0 {
		if commitMessage == "" {
			commitMessage = fmt.Sprintf("Update documents - %s", getCurrentTimestamp())
		}
		log.Printf("[GIT] Committing changes with message: %s", commitMessage)
		if err := exec.Command("git", "-C", docsDir, "commit", "-m", commitMessage).Run(); err != nil {
			return fmt.Errorf("failed to commit changes: %v", err)
		}
		log.Printf("[GIT] Changes committed successfully")
	} else {
		log.Printf("[GIT] No changes to commit")
	}

	// Check if remote branch exists before attempting to pull
	// First try to fetch to update remote refs
	log.Printf("[GIT] Fetching from origin to update remote refs...")
	fetchCmd := exec.Command("git", "-C", docsDir, "fetch", "origin")
	fetchCmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	fetchOutput, fetchErr := fetchCmd.CombinedOutput()
	if fetchErr != nil {
		log.Printf("[GIT] WARNING: Failed to fetch from origin: %v", fetchErr)
		log.Printf("[GIT] Fetch error output: %s", string(fetchOutput))
		// Check if it's an authentication error
		if strings.Contains(string(fetchOutput), "Authentication failed") ||
			strings.Contains(string(fetchOutput), "Permission denied") ||
			strings.Contains(string(fetchOutput), "Invalid username or token") {
			log.Printf("[GIT] ERROR: Authentication or permission issue with remote")
		}
	} else {
		log.Printf("[GIT] Successfully fetched from origin")
		log.Printf("[GIT] Fetch output: %s", string(fetchOutput))
	}

	// Check if remote tracking branch exists locally (more reliable than ls-remote)
	log.Printf("[GIT] Checking if remote tracking branch 'origin/%s' exists locally...", githubBranch)
	remoteRef := fmt.Sprintf("origin/%s", githubBranch)
	checkRemoteCmd := exec.Command("git", "-C", docsDir, "show-ref", "--verify", "--quiet", remoteRef)
	remoteBranchExists := checkRemoteCmd.Run() == nil

	if !remoteBranchExists {
		// Also try ls-remote as a fallback (in case local refs are stale)
		log.Printf("[GIT] Remote tracking branch not found locally, trying ls-remote...")
		lsRemoteCmd := exec.Command("git", "-C", docsDir, "ls-remote", "--heads", "origin", githubBranch)
		lsRemoteOutput, lsRemoteErr := lsRemoteCmd.Output()
		if lsRemoteErr == nil && len(strings.TrimSpace(string(lsRemoteOutput))) > 0 {
			remoteBranchExists = true
			log.Printf("[GIT] Remote branch found via ls-remote")
		} else {
			log.Printf("[GIT] Remote branch not found via ls-remote either")
		}
	} else {
		log.Printf("[GIT] Remote tracking branch found locally")
	}

	if remoteBranchExists {
		log.Printf("[GIT] Remote branch exists, pulling latest changes...")
		// Pull latest changes from GitHub
		if err := PullFromGitHub(docsDir, githubBranch); err != nil {
			return fmt.Errorf("failed to pull from GitHub: %v", err)
		}

		// Check for conflicts after pull - fail if conflicts exist
		if err := CheckForConflicts(docsDir); err != nil {
			return err
		}
		log.Printf("[GIT] Successfully pulled from remote")
	} else {
		log.Printf("[GIT] Remote branch does not exist, skipping pull (first push scenario)")
	}

	// Push changes to GitHub
	log.Printf("[GIT] Pushing changes to GitHub...")
	if err := PushToGitHub(docsDir, githubBranch); err != nil {
		return fmt.Errorf("failed to push to GitHub: %v", err)
	}
	log.Printf("[GIT] Successfully pushed to GitHub")

	return nil
}

// ForcePushLocal overwrites GitHub with local changes (discards remote changes)
func ForcePushLocal(docsDir, githubBranch string, commitMessage string) error {
	// First, add and commit any local changes
	if err := gitAddWithRetry(docsDir); err != nil {
		return err
	}

	// Check if there are changes to commit
	statusCmd := exec.Command("git", "-C", docsDir, "status", "--porcelain")
	output, err := statusCmd.Output()
	if err != nil {
		return fmt.Errorf("failed to check git status: %v", err)
	}

	// Commit changes if any
	if len(strings.TrimSpace(string(output))) > 0 {
		if commitMessage == "" {
			commitMessage = fmt.Sprintf("Force push local changes - %s", getCurrentTimestamp())
		}
		if err := exec.Command("git", "-C", docsDir, "commit", "-m", commitMessage).Run(); err != nil {
			return fmt.Errorf("failed to commit changes: %v", err)
		}
	}

	// Force push to GitHub (overwrites remote)
	if err := exec.Command("git", "-C", docsDir, "push", "--force", "origin", githubBranch).Run(); err != nil {
		return fmt.Errorf("failed to force push to GitHub: %v", err)
	}

	return nil
}

// ForcePullRemote overwrites local with GitHub changes (discards local changes)
func ForcePullRemote(docsDir, githubBranch string) error {
	// Fetch latest changes
	if err := exec.Command("git", "-C", docsDir, "fetch", "origin").Run(); err != nil {
		return fmt.Errorf("failed to fetch from origin: %v", err)
	}

	// Reset local branch to match remote exactly
	if err := exec.Command("git", "-C", docsDir, "reset", "--hard", fmt.Sprintf("origin/%s", githubBranch)).Run(); err != nil {
		return fmt.Errorf("failed to reset to remote branch: %v", err)
	}

	// Clean any untracked files
	if err := exec.Command("git", "-C", docsDir, "clean", "-fd").Run(); err != nil {
		return fmt.Errorf("failed to clean untracked files: %v", err)
	}

	return nil
}

// getCurrentTimestamp returns current timestamp in a readable format
func getCurrentTimestamp() string {
	return time.Now().Format("2006-01-02 15:04:05")
}
