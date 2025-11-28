package utils

import (
	"fmt"
	"os/exec"
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

// PullFromGitHub pulls the latest changes from GitHub
func PullFromGitHub(docsDir, githubBranch string) error {
	// Fetch latest changes
	if err := exec.Command("git", "-C", docsDir, "fetch", "origin").Run(); err != nil {
		return fmt.Errorf("failed to fetch from origin: %v", err)
	}

	// Checkout the correct branch
	if err := exec.Command("git", "-C", docsDir, "checkout", githubBranch).Run(); err != nil {
		return fmt.Errorf("failed to checkout branch %s: %v", githubBranch, err)
	}

	// Pull latest changes
	if err := exec.Command("git", "-C", docsDir, "pull", "origin", githubBranch).Run(); err != nil {
		return fmt.Errorf("failed to pull from origin: %v", err)
	}

	return nil
}

// PushToGitHub pushes local changes to GitHub
func PushToGitHub(docsDir, githubBranch string) error {
	// Add all files
	if err := exec.Command("git", "-C", docsDir, "add", ".").Run(); err != nil {
		return fmt.Errorf("failed to add files: %v", err)
	}

	// Check if there are changes to commit
	statusCmd := exec.Command("git", "-C", docsDir, "status", "--porcelain")
	output, err := statusCmd.Output()
	if err != nil {
		return fmt.Errorf("failed to check git status: %v", err)
	}

	// Commit changes if there are any
	if len(strings.TrimSpace(string(output))) > 0 {
		commitMessage := fmt.Sprintf("Auto-sync: %s", getCurrentTimestamp())
		if err := exec.Command("git", "-C", docsDir, "commit", "-m", commitMessage).Run(); err != nil {
			return fmt.Errorf("failed to commit changes: %v", err)
		}
	}

	// Always push to GitHub (even if no new commits, there might be existing commits to push)
	if err := exec.Command("git", "-C", docsDir, "push", "origin", githubBranch).Run(); err != nil {
		return fmt.Errorf("failed to push to origin: %v", err)
	}

	return nil
}

// GetGitStatus returns the current git status
func GetGitStatus(docsDir string) (*GitStatus, error) {
	statusCmd := exec.Command("git", "-C", docsDir, "status", "--porcelain")
	output, err := statusCmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to check git status: %v", err)
	}

	status := &GitStatus{
		HasChanges:     false,
		HasConflicts:   false,
		Conflicts:      []string{},
		StagedFiles:    []string{},
		UnstagedFiles:  []string{},
		UntrackedFiles: []string{},
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(lines) == 0 || (len(lines) == 1 && lines[0] == "") {
		return status, nil
	}

	status.HasChanges = true

	for _, line := range lines {
		if line == "" {
			continue
		}

		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}

		statusCode := parts[0]
		filename := strings.Join(parts[1:], " ")

		// Check for merge conflicts
		if strings.Contains(statusCode, "U") || strings.Contains(statusCode, "A") {
			status.HasConflicts = true
			status.Conflicts = append(status.Conflicts, filename)
		}

		// Categorize files
		if statusCode[0] != ' ' && statusCode[0] != '?' {
			status.StagedFiles = append(status.StagedFiles, filename)
		}
		if len(statusCode) > 1 && statusCode[1] != ' ' {
			status.UnstagedFiles = append(status.UnstagedFiles, filename)
		}
		if statusCode[0] == '?' {
			status.UntrackedFiles = append(status.UntrackedFiles, filename)
		}
	}

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

// SyncWithGitHub performs a complete sync: commit → pull → push
func SyncWithGitHub(docsDir, githubBranch string, commitMessage string) error {
	// First, add and commit any local changes
	if err := exec.Command("git", "-C", docsDir, "add", ".").Run(); err != nil {
		return fmt.Errorf("failed to add files: %v", err)
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
		if err := exec.Command("git", "-C", docsDir, "commit", "-m", commitMessage).Run(); err != nil {
			return fmt.Errorf("failed to commit changes: %v", err)
		}
	}

	// Pull latest changes from GitHub
	if err := PullFromGitHub(docsDir, githubBranch); err != nil {
		return fmt.Errorf("failed to pull from GitHub: %v", err)
	}

	// Check for conflicts after pull - fail if conflicts exist
	if err := CheckForConflicts(docsDir); err != nil {
		return err
	}

	// Push changes to GitHub
	if err := PushToGitHub(docsDir, githubBranch); err != nil {
		return fmt.Errorf("failed to push to GitHub: %v", err)
	}

	return nil
}

// ForcePushLocal overwrites GitHub with local changes (discards remote changes)
func ForcePushLocal(docsDir, githubBranch string, commitMessage string) error {
	// First, add and commit any local changes
	if err := exec.Command("git", "-C", docsDir, "add", ".").Run(); err != nil {
		return fmt.Errorf("failed to add files: %v", err)
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
