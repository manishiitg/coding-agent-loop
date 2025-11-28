package vertex

import (
	"context"
	"fmt"
	"os"
	"time"

	llmproviders "llm-providers"
	"llm-providers/interfaces"
	"llm-providers/llmtypes"

	"github.com/joho/godotenv"
	"github.com/spf13/cobra"

	"llm-providers/internal/testing"
	sharedutils "llm-providers/internal/testing/commands/shared"
)

var VertexTokenUsageTestCmd = &cobra.Command{
	Use:   "vertex-token-usage",
	Short: "Test Vertex AI token usage extraction",
	Long: `Test token usage extraction from Vertex AI (Gemini) LLM calls.
	
This command tests if Vertex AI returns token usage information in their GenerationInfo.`,
	Run: runVertexTokenUsageTest,
}

var (
	vertexTokenTestPrompt string
)

func init() {
	VertexTokenUsageTestCmd.Flags().StringVar(&vertexTokenTestPrompt, "prompt", "Hello world", "Test prompt")
}

func runVertexTokenUsageTest(cmd *cobra.Command, args []string) {
	_ = godotenv.Load(".env")

	fmt.Printf("🧪 Testing Vertex AI Token Usage Extraction\n")
	fmt.Printf("===========================================\n\n")

	// Create simple message
	messages := []llmtypes.MessageContent{
		{
			Role:  llmtypes.ChatMessageTypeHuman,
			Parts: []llmtypes.ContentPart{llmtypes.TextContent{Text: vertexTokenTestPrompt}},
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
	mainTraceID := tracer.StartTrace("Vertex AI Token Usage Test", map[string]interface{}{
		"test_type": "token_usage_validation",
		"provider":  "vertex",
		"timestamp": time.Now().UTC(),
	})

	fmt.Printf("🔍 Started trace: %s\n", mainTraceID)

	// Test Vertex AI
	testVertexAITokenUsage(messages, mainTraceID, logger)

	// End trace
	tracer.EndTrace(mainTraceID, map[string]interface{}{
		"final_status": "completed",
		"success":      true,
		"test_type":    "token_usage_validation",
		"timestamp":    time.Now().UTC(),
	})

	fmt.Printf("\n🎉 Vertex AI Token Usage Test Complete!\n")
	fmt.Printf("🔍 Check Langfuse for trace: %s\n", mainTraceID)
}

// testVertexAITokenUsage runs Vertex AI token usage tests
func testVertexAITokenUsage(messages []llmtypes.MessageContent, mainTraceID interfaces.TraceID, logger interfaces.Logger) {
	// Test: Vertex AI (Google GenAI) for simple query
	fmt.Printf("\n🧪 TEST: Vertex AI / Google GenAI (Simple Query)\n")
	fmt.Printf("================================================\n")

	vertexConfig := llmproviders.Config{
		Provider:     llmproviders.ProviderVertex,
		ModelID:      "gemini-2.5-flash",
		Temperature:  0.7,
		EventEmitter: nil,
		TraceID:      mainTraceID,
		Logger:       logger,
		Context:      context.Background(),
	}

	vertexLLM, err := llmproviders.InitializeLLM(vertexConfig)
	if err != nil {
		fmt.Printf("❌ Error creating Vertex AI LLM: %v\n", err)
		fmt.Printf("⏭️  Skipping Vertex AI test\n")
		fmt.Printf("   Note: Make sure VERTEX_API_KEY or GOOGLE_API_KEY is set\n")
		return
	}

	fmt.Printf("🔧 Created Vertex AI LLM using providers.go (Google GenAI SDK)\n")
	sharedutils.TestLLMTokenUsage(vertexLLM, messages, vertexTokenTestPrompt)

	// Test cached tokens with multi-turn conversation
	fmt.Printf("\n🧪 TEST: Vertex AI (Multi-Turn Conversation with Cache)\n")
	fmt.Printf("=======================================================\n")
	sharedutils.TestLLMTokenUsageWithCache(vertexLLM)

	// Test: Vertex AI (Google GenAI) for tool calling with token usage
	fmt.Printf("\n🧪 TEST: Vertex AI / Google GenAI (Tool Calling with Token Usage)\n")
	fmt.Printf("==================================================================\n")

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

	fmt.Printf("🔧 Testing Vertex AI with tool calling to verify token usage extraction...\n")
	sharedutils.TestLLMTokenUsageWithTools(vertexLLM, toolMessages, []llmtypes.Tool{weatherTool})
}
