package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/joho/godotenv"

	"github.com/manishiitg/mcpagent/agent"
	"github.com/manishiitg/mcpagent/llm"
	"github.com/manishiitg/mcpagent/observability"
)

func main() {
	// Load .env file if it exists
	if _, err := os.Stat(".env"); err == nil {
		if err := godotenv.Load(); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: Could not load .env file: %v\n", err)
		}
	}

	// Step 1: Check for API keys or credentials
	// Vertex AI typically uses Application Default Credentials (ADC) or GOOGLE_APPLICATION_CREDENTIALS
	
	// Check for LangSmith credentials
	langsmithAPIKey := os.Getenv("LANGSMITH_API_KEY")
	if langsmithAPIKey == "" {
		fmt.Fprintf(os.Stderr, "Warning: LANGSMITH_API_KEY not set, tracing will be disabled\n")
	}

	// Step 2: Initialize Vertex LLM
	llmModel, err := llm.InitializeLLM(llm.Config{
		Provider:    llm.ProviderVertex,
		ModelID:     "gemini-3-flash-preview",
		Temperature: 0.7,
		Logger:      nil,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize LLM: %v\n", err)
		os.Exit(1)
	}

	// Step 3: Initialize tracer
	// The LangSmith tracer reads credentials from environment variables:
	// - LANGSMITH_API_KEY
	// - LANGSMITH_ENDPOINT (optional)
	// - LANGSMITH_PROJECT (optional)
	var tracer observability.Tracer

	if langsmithAPIKey != "" {
		// Use factory function to get tracer (reads from env vars)
		tracer = observability.GetTracer("langsmith")
		fmt.Println("LangSmith tracing enabled")
	} else {
		// Use noop tracer if LangSmith not configured
		tracer = observability.NoopTracer{}
		fmt.Println("Using noop tracer (no LangSmith credentials)")
	}

	// Step 4: Generate a unique trace ID
	traceID := fmt.Sprintf("example-trace-%d", time.Now().UnixNano())
	fmt.Printf("Trace ID: %s\n", traceID)

	// Step 5: Set up MCP server configuration path
	configPath := "mcp_servers.json"
	if len(os.Args) > 1 {
		configPath = os.Args[1]
	}

	// Step 6: Create a context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Step 7: Create the agent WITH tracer and traceID
	agent, err := mcpagent.NewAgent(
		ctx,
		llmModel,
		configPath,
		mcpagent.WithTracer(tracer),                        // Enable tracing
		mcpagent.WithTraceID(observability.TraceID(traceID)), // Set trace ID for this session
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create agent: %v\n", err)
		os.Exit(1)
	}

	// Step 8: Ask the agent a question
	question := "Get me the documentation for React library"
	if len(os.Args) > 2 {
		question = os.Args[2]
	}

	fmt.Printf("\n=== Question ===\n%s\n================\n\n", question)

	answer, err := agent.Ask(ctx, question)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to get answer: %v\n", err)
		if flusher, ok := tracer.(interface{ Flush() }); ok {
			fmt.Println("\nFlushing tracer on error...")
			flusher.Flush()
			fmt.Println("Tracer flushed successfully")
		}
		os.Exit(1)
	}

	// Step 9: Print the answer
	fmt.Println("\n=== Agent Response ===")
	fmt.Println(answer)
	fmt.Println("======================")

	// Step 10: Flush the tracer to ensure all events are sent
	if flusher, ok := tracer.(interface{ Flush() }); ok {
		fmt.Println("\nFlushing tracer...")
		flusher.Flush()
		fmt.Println("Tracer flushed successfully")
	}

	// Step 11: Print trace URL for easy access
	fmt.Printf("\n=== LangSmith Trace ===\n")
	fmt.Printf("Trace ID: %s\n", traceID)
	if langsmithAPIKey != "" {
		langsmithHost := os.Getenv("LANGSMITH_ENDPOINT")
		if langsmithHost == "" {
			langsmithHost = "https://smith.langchain.com"
		}
		fmt.Printf("View in LangSmith: %s\n", langsmithHost)
	}
	fmt.Println("======================")
}
