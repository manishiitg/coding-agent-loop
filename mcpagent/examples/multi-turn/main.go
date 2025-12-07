package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/joho/godotenv"

	mcpagent "mcpagent/agent"
	"mcpagent/llm"
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
		Provider:    llm.ProviderOpenAI,
		ModelID:     "gpt-4.1",
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
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	// Step 5: Create the agent (passing nil for logger and tracer - no tracing when tracer is nil)
	serverName := "" // Use all servers, or specify a specific server name
	modelID := "gpt-4.1"

	agent, err := mcpagent.NewAgent(
		ctx,
		llmModel,
		serverName, // server name (empty = all servers)
		configPath, // path to MCP config file
		modelID,    // model ID for tracking
		nil,        // tracer - nil means no tracing (traceID can be empty)
		"",         // trace ID - empty when tracer is nil (no tracing)
		nil,        // logger - nil will use default logger internally
		// Optional: Add agent options here
		// mcpagent.WithMaxTurns(10),
		// mcpagent.WithTemperature(0.7),
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create agent: %v\n", err)
		os.Exit(1)
	}

	// Step 6: Initialize conversation history (empty to start)
	conversationHistory := []llm.MessageContent{}

	// Step 7: Multi-turn conversation example
	questions := []string{
		"Get me the documentation for React library",
		"What are the main features mentioned in that documentation?",
		"Can you give me a code example from it?",
	}

	// Allow custom questions via command line
	if len(os.Args) > 2 {
		questions = os.Args[2:]
	}

	fmt.Println("=== Multi-Turn Conversation Example ===")
	fmt.Println()

	for i, question := range questions {
		fmt.Printf("--- Turn %d ---\n", i+1)
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
			os.Exit(1)
		}

		// Update conversation history with the response
		conversationHistory = updatedHistory

		// Print the answer
		fmt.Printf("Agent: %s\n\n", answer)
		fmt.Println("---")
		fmt.Println()
	}

	fmt.Println("=== Conversation Complete ===")
	fmt.Printf("Total turns: %d\n", len(questions))
	fmt.Printf("Total messages in history: %d\n", len(conversationHistory))
}
