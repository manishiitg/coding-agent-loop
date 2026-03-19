package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/manishiitg/mcp-agent-builder-go/workspace/utils"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// syncCmd represents the sync command
var syncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Sync local documents with GitHub repository",
	Long: `Sync local documents with GitHub repository.

This command will:
- Pull the latest changes from GitHub if the local directory is not empty
- Clone the repository if the local directory is empty
- Ensure the local directory is up to date with the remote repository

Examples:
  planner sync                    # Sync with default configuration
  planner sync --force           # Force sync even if there are local changes
  planner sync --pull-only       # Only pull changes, don't push local changes`,
	Run: runSync,
}

func init() {
	syncCmd.Flags().Bool("force", false, "Force sync even if there are local changes")
	syncCmd.Flags().Bool("pull-only", false, "Only pull changes from GitHub, don't push local changes")
	viper.BindPFlag("sync-force", syncCmd.Flags().Lookup("force"))
	viper.BindPFlag("sync-pull-only", syncCmd.Flags().Lookup("pull-only"))
}

func runSync(cmd *cobra.Command, args []string) {
	// Get configuration
	githubToken := viper.GetString("github-token")
	githubRepo := viper.GetString("github-repo")
	githubBranch := viper.GetString("github-branch")
	docsDir := viper.GetString("docs-dir")
	force := viper.GetBool("sync-force")
	pullOnly := viper.GetBool("sync-pull-only")

	// Validate GitHub configuration
	if githubToken == "" || githubRepo == "" {
		fmt.Printf("❌ GitHub configuration missing\n")
		fmt.Printf("   Please set GITHUB_TOKEN and GITHUB_REPO environment variables\n")
		os.Exit(1)
	}

	fmt.Printf("🔄 Starting sync with GitHub repository: %s\n", githubRepo)
	fmt.Printf("📁 Local directory: %s\n", docsDir)
	fmt.Printf("🌿 Branch: %s\n", githubBranch)

	// Create docs directory if it doesn't exist
	if err := os.MkdirAll(docsDir, 0755); err != nil {
		fmt.Printf("❌ Failed to create docs directory: %v\n", err)
		os.Exit(1)
	}

	// Check if local directory is empty
	isEmpty, err := isDirEmpty(docsDir)
	if err != nil {
		fmt.Printf("❌ Failed to check directory status: %v\n", err)
		os.Exit(1)
	}

	if isEmpty {
		fmt.Printf("📥 Local directory is empty, cloning repository...\n")
		if err := cloneRepository(docsDir, githubRepo, githubToken, githubBranch); err != nil {
			fmt.Printf("❌ Failed to clone repository: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("✅ Successfully cloned repository\n")
	} else {
		fmt.Printf("📁 Local directory has content, checking git status...\n")

		// Check if it's a git repository
		if _, err := os.Stat(filepath.Join(docsDir, ".git")); os.IsNotExist(err) {
			fmt.Printf("❌ Local directory is not a git repository\n")
			fmt.Printf("   Please remove the directory contents or initialize git manually\n")
			os.Exit(1)
		}

		// Check for local changes
		statusCmd := exec.Command("git", "-C", docsDir, "status", "--porcelain", "-uno", "--no-renames")
		output, err := statusCmd.Output()
		if err != nil {
			fmt.Printf("❌ Failed to check git status: %v\n", err)
			os.Exit(1)
		}

		hasLocalChanges := len(strings.TrimSpace(string(output))) > 0
		if hasLocalChanges && !force {
			fmt.Printf("⚠️  Local directory has uncommitted changes\n")
			fmt.Printf("   Use --force to sync anyway, or commit/stash changes first\n")
			os.Exit(1)
		}

		if hasLocalChanges && force {
			fmt.Printf("⚠️  Local changes detected, but --force specified, proceeding...\n")
		}

		// Pull latest changes
		fmt.Printf("📥 Pulling latest changes from GitHub...\n")
		if err := utils.PullFromGitHub(docsDir, githubBranch); err != nil {
			fmt.Printf("❌ Failed to pull from GitHub: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("✅ Successfully pulled latest changes\n")
	}

	// If not pull-only, also push local changes
	if !pullOnly {
		fmt.Printf("📤 Checking for local changes to push...\n")
		if err := utils.PushToGitHub(docsDir, githubBranch); err != nil {
			fmt.Printf("⚠️  Failed to push to GitHub: %v\n", err)
			// Don't exit on push failure, as this might be expected
		} else {
			fmt.Printf("✅ Successfully pushed local changes\n")
		}
	}

	fmt.Printf("🎉 Sync completed successfully!\n")
}

// cloneRepository clones the GitHub repository
func cloneRepository(docsDir, githubRepo, githubToken, githubBranch string) error {
	// Remove existing directory contents if any
	entries, err := os.ReadDir(docsDir)
	if err != nil {
		return fmt.Errorf("failed to read directory: %v", err)
	}

	for _, entry := range entries {
		if err := os.RemoveAll(filepath.Join(docsDir, entry.Name())); err != nil {
			return fmt.Errorf("failed to remove existing file: %v", err)
		}
	}

	// Clone repository
	remoteURL := fmt.Sprintf("https://%s@github.com/%s.git", githubToken, githubRepo)
	cloneCmd := exec.Command("git", "clone", "--branch", githubBranch, remoteURL, docsDir)
	if err := cloneCmd.Run(); err != nil {
		return fmt.Errorf("failed to clone repository: %v", err)
	}

	// Set up git config
	if err := exec.Command("git", "-C", docsDir, "config", "user.name", "Planner API").Run(); err != nil {
		return fmt.Errorf("failed to set git user name: %v", err)
	}

	if err := exec.Command("git", "-C", docsDir, "config", "user.email", "planner@api.local").Run(); err != nil {
		return fmt.Errorf("failed to set git user email: %v", err)
	}

	return nil
}

// getCurrentTimestamp returns current timestamp in a readable format
func getCurrentTimestamp() string {
	return time.Now().Format("2006-01-02 15:04:05")
}

// isDirEmpty checks if a directory is empty
func isDirEmpty(dir string) (bool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false, err
	}
	return len(entries) == 0, nil
}
