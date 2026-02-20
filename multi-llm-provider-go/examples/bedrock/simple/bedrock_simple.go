package main

import (
	"context"
	"fmt"
	"os"

	llmproviders "github.com/manishiitg/multi-llm-provider-go"
	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

func main() {
	// Check for AWS credentials
	region := os.Getenv("AWS_REGION")
	if region == "" {
		region = "us-east-1" // Default region
	}
	accessKey := os.Getenv("AWS_ACCESS_KEY_ID")
	secretKey := os.Getenv("AWS_SECRET_ACCESS_KEY")

	if accessKey == "" || secretKey == "" {
		fmt.Fprintf(os.Stderr, "Error: AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY environment variables are required\n")
		fmt.Fprintf(os.Stderr, "Set them with:\n")
		fmt.Fprintf(os.Stderr, "  export AWS_REGION=us-east-1\n")
		fmt.Fprintf(os.Stderr, "  export AWS_ACCESS_KEY_ID=your-access-key\n")
		fmt.Fprintf(os.Stderr, "  export AWS_SECRET_ACCESS_KEY=your-secret-key\n")
		os.Exit(1)
	}

	// Configure the Bedrock provider
	// Note: Logger and EventEmitter can be nil - the library will use no-op implementations
	config := llmproviders.Config{
		Provider:     llmproviders.ProviderBedrock,
		ModelID:      "us.anthropic.claude-3-haiku-20240307-v1:0", // Use a cost-effective model for examples
		Temperature:  0.7,
		Logger:       nil, // nil is allowed - uses no-op logger
		EventEmitter: nil, // nil is allowed - uses no-op event emitter
	}

	// Initialize the LLM
	fmt.Printf("Initializing Bedrock provider with model: %s (region: %s)\n", config.ModelID, region)
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
	fmt.Println("Sending request to Bedrock...")
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
