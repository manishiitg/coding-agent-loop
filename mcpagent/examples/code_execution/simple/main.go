package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/joho/godotenv"

	mcpagent "github.com/manishiitg/mcpagent/agent"
	"github.com/manishiitg/mcpagent/executor"
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
	agentLogFile := filepath.Join(logDir, "code-execution.log")

	// Clear existing log files to start fresh for this run
	if err := os.Truncate(llmLogFile, 0); err != nil && !os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "Warning: Failed to clear LLM log file: %v\n", err)
	}
	if err := os.Truncate(agentLogFile, 0); err != nil && !os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "Warning: Failed to clear agent log file: %v\n", err)
	}

	// Create logger for LLM operations
	llmLogger, err := loggerv2.New(loggerv2.Config{
		Level:      "info",
		Format:     "text",
		Output:     llmLogFile,
		EnableFile: false,
		FilePath:   "",
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create LLM logger: %v\n", err)
		os.Exit(1)
	}
	defer llmLogger.Close()

	fmt.Printf("LLM logging to: %s (cleared)\n", llmLogFile)

	// Step 3: Initialize OpenAI LLM with file logger
	llmModel, err := llm.InitializeLLM(llm.Config{
		Provider:    llm.ProviderOpenAI,
		ModelID:     openai.ModelGPT41,
		Temperature: 0.7,
		Logger:      llmLogger,
		APIKeys: &llm.ProviderAPIKeys{
			OpenAI: &openAIKey,
		},
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize LLM: %v\n", err)
		os.Exit(1)
	}

	// Step 4: Create logger for agent operations
	agentLogger, err := loggerv2.New(loggerv2.Config{
		Level:      "info",
		Format:     "text",
		Output:     agentLogFile,
		EnableFile: false,
		FilePath:   "",
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create agent logger: %v\n", err)
		os.Exit(1)
	}
	defer agentLogger.Close()

	fmt.Printf("Agent logging to: %s (cleared)\n", agentLogFile)

	// Step 5: Set up MCP server configuration path
	configPath := "mcp_servers.json"
	if len(os.Args) > 1 {
		configPath = os.Args[1]
	}

	// Step 6: Start HTTP server for code execution
	// Code execution mode requires an HTTP server to handle API calls from generated Go code
	fmt.Println("=== Code Execution Mode Example ===")
	fmt.Println("This example demonstrates code execution mode.")
	fmt.Println("The agent will automatically write and execute Go code when appropriate.")
	fmt.Println("You can give normal instructions - the agent handles code execution automatically.")
	fmt.Println()

	// Create executor handlers
	handlers := executor.NewExecutorHandlers(configPath, agentLogger)

	// Create HTTP mux and register handlers
	mux := http.NewServeMux()
	mux.HandleFunc("/api/mcp/execute", handlers.HandleMCPExecute)
	mux.HandleFunc("/api/custom/execute", handlers.HandleCustomExecute)
	mux.HandleFunc("/api/virtual/execute", handlers.HandleVirtualExecute)

	// Get server address from environment or use default
	serverAddr := os.Getenv("MCP_API_URL")
	if serverAddr == "" {
		serverAddr = "http://localhost:8000"
	}
	// Extract host:port from URL (remove http:// if present)
	if len(serverAddr) > 7 && serverAddr[:7] == "http://" {
		serverAddr = serverAddr[7:]
	} else if len(serverAddr) > 8 && serverAddr[:8] == "https://" {
		serverAddr = serverAddr[8:]
	}
	// Default to localhost:8000 if just port or empty
	if serverAddr == "" || (len(serverAddr) > 0 && serverAddr[0] == ':') {
		serverAddr = "127.0.0.1:8000"
	} else if len(serverAddr) > 0 && serverAddr[0] != ':' && !strings.Contains(serverAddr, ":") {
		// If just a port number, add localhost
		serverAddr = "127.0.0.1:" + serverAddr
	}

	// Start server
	server := &http.Server{
		Addr:    serverAddr,
		Handler: mux,
	}

	// Start server in background
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Fprintf(os.Stderr, "Server error: %v\n", err)
		}
	}()

	fmt.Printf("✓ HTTP server started on http://%s\n", serverAddr)
	fmt.Println("  Endpoints: /api/mcp/execute, /api/custom/execute, /api/virtual/execute")
	fmt.Printf("  (Set MCP_API_URL environment variable to change port)\n")
	fmt.Println()

	// Give server a moment to start
	time.Sleep(500 * time.Millisecond)

	// Shutdown function
	defer func() {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			fmt.Fprintf(os.Stderr, "Server shutdown error: %v\n", err)
		}
		fmt.Println("HTTP server stopped")
	}()

	// Step 7: Create a context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	// Step 8: Create the agent with code execution mode enabled
	agent, err := mcpagent.NewAgent(
		ctx,
		llmModel,
		configPath,
		mcpagent.WithLogger(agentLogger),
		mcpagent.WithCodeExecutionMode(true), // Enable code execution mode
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create agent: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("✓ Code execution mode enabled\n")
	fmt.Println()

	// Log start to agent log file
	agentLogger.Info("Code execution example started")

	// Step 9: Initialize conversation history (empty to start)
	conversationHistory := []llm.MessageContent{}

	// Step 10: Example questions that will trigger code execution
	// The agent will automatically use code execution mode when appropriate
	questions := []string{
		"Get me the documentation for React library",
		"Think through the problem: How can I improve the performance of a web application?",
	}

	// Allow custom questions via command line
	if len(os.Args) > 2 {
		questions = os.Args[2:]
	}

	// Step 11: Ask questions and demonstrate code execution with conversation history
	for i, question := range questions {
		fmt.Printf("--- Example %d ---\n", i+1)
		fmt.Printf("You: %s\n\n", question)

		// Add user message to conversation history
		userMessage := llm.MessageContent{
			Role:  llm.ChatMessageTypeHuman,
			Parts: []llm.ContentPart{llm.TextContent{Text: question}},
		}
		conversationHistory = append(conversationHistory, userMessage)

		// Ask the agent with conversation history
		answer, updatedHistory, err := agent.AskWithHistory(ctx, conversationHistory)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to get answer: %v\n", err)
			continue
		}

		// Update conversation history with the response
		conversationHistory = updatedHistory

		fmt.Printf("Agent: %s\n\n", answer)
		fmt.Println("---")
		fmt.Println()
	}

	fmt.Println("=== Example Complete ===")
	fmt.Println("Code execution mode allows the LLM to write and execute Go code")
	fmt.Println("Generated code is in the 'generated/' directory")
	fmt.Println("Code execution workspaces are in the 'workspace/' directory")
	fmt.Println("Check the logs directory for detailed execution logs")
	fmt.Printf("Total turns: %d\n", len(questions))
	fmt.Printf("Total messages in history: %d\n", len(conversationHistory))

	// Log completion to agent log file
	agentLogger.Info("Code execution example completed")
}
