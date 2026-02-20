package main

import (
	"context"
	"fmt"
	"os"

	llmproviders "github.com/manishiitg/multi-llm-provider-go"
	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

func main() {
	// Check for API key
	apiKey := os.Getenv("VERTEX_API_KEY")
	if apiKey == "" {
		// Try alternative environment variable
		apiKey = os.Getenv("GOOGLE_API_KEY")
	}
	if apiKey == "" {
		fmt.Fprintf(os.Stderr, "Error: VERTEX_API_KEY or GOOGLE_API_KEY environment variable is required\n")
		fmt.Fprintf(os.Stderr, "Set it with: export VERTEX_API_KEY=your-api-key\n")
		fmt.Fprintf(os.Stderr, "Or: export GOOGLE_API_KEY=your-api-key\n")
		os.Exit(1)
	}

	// Configure the Vertex AI provider
	// Note: Logger and EventEmitter can be nil - the library will use no-op implementations
	config := llmproviders.Config{
		Provider:     llmproviders.ProviderVertex,
		ModelID:      "gemini-2.5-flash", // Use a cost-effective model for examples
		Temperature:  0.7,
		Logger:       nil, // nil is allowed - uses no-op logger
		EventEmitter: nil, // nil is allowed - uses no-op event emitter
	}

	// Initialize the LLM
	fmt.Printf("Initializing Vertex AI provider with model: %s\n", config.ModelID)
	llm, err := llmproviders.InitializeLLM(config)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize LLM: %v\n", err)
		os.Exit(1)
	}

	// Create context
	ctx := context.Background()

	// Prepare the message
	messages := []llmtypes.MessageContent{
		llmtypes.TextParts(llmtypes.ChatMessageTypeHuman, "Hello! Can you introduce yourself in one sentence?"),
	}

	// Generate content
	fmt.Println("Sending request to Vertex AI...")
	response, err := llm.GenerateContent(ctx, messages)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to generate content: %v\n", err)
		os.Exit(1)
	}

	// Display the response
	if len(response.Choices) > 0 {
		choice := response.Choices[0]
		fmt.Println("\n📝 Response:")
		fmt.Println(choice.Content)
		fmt.Println()

		// Display token usage if available
		if choice.GenerationInfo != nil {
			fmt.Println("📊 Token Usage:")
			if choice.GenerationInfo.InputTokens != nil {
				fmt.Printf("  Input tokens:  %d\n", *choice.GenerationInfo.InputTokens)
			}
			if choice.GenerationInfo.OutputTokens != nil {
				fmt.Printf("  Output tokens: %d\n", *choice.GenerationInfo.OutputTokens)
			}
			if choice.GenerationInfo.TotalTokens != nil {
				fmt.Printf("  Total tokens:  %d\n", *choice.GenerationInfo.TotalTokens)
			}
		}
	} else {
		fmt.Println("No response received")
	}
}
