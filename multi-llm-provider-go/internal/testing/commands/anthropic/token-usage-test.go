package anthropic

import (
	"context"
	"fmt"
	"os"
	"time"

	llmproviders "github.com/manishiitg/multi-llm-provider-go"
	"github.com/manishiitg/multi-llm-provider-go/interfaces"
	"github.com/manishiitg/multi-llm-provider-go/llmtypes"

	"github.com/joho/godotenv"
	"github.com/spf13/cobra"

	"github.com/manishiitg/multi-llm-provider-go/internal/testing"
	sharedutils "github.com/manishiitg/multi-llm-provider-go/internal/testing/commands/shared"
)

var AnthropicTokenUsageTestCmd = &cobra.Command{
	Use:   "anthropic-token-usage",
	Short: "Test Anthropic token usage extraction",
	Long: `Test token usage extraction from Anthropic (Claude) LLM calls.
	
This command tests if Anthropic returns token usage information in their GenerationInfo.`,
	Run: runAnthropicTokenUsageTest,
}

var (
	anthropicTokenTestPrompt string
)

func init() {
	AnthropicTokenUsageTestCmd.Flags().StringVar(&anthropicTokenTestPrompt, "prompt", "Hello world", "Test prompt")
}

func runAnthropicTokenUsageTest(cmd *cobra.Command, args []string) {
	_ = godotenv.Load(".env")

	fmt.Printf("🧪 Testing Anthropic Token Usage Extraction\n")
	fmt.Printf("==========================================\n\n")

	// Create simple message
	messages := []llmtypes.MessageContent{
		{
			Role:  llmtypes.ChatMessageTypeHuman,
			Parts: []llmtypes.ContentPart{llmtypes.TextContent{Text: anthropicTokenTestPrompt}},
		},
	}

	// Initialize logger
	logger := testing.GetTestLogger()

	// Set environment for Langfuse tracing
	os.Setenv("TRACING_PROVIDER", "langfuse")
	os.Setenv("LANGFUSE_DEBUG", "true")

	// Initialize tracer
	tracer := testing.InitializeTracer(logger)

	// Start trace
	mainTraceID := tracer.StartTrace("Anthropic Token Usage Test", map[string]interface{}{
		"test_type": "token_usage_validation",
		"provider":  "anthropic",
		"timestamp": time.Now().UTC(),
	})

	fmt.Printf("🔍 Started trace: %s\n", mainTraceID)

	// Test Anthropic
	testAnthropicTokenUsage(messages, mainTraceID, logger)

	// End trace
	tracer.EndTrace(mainTraceID, map[string]interface{}{
		"final_status": "completed",
		"success":      true,
		"test_type":    "token_usage_validation",
		"timestamp":    time.Now().UTC(),
	})

	fmt.Printf("\n🎉 Anthropic Token Usage Test Complete!\n")
	fmt.Printf("🔍 Check Langfuse for trace: %s\n", mainTraceID)
}

// testAnthropicTokenUsage runs Anthropic token usage tests
func testAnthropicTokenUsage(messages []llmtypes.MessageContent, mainTraceID interfaces.TraceID, logger interfaces.Logger) {
	fmt.Printf("\n🧪 TEST: Anthropic Direct API (Simple Query)\n")
	fmt.Printf("============================================\n")

	anthropicConfig := llmproviders.Config{
		Provider:     llmproviders.ProviderAnthropic,
		ModelID:      "claude-haiku-4-5-20251001",
		Temperature:  0.7,
		EventEmitter: nil,
		TraceID:      mainTraceID,
		Logger:       logger,
	}

	anthropicLLM, err := llmproviders.InitializeLLM(anthropicConfig)
	if err != nil {
		fmt.Printf("❌ Error creating Anthropic Claude LLM: %v\n", err)
		fmt.Printf("⏭️  Skipping Anthropic test\n")
		fmt.Printf("   Note: Make sure ANTHROPIC_API_KEY is set\n")
		return
	}

	fmt.Printf("🔧 Created Anthropic Claude LLM using providers.go (Anthropic SDK)\n")
	sharedutils.TestLLMTokenUsage(context.Background(), anthropicLLM, messages, anthropicTokenTestPrompt)

	// Test cached tokens with multi-turn conversation
	fmt.Printf("\n🧪 TEST: Anthropic (Multi-Turn Conversation with Cache)\n")
	fmt.Printf("======================================================\n")
	sharedutils.TestLLMTokenUsageWithCache(context.Background(), anthropicLLM)

	// Test: Anthropic direct API for tool calling with token usage
	fmt.Printf("\n🧪 TEST: Anthropic Direct API (Tool Calling with Token Usage)\n")
	fmt.Printf("==============================================================\n")

	// Create a simple tool for testing
	weatherTool := llmtypes.Tool{
		Type: "function",
		Function: &llmtypes.FunctionDefinition{
			Name:        "get_weather",
			Description: "Get current weather for a location",
			Parameters: llmtypes.NewParameters(map[string]interface{}{
				"type": "object",
				"properties": map[string]any{
					"location": map[string]any{
						"type":        "string",
						"description": "City name",
					},
				},
				"required": []string{"location"},
			}),
		},
	}

	toolMessages := []llmtypes.MessageContent{
		{
			Role:  llmtypes.ChatMessageTypeHuman,
			Parts: []llmtypes.ContentPart{llmtypes.TextContent{Text: "What's the weather in Tokyo?"}},
		},
	}

	fmt.Printf("🔧 Testing Anthropic with tool calling to verify token usage extraction...\n")
	sharedutils.TestLLMTokenUsageWithTools(context.Background(), anthropicLLM, toolMessages, []llmtypes.Tool{weatherTool})
}
