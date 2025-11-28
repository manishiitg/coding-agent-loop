package utils

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"workspace/models"
)

// GitVersionManager handles git version operations
type GitVersionManager struct {
	docsDir string
}

// NewGitVersionManager creates a new git version manager
func NewGitVersionManager(docsDir string) *GitVersionManager {
	return &GitVersionManager{
		docsDir: docsDir,
	}
}

// GetFileVersionHistory gets the version history for a specific file
func (gvm *GitVersionManager) GetFileVersionHistory(filePath string, limit int) ([]models.FileVersion, error) {
	// Check if git repository exists
	if _, err := os.Stat(filepath.Join(gvm.docsDir, ".git")); os.IsNotExist(err) {
		return []models.FileVersion{}, nil // No git repo, no versions
	}

	// Get relative path from docs directory
	relPath, err := filepath.Rel(gvm.docsDir, filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to get relative path: %v", err)
	}

	// Get commit history for the file
	cmd := exec.Command("git", "-C", gvm.docsDir, "log",
		"--oneline",
		"--follow",
		fmt.Sprintf("--max-count=%d", limit),
		"--pretty=format:%H|%s|%an|%ad",
		"--date=iso",
		"--", relPath)

	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to get git log: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	versions := make([]models.FileVersion, 0, len(lines))

	for _, line := range lines {
		if line == "" {
			continue
		}

		parts := strings.Split(line, "|")
		if len(parts) < 4 {
			continue
		}

		commitHash := parts[0]
		commitMessage := parts[1]
		author := parts[2]
		dateStr := parts[3]

		// Parse date
		date, err := time.Parse("2006-01-02 15:04:05 -0700", dateStr)
		if err != nil {
			// Try alternative format
			date, err = time.Parse("2006-01-02T15:04:05-07:00", dateStr)
			if err != nil {
				date = time.Now() // Fallback to current time
			}
		}

		// Get file content at this commit
		content, _ := gvm.getFileContentAtCommit(relPath, commitHash)

		// Get diff for this commit
		diff, _ := gvm.getFileDiffAtCommit(relPath, commitHash)

		version := models.FileVersion{
			CommitHash:    commitHash,
			CommitMessage: commitMessage,
			Author:        author,
			Date:          date,
			Content:       content,
			Diff:          diff,
		}

		versions = append(versions, version)
	}

	return versions, nil
}

// getFileContentAtCommit gets the file content at a specific commit
func (gvm *GitVersionManager) getFileContentAtCommit(relPath, commitHash string) (string, error) {
	cmd := exec.Command("git", "-C", gvm.docsDir, "show",
		fmt.Sprintf("%s:%s", commitHash, relPath))

	output, err := cmd.Output()
	if err != nil {
		return "", err
	}

	return string(output), nil
}

// getFileDiffAtCommit gets the diff for a file at a specific commit
func (gvm *GitVersionManager) getFileDiffAtCommit(relPath, commitHash string) (string, error) {
	// Get diff between this commit and its parent
	cmd := exec.Command("git", "-C", gvm.docsDir, "show",
		"--format=",
		commitHash,
		"--", relPath)

	output, err := cmd.Output()
	if err != nil {
		return "", err
	}

	return string(output), nil
}

// RestoreFileVersion restores a file to a specific version
func (gvm *GitVersionManager) RestoreFileVersion(filePath, commitHash, commitMessage string) error {
	// Check if git repository exists
	if _, err := os.Stat(filepath.Join(gvm.docsDir, ".git")); os.IsNotExist(err) {
		return fmt.Errorf("git repository not found")
	}

	// Get relative path from docs directory
	relPath, err := filepath.Rel(gvm.docsDir, filePath)
	if err != nil {
		return fmt.Errorf("failed to get relative path: %v", err)
	}

	// Get file content at the specified commit
	cmd := exec.Command("git", "-C", gvm.docsDir, "show",
		fmt.Sprintf("%s:%s", commitHash, relPath))

	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("failed to get file content at commit: %v", err)
	}

	// Write the content to the file
	if err := os.WriteFile(filePath, output, 0644); err != nil {
		return fmt.Errorf("failed to write file: %v", err)
	}

	// If commit message provided, commit the restoration
	if commitMessage != "" {
		// Add the file
		addCmd := exec.Command("git", "-C", gvm.docsDir, "add", relPath)
		if err := addCmd.Run(); err != nil {
			return fmt.Errorf("failed to add file to git: %v", err)
		}

		// Commit the restoration
		commitCmd := exec.Command("git", "-C", gvm.docsDir, "commit", "-m", commitMessage)
		if err := commitCmd.Run(); err != nil {
			return fmt.Errorf("failed to commit restoration: %v", err)
		}

		// Push changes
		pushCmd := exec.Command("git", "-C", gvm.docsDir, "push", "origin", "main")
		if err := pushCmd.Run(); err != nil {
			// Try without branch specification
			pushCmd = exec.Command("git", "-C", gvm.docsDir, "push")
			if err := pushCmd.Run(); err != nil {
				// Log but don't fail - push is optional
				fmt.Printf("Warning: Failed to push restoration: %v\n", err)
			}
		}
	}

	return nil
}

// GetFileDiff gets the diff between two commits for a file
func (gvm *GitVersionManager) GetFileDiff(filePath, fromCommit, toCommit string) (string, error) {
	// Check if git repository exists
	if _, err := os.Stat(filepath.Join(gvm.docsDir, ".git")); os.IsNotExist(err) {
		return "", fmt.Errorf("git repository not found")
	}

	// Get relative path from docs directory
	relPath, err := filepath.Rel(gvm.docsDir, filePath)
	if err != nil {
		return "", fmt.Errorf("failed to get relative path: %v", err)
	}

	// Get diff between commits
	cmd := exec.Command("git", "-C", gvm.docsDir, "diff",
		fromCommit, toCommit, "--", relPath)

	output, err := cmd.Output()
	if err != nil {
		return "", err
	}

	return string(output), nil
}
