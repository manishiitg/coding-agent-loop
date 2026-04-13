package main

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/manishiitg/mcp-agent-builder-go/workspace/handlers"
	"github.com/manishiitg/mcp-agent-builder-go/workspace/utils"

	"github.com/gin-contrib/gzip"
	"github.com/gin-gonic/gin"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// serverCmd represents the server command
var serverCmd = &cobra.Command{
	Use:   "server",
	Short: "Start the Planner REST API server",
	Long: `Start the HTTP server for the Planner REST API.

The server provides endpoints for:
- Document management (CRUD operations)
- Markdown structure analysis
- GitHub integration
- Search and navigation`,
	Run: runServer,
}

func init() {
	serverCmd.Flags().Bool("debug", false, "Enable debug mode")
	viper.BindPFlag("debug", serverCmd.Flags().Lookup("debug"))
}

func runServer(cmd *cobra.Command, args []string) {
	// Get configuration
	port := viper.GetString("port")
	docsDir := viper.GetString("docs-dir")
	debug := viper.GetBool("debug")
	githubToken := viper.GetString("github-token")
	githubRepo := viper.GetString("github-repo")

	absDocsDir, err := filepath.Abs(docsDir)
	if err != nil {
		fmt.Printf("Failed to resolve docs directory: %v\n", err)
		os.Exit(1)
	}
	docsDir = absDocsDir
	viper.Set("docs-dir", docsDir)

	// Create docs directory if it doesn't exist
	if err := os.MkdirAll(docsDir, 0755); err != nil {
		fmt.Printf("Failed to create docs directory: %v\n", err)
		os.Exit(1)
	}

	// Create default workspace subdirectories.
	// Root-level Chats/ and Downloads/ are kept for backwards compatibility with existing workspaces.
	// New sessions write to _users/<userID>/Chats/ instead (per-user isolation).
	defaultFolders := []string{"Chats", "Downloads", "Workflow", "skills", "_users/default/Chats", "_users/default/memories", "_users/default/chat_history"}
	for _, folder := range defaultFolders {
		path := filepath.Join(docsDir, folder)
		if err := os.MkdirAll(path, 0755); err != nil {
			fmt.Printf("Warning: Failed to create folder %s: %v\n", folder, err)
		}
	}

	// Sync with GitHub on startup if credentials are configured
	if githubToken != "" && githubRepo != "" {
		fmt.Printf("🔄 Syncing with GitHub repository on startup...\n")
		if err := syncWithGitHubOnStartup(docsDir, githubToken, githubRepo); err != nil {
			// Check if error is due to merge conflicts
			if strings.Contains(err.Error(), "merge conflict") || strings.Contains(err.Error(), "conflicts detected") {
				fmt.Printf("❌ Merge conflicts detected during startup sync:\n")
				fmt.Printf("   %v\n", err)
				fmt.Printf("\n⚠️  Server cannot start with unresolved conflicts.\n")
				fmt.Printf("   Please resolve conflicts manually and restart the server.\n")
				fmt.Printf("   Conflicted files need to be resolved before the server can start.\n")
				os.Exit(1)
			}
			// Check if error is due to authentication failure
			errLower := strings.ToLower(err.Error())
			if strings.Contains(errLower, "authentication failed") ||
				strings.Contains(errLower, "invalid or expired") ||
				strings.Contains(errLower, "could not read password") ||
				strings.Contains(errLower, "could not read username") ||
				strings.Contains(errLower, "permission denied") ||
				strings.Contains(errLower, "bad credentials") ||
				strings.Contains(errLower, "unauthorized") {
				// Show last 4 characters of token for debugging
				tokenSuffix := ""
				if len(githubToken) > 4 {
					tokenSuffix = githubToken[len(githubToken)-4:]
				} else if len(githubToken) > 0 {
					tokenSuffix = strings.Repeat("*", len(githubToken))
				}

				// Extract username from repository (format: username/repo)
				username := ""
				if parts := strings.Split(githubRepo, "/"); len(parts) >= 1 {
					username = parts[0]
				}

				// Also check for explicit username in config
				githubUsername := viper.GetString("github-username")
				if githubUsername != "" {
					username = githubUsername
				}

				fmt.Printf("❌ GitHub authentication failed during startup sync:\n")
				// Print error message, preserving newlines for raw error output
				errMsg := err.Error()
				// Split by newlines to format nicely
				errLines := strings.Split(errMsg, "\n")
				for _, line := range errLines {
					if strings.HasPrefix(line, "Raw error:") {
						fmt.Printf("   %s\n", line)
					} else {
						fmt.Printf("   %s\n", line)
					}
				}
				fmt.Printf("\n⚠️  Server cannot start with invalid GitHub credentials.\n")
				fmt.Printf("   Please check your GITHUB_TOKEN and GITHUB_REPO configuration.\n")
				if tokenSuffix != "" {
					fmt.Printf("   Token (last 4 chars): ...%s\n", tokenSuffix)
				}
				if username != "" {
					fmt.Printf("   Username: %s\n", username)
				}
				fmt.Printf("   Repository: %s\n", githubRepo)
				fmt.Printf("   Ensure the token is valid and has access to the repository.\n")
				os.Exit(1)
			}
			// For other errors (network issues), log warning but continue
			fmt.Printf("⚠️  Failed to sync with GitHub on startup: %v\n", err)
			fmt.Printf("   Server will continue without sync\n")
		} else {
			fmt.Printf("✅ Successfully synced with GitHub\n")
		}
	} else {
		fmt.Printf("ℹ️  GitHub credentials not configured, skipping sync\n")
	}

	// Sync system skills on startup (installs missing required skills via npx)
	go syncSystemSkillsOnStartup(docsDir)

	// Set Gin mode
	if debug {
		gin.SetMode(gin.DebugMode)
	} else {
		gin.SetMode(gin.ReleaseMode)
	}

	// Create Gin router
	r := gin.Default()

	// Gzip compression
	r.Use(gzip.Gzip(gzip.DefaultCompression))

	// Add CORS middleware
	r.Use(func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, PATCH, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization, X-User-ID")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}

		c.Next()
	})

	// Health check endpoint
	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status":   "healthy",
			"service":  "planner-api",
			"docs_dir": docsDir,
		})
	})
	r.HEAD("/health", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	// API routes
	api := r.Group("/api")
	{
		// Search routes (separate paths to avoid conflicts)
		api.GET("/search", handlers.SearchDocuments)

		// File upload route
		api.POST("/upload", handlers.UploadFile)

		// Shell execution route
		api.POST("/execute", handlers.ExecuteShellCommand)

		// CDP connectivity check (used by frontend to verify Chrome is reachable from container)
		api.GET("/cdp-check", handlers.CheckCdpConnection)

		// Browser process management (list/cleanup stale chromium instances)
		api.GET("/browser/processes", handlers.ListBrowserProcesses)
		api.POST("/browser/cleanup", handlers.KillBrowserProcesses)

		// Google Workspace CLI routes
		api.GET("/gws-auth-status", handlers.CheckGWSAuthStatus)
		api.POST("/gws-sync-skills", handlers.SyncGWSSkills)

		// Skills CLI routes (npx skills — runs inside container)
		api.POST("/skills/cli/install", handleSkillInstall)
		api.GET("/skills/cli/search", handleSkillSearch)
		api.GET("/skills/cli/available", handleSkillCLIAvailable)

		// Version management routes (separate from wildcard routes)
		api.GET("/versions/*filepath", handlers.GetFileVersionHistory)
		api.POST("/restore/*filepath", handlers.RestoreFileVersion)

		// Folder operations
		api.POST("/folders", handlers.CreateFolder)
		api.POST("/folders/copy", handlers.CopyFolder)
		api.DELETE("/folders/*folderpath", handlers.DeleteFolder)

		// Document management routes - SPECIFIC routes BEFORE wildcard
		api.POST("/documents", handlers.CreateDocument)
		api.GET("/documents", handlers.ListDocuments)

		// Glob search route (separate path to avoid wildcard conflict)
		api.GET("/glob", handlers.GlobDocuments)

		// Document operations with filepath (catch-all route - MUST BE LAST)
		api.Any("/documents/*filepath", handlers.HandleDocumentRequest)

		// GitHub sync routes
		sync := api.Group("/sync")
		{
			sync.POST("/github", handlers.SyncWithGitHub)
			sync.GET("/status", handlers.GetSyncStatus)
		}

		// Workspace backup routes
		workspace := api.Group("/workspace")
		{
			workspace.POST("/export", handlers.ExportWorkspace)
			workspace.POST("/import", handlers.ImportWorkspace)
		}
	}

	// Start server
	// Use net.Listen to support dynamic port allocation (port 0)
	listener, err := net.Listen("tcp", ":"+port)
	if err != nil {
		fmt.Printf("Failed to listen on port %s: %v\n", port, err)
		os.Exit(1)
	}

	// Get the actual port (in case 0 was used)
	actualPort := listener.Addr().(*net.TCPAddr).Port
	fmt.Printf("Starting Planner API server on port %d\n", actualPort)

	// Print a specific marker for Electron to parse
	fmt.Printf("DynamicPort: %d\n", actualPort)

	fmt.Printf("Docs directory: %s\n", docsDir)
	fmt.Printf("Health check: http://localhost:%d/health\n", actualPort)
	fmt.Printf("API docs: http://localhost:%d/api/documents\n", actualPort)

	if err := r.RunListener(listener); err != nil {
		fmt.Printf("Failed to start server: %v\n", err)
		os.Exit(1)
	}
}

// syncWithGitHubOnStartup syncs the local directory with GitHub on server startup
// - Clones from GitHub if the directory is empty (first time setup)
// - Pulls from remote if directory has content and is a git repository
// - Exits with error if merge conflicts are detected
func syncWithGitHubOnStartup(docsDir, githubToken, githubRepo string) error {
	// Check if local directory is empty
	isEmpty, err := isDirEmpty(docsDir)
	if err != nil {
		return fmt.Errorf("failed to check directory status: %v", err)
	}

	// Check if it's effectively empty (only standard folders created by Dockerfile)
	// Dockerfile creates Downloads, Chats, Workspace, so we consider dir empty if only these exist
	if !isEmpty {
		entries, _ := os.ReadDir(docsDir)
		isEffectivelyEmpty := true
		for _, entry := range entries {
			name := entry.Name()
			// Ignore these folders and .DS_Store
			if name != "Downloads" && name != "Chats" && name != "data" && name != ".DS_Store" {
				isEffectivelyEmpty = false
				break
			}
		}
		if isEffectivelyEmpty {
			fmt.Printf("ℹ️  Directory contains only standard folders (Downloads/Chats/Plans) - treating as empty\n")
			isEmpty = true
		}
	}

	githubBranch := viper.GetString("github-branch")

	if isEmpty {
		// Clone repository if empty (first time setup)
		fmt.Printf("📥 First time setup: cloning repository from GitHub...\n")
		return cloneRepository(docsDir, githubRepo, githubToken, githubBranch)
	} else {
		// Directory has content - check if it's a git repository
		gitDir := filepath.Join(docsDir, ".git")
		if _, err := os.Stat(gitDir); os.IsNotExist(err) {
			// Not a git repository - initialize git and set up remote
			fmt.Printf("ℹ️  Local directory has content but is not a git repository — initializing git...\n")
			if err := exec.Command("git", "-C", docsDir, "init", "--initial-branch=main").Run(); err != nil {
				return fmt.Errorf("failed to initialize git repository: %v", err)
			}
			exec.Command("git", "-C", docsDir, "config", "user.name", "Planner API").Run()
			exec.Command("git", "-C", docsDir, "config", "user.email", "planner@api.local").Run()
			exec.Command("git", "-C", docsDir, "config", "credential.helper", "").Run()
			exec.Command("git", "-C", docsDir, "config", "pull.rebase", "false").Run()
			remoteURL := fmt.Sprintf("https://%s@github.com/%s.git", githubToken, githubRepo)
			exec.Command("git", "-C", docsDir, "remote", "add", "origin", remoteURL).Run()
			fmt.Printf("✅ Git initialized and remote configured\n")
			// Pull from remote to sync existing content
			fmt.Printf("📥 Pulling from GitHub...\n")
			if err := utils.PullFromGitHub(docsDir, githubBranch); err != nil {
				fmt.Printf("⚠️  Failed to pull on fresh git init: %v\n", err)
			}
			return nil
		}

		// Ensure remote URL is configured with the token before pulling
		fmt.Printf("🔧 Configuring git remote with token...\n")
		remoteURL := fmt.Sprintf("https://%s@github.com/%s.git", githubToken, githubRepo)

		// Check if remote exists and update it
		checkRemoteCmd := exec.Command("git", "-C", docsDir, "remote", "get-url", "origin")
		if err := checkRemoteCmd.Run(); err != nil {
			// Remote doesn't exist, add it
			fmt.Printf("   Adding remote origin...\n")
			if err := exec.Command("git", "-C", docsDir, "remote", "add", "origin", remoteURL).Run(); err != nil {
				return fmt.Errorf("failed to add remote origin: %v", err)
			}
		} else {
			// Remote exists, update it with current token
			fmt.Printf("   Updating remote origin URL...\n")
			if err := exec.Command("git", "-C", docsDir, "remote", "set-url", "origin", remoteURL).Run(); err != nil {
				return fmt.Errorf("failed to update remote origin: %v", err)
			}
		}

		// Configure git for non-interactive use
		exec.Command("git", "-C", docsDir, "config", "credential.helper", "").Run()
		exec.Command("git", "-C", docsDir, "config", "user.name", "Planner API").Run()
		exec.Command("git", "-C", docsDir, "config", "user.email", "planner@api.local").Run()
		exec.Command("git", "-C", docsDir, "config", "pull.rebase", "false").Run()
		fmt.Printf("✅ Git config initialized (user, credential.helper, pull.rebase)\n")

		// It's a git repository - pull latest changes
		fmt.Printf("📥 Pulling latest changes from GitHub...\n")
		if err := utils.PullFromGitHub(docsDir, githubBranch); err != nil {
			// Check if error is due to merge conflicts
			if strings.Contains(err.Error(), "merge conflict") || strings.Contains(err.Error(), "conflict") {
				// Get detailed conflict information
				status, statusErr := utils.GetGitStatus(docsDir)
				if statusErr == nil && status.HasConflicts {
					conflictFiles := strings.Join(status.Conflicts, ", ")
					return fmt.Errorf("merge conflicts detected in files: %s. Please resolve conflicts manually before starting the server", conflictFiles)
				}
				return fmt.Errorf("merge conflict: %v", err)
			}
			// For other errors (network, auth), return error but let caller decide
			return fmt.Errorf("failed to pull from GitHub: %v", err)
		}

		// Check for conflicts after pull
		fmt.Printf("🔍 Checking for merge conflicts...\n")
		if err := utils.CheckForConflicts(docsDir); err != nil {
			// Get detailed conflict information
			status, statusErr := utils.GetGitStatus(docsDir)
			if statusErr == nil && status.HasConflicts {
				conflictFiles := strings.Join(status.Conflicts, ", ")
				return fmt.Errorf("merge conflicts detected in files: %s. Please resolve conflicts manually before starting the server", conflictFiles)
			}
			return err
		}

		fmt.Printf("✅ Successfully pulled latest changes (no conflicts)\n")
		return nil
	}
}
