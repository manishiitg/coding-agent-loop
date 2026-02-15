package main

import (
	"context"
	"fmt"
	"os"

	llmproviders "github.com/manishiitg/multi-llm-provider-go"
	"github.com/manishiitg/multi-llm-provider-go/interfaces"
	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

// SimpleLogger is a minimal logger implementation for examples
type SimpleLogger struct{}

func (l *SimpleLogger) Infof(format string, v ...any) {
	fmt.Printf("[INFO] "+format+"\n", v...)
}

func (l *SimpleLogger) Errorf(format string, v ...any) {
	fmt.Printf("[ERROR] "+format+"\n", v...)
}

func (l *SimpleLogger) Debugf(format string, args ...interface{}) {
	fmt.Printf("[DEBUG] "+format+"\n", args...)
}

// NoOpEventEmitter is a minimal event emitter that does nothing
type NoOpEventEmitter struct{}

func (e *NoOpEventEmitter) EmitLLMInitializationStart(provider string, modelID string, temperature float64, traceID interfaces.TraceID, metadata interfaces.LLMMetadata) {
}

func (e *NoOpEventEmitter) EmitLLMInitializationSuccess(provider string, modelID string, capabilities string, traceID interfaces.TraceID, metadata interfaces.LLMMetadata) {
}

func (e *NoOpEventEmitter) EmitLLMInitializationError(provider string, modelID string, operation string, err error, traceID interfaces.TraceID, metadata interfaces.LLMMetadata) {
}

func (e *NoOpEventEmitter) EmitLLMGenerationSuccess(provider string, modelID string, operation string, messages int, temperature float64, messageContent string, responseLength int, choicesCount int, traceID interfaces.TraceID, metadata interfaces.LLMMetadata) {
}

func (e *NoOpEventEmitter) EmitLLMGenerationError(provider string, modelID string, operation string, messages int, temperature float64, messageContent string, err error, traceID interfaces.TraceID, metadata interfaces.LLMMetadata) {
}

func (e *NoOpEventEmitter) EmitToolCallDetected(provider string, modelID string, toolCallID string, toolName string, arguments string, traceID interfaces.TraceID, metadata interfaces.LLMMetadata) {
}

func main() {
	// Check for API key
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		fmt.Fprintf(os.Stderr, "Error: OPENAI_API_KEY environment variable is required\n")
		fmt.Fprintf(os.Stderr, "Set it with: export OPENAI_API_KEY=your-api-key\n")
		os.Exit(1)
	}

	// Configure the OpenAI provider
	// Note: Logger and EventEmitter can be nil - the library will use no-op implementations
	// You can also provide custom implementations if you want logging/events
	config := llmproviders.Config{
		Provider:     llmproviders.ProviderOpenAI,
		ModelID:      "gpt-4.1-mini", // Use a cost-effective model for examples
		Temperature:  0.7,
		Logger:       nil, // nil is allowed - uses no-op logger
		EventEmitter: nil, // nil is allowed - uses no-op event emitter
	}

	// Alternative: Use custom logger and event emitter for more control
	// logger := &SimpleLogger{}
	// eventEmitter := &NoOpEventEmitter{}
	// config.Logger = logger
	// config.EventEmitter = eventEmitter

	// Initialize the LLM
	fmt.Printf("Initializing OpenAI provider with model: %s\n", config.ModelID)
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
	fmt.Println("Sending request to OpenAI...")
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
