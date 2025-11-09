package main

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	"planner/handlers"

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
	enableSemanticSearch := viper.GetBool("enable-semantic-search")

	// Create docs directory if it doesn't exist
	if err := os.MkdirAll(docsDir, 0755); err != nil {
		fmt.Printf("Failed to create docs directory: %v\n", err)
		os.Exit(1)
	}

	// Sync with GitHub on startup if credentials are configured
	if githubToken != "" && githubRepo != "" {
		fmt.Printf("🔄 Syncing with GitHub repository on startup...\n")
		if err := syncWithGitHubOnStartup(docsDir, githubToken, githubRepo); err != nil {
			fmt.Printf("⚠️  Failed to sync with GitHub on startup: %v\n", err)
			fmt.Printf("   Server will continue without sync\n")
		} else {
			fmt.Printf("✅ Successfully synced with GitHub\n")
		}
	} else {
		fmt.Printf("ℹ️  GitHub credentials not configured, skipping sync\n")
	}

	// Set Gin mode
	if debug {
		gin.SetMode(gin.DebugMode)
	} else {
		gin.SetMode(gin.ReleaseMode)
	}

	// Initialize semantic search services conditionally
	if enableSemanticSearch {
		fmt.Printf("🔧 Initializing semantic search services...\n")
		handlers.InitializeSemanticServices()
		fmt.Printf("✅ Semantic search services initialized\n")
	} else {
		fmt.Printf("ℹ️  Semantic search is disabled\n")
	}

	// Create Gin router
	r := gin.Default()

	// Add CORS middleware
	r.Use(func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, PATCH, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}

		c.Next()
	})

	// Health check endpoint
	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status":          "healthy",
			"service":         "planner-api",
			"docs_dir":        docsDir,
			"semantic_search": enableSemanticSearch,
		})
	})

	// API routes
	api := r.Group("/api")
	{
		// Document management routes
		documents := api.Group("/documents")
		{
			documents.POST("", handlers.CreateDocument)
			documents.GET("", handlers.ListDocuments)
		}

		// Search routes (separate paths to avoid conflicts)
		api.GET("/search", handlers.SearchDocuments)
		api.GET("/search/semantic", handlers.SemanticSearch)
		api.POST("/search/process-file", handlers.ProcessFile)

		// File upload route
		api.POST("/upload", handlers.UploadFile)

		// Shell execution route
		api.POST("/execute", handlers.ExecuteShellCommand)

		// Version management routes (separate from wildcard routes)
		api.GET("/versions/*filepath", handlers.GetFileVersionHistory)
		api.POST("/restore/*filepath", handlers.RestoreFileVersion)

		// Folder operations
		api.POST("/folders", handlers.CreateFolder)
		api.DELETE("/folders/*folderpath", handlers.DeleteFolder)

		// Document operations with filepath (catch-all route handles all document operations)
		api.Any("/documents/*filepath", handlers.HandleDocumentRequest)

		// GitHub sync routes
		sync := api.Group("/sync")
		{
			sync.POST("/github", handlers.SyncWithGitHub)
			sync.GET("/status", handlers.GetSyncStatus)
		}

		// Semantic search monitoring routes
		semantic := api.Group("/semantic")
		{
			semantic.GET("/jobs", handlers.GetJobStatus)
			semantic.GET("/stats", handlers.GetSemanticStats)
			semantic.POST("/resync", handlers.TriggerResync)
		}
	}

	// Start server
	fmt.Printf("Starting Planner API server on port %s\n", port)
	absDocsDir, _ := filepath.Abs(docsDir)
	fmt.Printf("Docs directory: %s\n", absDocsDir)
	fmt.Printf("Health check: http://localhost:%s/health\n", port)
	fmt.Printf("API docs: http://localhost:%s/api/documents\n", port)

	if err := r.Run(":" + port); err != nil {
		fmt.Printf("Failed to start server: %v\n", err)
		os.Exit(1)
	}
}

// syncWithGitHubOnStartup syncs the local directory with GitHub on server startup
// Only clones from GitHub if the directory is empty (first time setup)
// Skips sync if directory has content to prevent data loss
func syncWithGitHubOnStartup(docsDir, githubToken, githubRepo string) error {
	// Check if local directory is empty
	isEmpty, err := isDirEmpty(docsDir)
	if err != nil {
		return fmt.Errorf("failed to check directory status: %v", err)
	}

	githubBranch := viper.GetString("github-branch")

	if isEmpty {
		// Clone repository if empty (first time setup)
		fmt.Printf("📥 First time setup: cloning repository from GitHub...\n")
		return cloneRepository(docsDir, githubRepo, githubToken, githubBranch)
	} else {
		// Skip sync if local folder has content to prevent data loss
		fmt.Printf("ℹ️  Local directory has content, skipping GitHub sync on startup\n")
		fmt.Printf("   Use 'planner sync' command manually if you need to sync with GitHub\n")
		return nil
	}
}
