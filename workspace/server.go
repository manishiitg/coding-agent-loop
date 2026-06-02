package main

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"

	"github.com/manishiitg/mcp-agent-builder-go/workspace/handlers"

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

		// Read-only SQLite query routes (report widgets + DatabasePopup).
		// Connection opened mode=ro + query_only — writes are rejected by the engine.
		api.POST("/query", handlers.QueryWorkflowDB)
		api.GET("/db/tables", handlers.GetWorkflowDBTables)

		// CDP connectivity check (used by frontend to verify Chrome is reachable from container)
		api.GET("/cdp-check", handlers.CheckCdpConnection)

		// Browser process management (list/cleanup stale chromium instances)
		api.GET("/browser/processes", handlers.ListBrowserProcesses)
		api.POST("/browser/cleanup", handlers.KillBrowserProcesses)

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
