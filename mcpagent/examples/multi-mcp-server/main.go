package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/joho/godotenv"

	mcpagent "github.com/manishiitg/mcpagent/agent"
	"github.com/manishiitg/mcpagent/llm"
	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"

	"github.com/manishiitg/multi-llm-provider-go/pkg/adapters/openai"
)

func main() {
	// Load .env file if it exists
	if _, err := os.Stat(".env"); err == nil {
		if err := godotenv.Load(); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: Could not load .env file: %v\n", err)
		}
	}

	// Step 1: Get OpenAI API key from environment
	openAIKey := os.Getenv("OPENAI_API_KEY")
	if openAIKey == "" {
		fmt.Fprintf(os.Stderr, "Please set OPENAI_API_KEY environment variable\n")
		os.Exit(1)
	}

	// Step 2: Set up file loggers
	// Create logs directory if it doesn't exist
	logDir := "logs"
	if err := os.MkdirAll(logDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create logs directory: %v\n", err)
		os.Exit(1)
	}

	// Define log file paths
	llmLogFile := filepath.Join(logDir, "llm.log")
	agentLogFile := filepath.Join(logDir, "multi-mcp-server.log")

	// Clear existing log files to start fresh for this run
	if err := os.Truncate(llmLogFile, 0); err != nil && !os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "Warning: Failed to clear LLM log file: %v\n", err)
	}
	if err := os.Truncate(agentLogFile, 0); err != nil && !os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "Warning: Failed to clear agent log file: %v\n", err)
	}

	// Create logger for LLM operations (API calls, token usage, etc.)
	llmLogger, err := loggerv2.New(loggerv2.Config{
		Level:      "info",     // Log level: debug, info, warn, error
		Format:     "text",     // Output format: text or json
		Output:     llmLogFile, // Write logs to file
		EnableFile: false,      // Output already set to file, no need for dual output
		FilePath:   "",         // Not needed when Output is set to file path
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create LLM logger: %v\n", err)
		os.Exit(1)
	}
	defer llmLogger.Close() // Ensure log file is closed on exit

	fmt.Printf("LLM logging to: %s (cleared)\n", llmLogFile)

	// Step 3: Initialize OpenAI LLM with file logger
	llmModel, err := llm.InitializeLLM(llm.Config{
		Provider:    llm.ProviderOpenAI,
		ModelID:     openai.ModelGPT41Mini,
		Temperature: 0.7,
		Logger:      llmLogger, // Use file logger for LLM operations
		APIKeys: &llm.ProviderAPIKeys{
			OpenAI: &openAIKey,
		},
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize LLM: %v\n", err)
		os.Exit(1)
	}

	// Step 4: Create logger for agent operations (MCP connections, tool execution, etc.)
	agentLogger, err := loggerv2.New(loggerv2.Config{
		Level:      "info",       // Log level: debug, info, warn, error
		Format:     "text",       // Output format: text or json
		Output:     agentLogFile, // Write logs to file
		EnableFile: false,        // Output already set to file, no need for dual output
		FilePath:   "",           // Not needed when Output is set to file path
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create agent logger: %v\n", err)
		os.Exit(1)
	}
	defer agentLogger.Close() // Ensure log file is closed on exit

	fmt.Printf("Agent logging to: %s (cleared)\n", agentLogFile)

	// Step 5: Set up MCP server configuration path
	configPath := "mcp_servers.json"
	if len(os.Args) > 1 {
		configPath = os.Args[1]
	}

	// Step 6: Create a context with timeout (longer timeout for multi-server operations)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	// Step 7: Create the agent with multiple MCP servers
	// By default, connects to all servers. Use WithServerName("server-name") to filter to specific server.
	// modelID is automatically extracted from llmModel
	// Tool filtering: Only allow read_email and search_emails from gmail, all tools from other servers

	agent, err := mcpagent.NewAgent(
		ctx,
		llmModel,
		configPath,                       // path to MCP config file
		mcpagent.WithLogger(agentLogger), // Use file logger for agent operations
		// Filter gmail to only allow read_email and search_emails
		mcpagent.WithSelectedTools([]string{"gmail:read_email", "gmail:search_emails"}),
		// Allow all tools from other servers
		mcpagent.WithSelectedServers([]string{"playwright", "sequential-thinking", "context7", "aws-knowledge-mcp", "google-sheets"}),
		// Caching is enabled by default
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create agent: %v\n", err)
		os.Exit(1)
	}

	// Step 8: Default task - demonstrates multi-server collaboration
	task := "Research cloud computing trends using browser automation (playwright), analyze the findings with sequential thinking, access AWS documentation for relevant services (aws-knowledge-mcp), and provide a comprehensive analysis. Use the browser to search for recent cloud computing trends, use sequential thinking to break down and analyze the information, and leverage AWS Knowledge MCP to access AWS documentation and best practices for relevant cloud services. Present your findings in a structured format with key insights."
	if len(os.Args) > 2 {
		// Allow custom task via command line
		task = os.Args[2]
	}

	fmt.Println("=== Multi-MCP Server Agent ===")
	fmt.Printf("Task: %s\n\n", task)
	fmt.Println("Starting multi-server collaboration...")
	fmt.Println()

	// Log task to agent log file
	agentLogger.Info("Multi-MCP server task started", loggerv2.Field{Key: "task", Value: task})

	// Step 9: Ask the agent to perform the task using multiple MCP servers
	answer, err := agent.Ask(ctx, task)
	if err != nil {
		agentLogger.Error("Failed to get answer from agent", err)
		fmt.Fprintf(os.Stderr, "Failed to get answer: %v\n", err)
		os.Exit(1)
	}

	// Step 10: Print the answer
	fmt.Println("\n=== Agent Response ===")
	fmt.Println(answer)
	fmt.Println("=====================")

	// Log completion to agent log file
	agentLogger.Info("Multi-MCP server task completed", loggerv2.Field{Key: "answer_length", Value: len(answer)})
}
