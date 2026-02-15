package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/joho/godotenv"

	mcpagent "github.com/manishiitg/mcpagent/agent"
	"github.com/manishiitg/mcpagent/llm"
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

	// Step 2: Initialize OpenAI LLM (logger can be nil - will use default internally)
	llmModel, err := llm.InitializeLLM(llm.Config{
		Provider:    "openai",
		ModelID:     "gpt-4o",
		Temperature: 0.7,
		Logger:      nil, // nil logger - will use default logger internally
		APIKeys: &llm.ProviderAPIKeys{
			OpenAI: &openAIKey,
		},
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize LLM: %v\n", err)
		os.Exit(1)
	}

	// Step 3: Set up MCP server configuration path
	configPath := "mcp_servers.json"
	if len(os.Args) > 1 {
		configPath = os.Args[1]
	}

	// Step 4: Create a context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Step 5: Create the agent (passing nil for logger and tracer - no tracing when tracer is nil)
	// By default, connects to all servers. Use WithServerName() to filter to specific server(s).
	// modelID is automatically extracted from llmModel

	agent, err := mcpagent.NewAgent(
		ctx,
		llmModel,
		configPath, // path to MCP config file
		// Optional: Add agent options here
		// mcpagent.WithMaxTurns(10),
		// mcpagent.WithTemperature(0.7),
		// mcpagent.WithTracer(tracer),
		// mcpagent.WithTraceID("trace-id"),
		// mcpagent.WithLogger(logger),
		// mcpagent.WithServerName("server-name"), // Filter to specific server(s)
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create agent: %v\n", err)
		os.Exit(1)
	}

	// Step 7: Ask the agent a question (default uses context7 MCP server)
	question := "Get me the documentation for React library"
	if len(os.Args) > 2 {
		question = os.Args[2]
	}

	answer, err := agent.Ask(ctx, question)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to get answer: %v\n", err)
		os.Exit(1)
	}

	// Step 8: Print the answer
	fmt.Println("\n=== Agent Response ===")
	fmt.Println(answer)
	fmt.Println("=====================")
}
