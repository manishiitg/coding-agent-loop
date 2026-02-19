package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/manishiitg/mcp-agent-builder-go/workspace/services"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var cfgFile string

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "planner",
	Short: "Planner REST API - Markdown Document Management",
	Long: `A REST API focused on markdown document management with advanced patching capabilities and GitHub integration.

This tool provides:
- Markdown document CRUD operations
- Structure analysis and parsing
- GitHub version control integration
- LLM agent ready endpoints`,
}

// Execute adds all child commands to the root command and sets flags appropriately.
func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

func init() {
	cobra.OnInitialize(initConfig)

	// Global flags
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.planner.yaml)")
	rootCmd.PersistentFlags().String("port", "8080", "HTTP server port")
	rootCmd.PersistentFlags().String("docs-dir", "./workspace-docs", "Documents directory")
	rootCmd.PersistentFlags().String("data-dir", "./data", "Data directory for application state")
	rootCmd.PersistentFlags().String("github-token", "", "GitHub personal access token")
	rootCmd.PersistentFlags().String("github-repo", "", "GitHub repository (username/repo-name)")
	rootCmd.PersistentFlags().Bool("enable-semantic-search", true, "Enable semantic search functionality")

	// Bind flags to viper
	viper.BindPFlag("port", rootCmd.PersistentFlags().Lookup("port"))
	viper.BindPFlag("docs-dir", rootCmd.PersistentFlags().Lookup("docs-dir"))
	viper.BindPFlag("data-dir", rootCmd.PersistentFlags().Lookup("data-dir"))
	viper.BindPFlag("github-token", rootCmd.PersistentFlags().Lookup("github-token"))
	viper.BindPFlag("github-repo", rootCmd.PersistentFlags().Lookup("github-repo"))
	viper.BindPFlag("enable-semantic-search", rootCmd.PersistentFlags().Lookup("enable-semantic-search"))

	// Set environment variable key replacer for Viper
	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))

	// Set default values
	viper.SetDefault("github-branch", "main")

	// Bind environment variables with correct prefixes
	viper.BindEnv("github-branch", "WORKSPACE_GITHUB_BRANCH")
	viper.BindEnv("github-token", "GITHUB_TOKEN")
	viper.BindEnv("github-repo", "GITHUB_REPO")
	viper.BindEnv("docs-dir", "DOCS_DIR")
	viper.BindEnv("data-dir", "DATA_DIR")
	viper.BindEnv("enable-semantic-search", "WORKSPACE_ENABLE_SEMANTIC_SEARCH")
	viper.BindEnv("enable-github-sync", "WORKSPACE_ENABLE_GITHUB_SYNC")

	// Add subcommands
	rootCmd.AddCommand(serverCmd)
	rootCmd.AddCommand(syncCmd)
	rootCmd.AddCommand(resyncCmd)

	// Initialize resync command
	initResync()
}

// resyncCmd represents the resync command
var resyncCmd = &cobra.Command{
	Use:   "resync",
	Short: "Re-sync all documents with Qdrant vector database",
	Long: `Re-sync all documents with Qdrant vector database.

This command will:
1. Delete all existing data from Qdrant
2. Queue all documents for processing
3. Generate embeddings and create vector index

Use this for:
- Initial setup
- Recovery from corruption
- Full index rebuild`,
	Run: runResync,
}

func initResync() {
	// Add flags
	resyncCmd.Flags().String("qdrant-url", "http://localhost:6333", "Qdrant server URL")
	resyncCmd.Flags().String("openai-model", "text-embedding-3-small", "OpenAI embedding model")
	resyncCmd.Flags().Bool("dry-run", false, "Show what would be done without actually doing it")
	resyncCmd.Flags().Bool("force", false, "Force re-sync even if services are not available")

	// Bind flags to viper
	viper.BindPFlag("qdrant-url", resyncCmd.Flags().Lookup("qdrant-url"))
	viper.BindPFlag("openai-model", resyncCmd.Flags().Lookup("openai-model"))
	viper.BindPFlag("dry-run", resyncCmd.Flags().Lookup("dry-run"))
	viper.BindPFlag("force", resyncCmd.Flags().Lookup("force"))
}

// initConfig reads in config file and ENV variables if set.
func initConfig() {
	if cfgFile != "" {
		// Use config file from the flag.
		viper.SetConfigFile(cfgFile)
	} else {
		// Find home directory.
		home, err := os.UserHomeDir()
		cobra.CheckErr(err)

		// Search config in home directory with name ".planner" (without extension).
		viper.AddConfigPath(home)
		viper.AddConfigPath(".")
		viper.SetConfigType("yaml")
		viper.SetConfigName(".planner")
	}

	viper.AutomaticEnv() // read in environment variables that match

	// If a config file is found, read it in.
	if err := viper.ReadInConfig(); err == nil {
		fmt.Fprintln(os.Stderr, "Using config file:", viper.ConfigFileUsed())
	}
}

func runResync(cmd *cobra.Command, args []string) {
	fmt.Println("🔄 Starting Qdrant re-sync...")

	// Get configuration
	docsDir := viper.GetString("docs-dir")
	qdrantURL := viper.GetString("qdrant-url")
	openaiModel := viper.GetString("openai-model")
	dryRun := viper.GetBool("dry-run")
	force := viper.GetBool("force")

	fmt.Printf("📁 Docs directory: %s\n", docsDir)
	fmt.Printf("🔗 Qdrant URL: %s\n", qdrantURL)
	fmt.Printf("🤖 OpenAI Embedding Model: %s\n", openaiModel)

	if dryRun {
		fmt.Println("🔍 DRY RUN MODE - No changes will be made")
	}

	// Check if docs directory exists
	if _, err := os.Stat(docsDir); os.IsNotExist(err) {
		fmt.Printf("❌ Docs directory does not exist: %s\n", docsDir)
		os.Exit(1)
	}

	// Initialize services
	fmt.Println("\n🔧 Initializing services...")

	qdrantClient := services.NewQdrantClient(qdrantURL)
	embeddingService := services.NewEmbeddingService()
	chunker := services.NewChunker()

	// Check service availability
	if !force {
		fmt.Println("🔍 Checking service availability...")

		if !qdrantClient.IsAvailable() {
			fmt.Printf("❌ Qdrant is not available at %s\n", qdrantURL)
			fmt.Println("   Use --force to skip this check")
			os.Exit(1)
		}
		fmt.Println("✅ Qdrant is available")

		if !embeddingService.IsAvailable() {
			fmt.Printf("❌ OpenAI embedding service is not available\n")
			fmt.Println("   Make sure OPENAI_API_KEY is set correctly")
			fmt.Println("   Use --force to skip this check")
			os.Exit(1)
		}
		fmt.Println("✅ OpenAI embedding service is available")
	}

	// Initialize file processor with the same data directory as the main server
	dataDir := viper.GetString("data-dir")
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		fmt.Printf("❌ Failed to create data directory: %v\n", err)
		os.Exit(1)
	}

	fileProcessor, err := services.NewFileProcessor(qdrantClient, embeddingService, chunker, dataDir)
	if err != nil {
		fmt.Printf("❌ Failed to initialize file processor: %v\n", err)
		os.Exit(1)
	}
	defer fileProcessor.Stop()

	// Start file processor
	fileProcessor.Start()

	// Step 1: Delete existing Qdrant data
	fmt.Println("\n🗑️  Step 1: Clearing existing Qdrant data...")
	if !dryRun {
		if err := qdrantClient.DeleteCollection("workspace"); err != nil {
			fmt.Printf("⚠️  Failed to delete existing collection (may not exist): %v\n", err)
		} else {
			fmt.Println("✅ Deleted existing workspace collection")
		}

		// Recreate collection
		if err := qdrantClient.CreateCollection("workspace", 1536); err != nil {
			fmt.Printf("❌ Failed to create workspace collection: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("✅ Created new workspace collection")
	} else {
		fmt.Println("🔍 Would delete existing workspace collection")
		fmt.Println("🔍 Would create new workspace collection")
	}

	// Step 2: Find all markdown files
	fmt.Println("\n📄 Step 2: Finding all markdown files...")

	var markdownFiles []string
	err = filepath.Walk(docsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip directories and non-markdown files
		if info.IsDir() || !strings.HasSuffix(strings.ToLower(path), ".md") {
			return nil
		}

		// Skip hidden files
		if strings.HasPrefix(filepath.Base(path), ".") {
			return nil
		}

		markdownFiles = append(markdownFiles, path)
		return nil
	})

	if err != nil {
		fmt.Printf("❌ Failed to walk docs directory: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("📊 Found %d markdown files\n", len(markdownFiles))

	if len(markdownFiles) == 0 {
		fmt.Println("⚠️  No markdown files found to process")
		return
	}

	// Step 3: Queue all files for processing
	fmt.Println("\n📤 Step 3: Queuing files for processing...")

	queuedCount := 0
	for _, filePath := range markdownFiles {
		// Read file content
		content, err := os.ReadFile(filePath)
		if err != nil {
			fmt.Printf("⚠️  Failed to read %s: %v\n", filePath, err)
			continue
		}

		// Get relative path for display
		relPath, _ := filepath.Rel(docsDir, filePath)

		if !dryRun {
			// Queue the file for processing
			err = fileProcessor.QueueJob(filePath, string(content), "create")
			if err != nil {
				fmt.Printf("⚠️  Failed to queue %s: %v\n", relPath, err)
				continue
			}
		}

		queuedCount++
		fmt.Printf("📝 Queued: %s\n", relPath)
	}

	fmt.Printf("✅ Queued %d files for processing\n", queuedCount)

	if dryRun {
		fmt.Println("\n🔍 DRY RUN COMPLETE - No actual processing was done")
		return
	}

	// Step 4: Wait for processing to complete
	fmt.Println("\n⏳ Step 4: Waiting for processing to complete...")
	fmt.Println("   This may take several minutes depending on the number of files...")

	startTime := time.Now()
	lastStats := make(map[string]int)

	for {
		time.Sleep(5 * time.Second)

		// Get current stats
		stats := fileProcessor.GetStats()

		jobStats, ok := stats["job_stats"].(map[string]int)
		if !ok {
			continue
		}

		// Check if processing is complete
		pending := jobStats["pending"]
		processing := jobStats["processing"]
		completed := jobStats["completed"]
		failed := jobStats["failed"]

		// Show progress
		elapsed := time.Since(startTime)
		fmt.Printf("📊 Progress: %d completed, %d pending, %d processing, %d failed (elapsed: %v)\n",
			completed, pending, processing, failed, elapsed.Round(time.Second))

		// Check if we're done
		if pending == 0 && processing == 0 {
			if failed > 0 {
				fmt.Printf("⚠️  Processing completed with %d failed jobs\n", failed)
			} else {
				fmt.Println("✅ All files processed successfully!")
			}
			break
		}

		// Check for stuck processing (no change in stats)
		if pending == lastStats["pending"] && processing == lastStats["processing"] {
			if processing > 0 {
				fmt.Println("⚠️  Processing appears to be stuck, but continuing to wait...")
			}
		}

		lastStats = jobStats
	}

	totalTime := time.Since(startTime)
	fmt.Printf("\n🎉 Re-sync completed in %v\n", totalTime.Round(time.Second))

	// Final stats
	finalStats := fileProcessor.GetStats()
	if jobStats, ok := finalStats["job_stats"].(map[string]int); ok {
		fmt.Printf("📊 Final stats: %+v\n", jobStats)
	}
}
