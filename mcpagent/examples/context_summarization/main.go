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
	agentLogFile := filepath.Join(logDir, "context-summarization.log")

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
		ModelID:     openai.ModelGPT52,
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

	// Step 4: Create logger for agent operations
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

	// Step 5: Create required directories
	// Docker bind mounts require the source directory to exist
	if err := os.MkdirAll("projects", 0755); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create projects directory: %v\n", err)
		os.Exit(1)
	}

	// Step 6: Set up MCP server configuration path
	configPath := "mcp_servers.json"
	if len(os.Args) > 1 {
		configPath = os.Args[1]
	}

	// Step 7: Create a context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	// Step 8: Create the agent with context summarization enabled
	// Context summarization will summarize old conversation history when token usage
	// exceeds the threshold percentage of the model's context window
	agent, err := mcpagent.NewAgent(
		ctx,
		llmModel,
		configPath,
		mcpagent.WithLogger(agentLogger), // Use file logger for agent operations
		// Enable context summarization
		mcpagent.WithContextSummarization(true),
		// Enable token-based summarization trigger (when token usage exceeds threshold)
		mcpagent.WithSummarizeOnTokenThreshold(true, 0.5), // Trigger at 50% of context window
		// Keep last 8 messages when summarizing (default)
		mcpagent.WithSummaryKeepLastMessages(8),
		// Set max turns to 25 for testing (default)
		mcpagent.WithMaxTurns(25),
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create agent: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("=== Context Summarization Example ===")
	fmt.Println("Configuration:")
	fmt.Println("  - Context summarization: Enabled")
	fmt.Println("  - Token threshold: 50% of context window")
	fmt.Println("  - Keep last messages: 8")
	fmt.Println("  - MCP Servers: filesystem, fetch, memory, sequential-thinking")
	fmt.Println()
	fmt.Println("Complex Task: Multi-step research and development workflow")
	fmt.Println("  This task will use multiple MCP servers:")
	fmt.Println("  - fetch: Retrieve information from the web")
	fmt.Println("  - sequential-thinking: Analyze and structure information")
	fmt.Println("  - memory: Store insights and findings")
	fmt.Println("  - filesystem: Create documents and project structure")
	fmt.Println()
	fmt.Println("When token usage exceeds 50% of the model's context window, the agent will:")
	fmt.Println("  1. Summarize old conversation history using LLM")
	fmt.Println("  2. Keep the last 8 messages intact (ensuring tool call/response pairs stay together)")
	fmt.Println("  3. Replace old messages with a summary")
	fmt.Println("  4. Continue with reduced token usage")
	fmt.Println()

	// Step 9: Create a complex multi-turn conversation that will hit max turns
	// This task uses multiple MCP servers and will require many turns, triggering summarization
	question := `I need you to help me with a comprehensive research and development task. Here's what I need:

1. First, use the fetch server to retrieve information about the latest trends in AI agent development from a reputable tech blog or documentation site.

2. Use the sequential-thinking server to analyze and break down the key concepts you found into structured steps.

3. Use the memory server to store important insights and findings from your research.

4. Use the filesystem server to create a structured document with your findings. Create a directory structure and save your analysis.

5. Fetch additional information about best practices for implementing these concepts.

6. Use sequential-thinking again to create a detailed implementation plan.

7. Store the implementation plan in memory.

8. Use filesystem to create a project structure based on your plan.

9. Finally, provide a comprehensive summary of everything you've done, including what you learned, what you stored in memory, and what files you created.

This is a complex multi-step task that will require many tool calls and turns. Work through it systematically.`
	if len(os.Args) > 2 {
		question = os.Args[2]
	}

	fmt.Println("Question:", question)
	fmt.Println()

	// Log question to agent log file
	agentLogger.Info("Context summarization example started",
		loggerv2.Field{Key: "question", Value: question},
		loggerv2.Field{Key: "token_threshold_percent", Value: 0.5},
		loggerv2.Field{Key: "keep_last_messages", Value: 8})

	answer, err := agent.Ask(ctx, question)
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
	agentLogger.Info("Context summarization example completed",
		loggerv2.Field{Key: "answer_length", Value: len(answer)})

	fmt.Println("\n✅ Example completed!")
	fmt.Println("📝 Check logs/context-summarization.log for detailed logs")
	fmt.Println("📝 Check logs/llm.log for LLM API calls and summarization requests")
	fmt.Println()
	fmt.Println("Note: If token usage exceeded 50% of context window, you should see:")
	fmt.Println("  - ContextSummarizationStarted event")
	fmt.Println("  - ContextSummarizationCompleted event (with summary)")
	fmt.Println("  - Conversation continued with reduced token usage")
}
