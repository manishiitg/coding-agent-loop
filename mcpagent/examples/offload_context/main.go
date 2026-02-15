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
	agentLogFile := filepath.Join(logDir, "context-offloading.log")

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
		ModelID:     openai.ModelGPT41,
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

	// Step 4: Create logger for agent operations (MCP connections, tool execution, context offloading, etc.)
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

	// Step 6: Create a context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Step 7: Create the agent with context offloading enabled
	// Context offloading automatically saves large tool outputs to filesystem
	// and provides virtual tools for on-demand access
	agent, err := mcpagent.NewAgent(
		ctx,
		llmModel,
		configPath,
		mcpagent.WithLogger(agentLogger), // Use file logger for agent operations
		// Enable context offloading (enabled by default)
		mcpagent.WithContextOffloading(true),
		// Set threshold to 100 tokens for testing (triggers more easily)
		// Default is 10000 tokens, but we set it lower for demonstration
		mcpagent.WithLargeOutputThreshold(100),
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create agent: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Context offloading threshold set to 100 tokens (for testing)")

	// Step 8: Ask the agent a question that will produce large output
	// The agent will automatically use context offloading to save large results to filesystem
	question := "Search for comprehensive information about React library documentation and best practices. Provide detailed results."
	if len(os.Args) > 2 {
		question = os.Args[2]
	}

	fmt.Println("=== Context Offloading Example ===")
	fmt.Println("Question:", question)
	fmt.Println("\nNote: If the tool output is large (>100 tokens), context offloading will:")
	fmt.Println("  1. Save the full output to tool_output_folder/{session-id}/")
	fmt.Println("  2. Replace it with file path + preview in LLM context")
	fmt.Println("  3. Make it accessible via context offloading virtual tools (read_large_output, search_large_output, query_large_output)")
	fmt.Println()

	// Log question to agent log file
	agentLogger.Info("Context offloading example started", loggerv2.Field{Key: "question", Value: question})

	answer, err := agent.Ask(ctx, question)
	if err != nil {
		agentLogger.Error("Failed to get answer from agent", err)
		fmt.Fprintf(os.Stderr, "Failed to get answer: %v\n", err)
		os.Exit(1)
	}

	// Step 9: Print the answer
	fmt.Println("\n=== Agent Response ===")
	fmt.Println(answer)
	fmt.Println("=====================")

	// Log completion to agent log file
	agentLogger.Info("Context offloading example completed", loggerv2.Field{Key: "answer_length", Value: len(answer)})

	// Step 10: Show where context offloading saved the files
	toolOutputHandler := agent.GetToolOutputHandler()
	if toolOutputHandler != nil {
		outputFolder := toolOutputHandler.GetToolOutputFolder()
		sessionID := toolOutputHandler.GetSessionID()
		if sessionID != "" {
			fmt.Printf("\n📁 Context offloading files location: %s/%s/\n", outputFolder, sessionID)
		} else {
			fmt.Printf("\n📁 Context offloading files location: %s/\n", outputFolder)
		}
		fmt.Println("   (Check this directory for any large tool outputs that were saved via context offloading)")
	}
}
