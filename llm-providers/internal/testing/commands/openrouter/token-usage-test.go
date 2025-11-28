package openrouter

import (
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

var OpenRouterTokenUsageTestCmd = &cobra.Command{
	Use:   "openrouter-token-usage",
	Short: "Test OpenRouter token usage extraction",
	Long: `Test token usage extraction from OpenRouter LLM calls.
	
This command tests if OpenRouter returns token usage information in their GenerationInfo.`,
	Run: runOpenRouterTokenUsageTest,
}

var (
	openrouterTokenTestPrompt string
)

func init() {
	OpenRouterTokenUsageTestCmd.Flags().StringVar(&openrouterTokenTestPrompt, "prompt", "Hello world", "Test prompt")
}

func runOpenRouterTokenUsageTest(cmd *cobra.Command, args []string) {
	_ = godotenv.Load(".env")

	fmt.Printf("🧪 Testing OpenRouter Token Usage Extraction\n")
	fmt.Printf("=============================================\n\n")

	// Create simple message
	messages := []llmtypes.MessageContent{
		{
			Role:  llmtypes.ChatMessageTypeHuman,
			Parts: []llmtypes.ContentPart{llmtypes.TextContent{Text: openrouterTokenTestPrompt}},
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
	mainTraceID := tracer.StartTrace("OpenRouter Token Usage Test", map[string]interface{}{
		"test_type": "token_usage_validation",
		"provider":  "openrouter",
		"timestamp": time.Now().UTC(),
	})

	fmt.Printf("🔍 Started trace: %s\n", mainTraceID)

	// Test OpenRouter
	testOpenRouterTokenUsage(messages, mainTraceID, logger)

	// End trace
	tracer.EndTrace(mainTraceID, map[string]interface{}{
		"final_status": "completed",
		"success":      true,
		"test_type":    "token_usage_validation",
		"timestamp":    time.Now().UTC(),
	})

	fmt.Printf("\n🎉 OpenRouter Token Usage Test Complete!\n")
	fmt.Printf("🔍 Check Langfuse for trace: %s\n", mainTraceID)
}

// testOpenRouterTokenUsage runs OpenRouter token usage tests
func testOpenRouterTokenUsage(messages []llmtypes.MessageContent, mainTraceID interfaces.TraceID, logger interfaces.Logger) {
	// Test 1: OpenRouter for simple query
	fmt.Printf("\n🧪 TEST: OpenRouter (Simple Query)\n")
	fmt.Printf("==================================\n")

	openrouterConfig := llmproviders.Config{
		Provider:     llmproviders.ProviderOpenRouter,
		ModelID:      "moonshotai/kimi-k2",
		Temperature:  0.7,
		EventEmitter: nil,
		TraceID:      mainTraceID,
		Logger:       logger,
	}

	openrouterLLM, err := llmproviders.InitializeLLM(openrouterConfig)
	if err != nil {
		fmt.Printf("❌ Error creating OpenRouter LLM: %v\n", err)
		fmt.Printf("⏭️  Skipping OpenRouter test\n")
		return
	}

	fmt.Printf("🔧 Created OpenRouter LLM using providers.go\n")
	sharedutils.TestLLMTokenUsage(openrouterLLM, messages, openrouterTokenTestPrompt)

	// Test 2: OpenRouter for complex reasoning query
	fmt.Printf("\n🧪 TEST: OpenRouter (Complex Reasoning Query)\n")
	fmt.Printf("==============================================\n")

	complexPrompt := `Please analyze the following complex scenario step by step: A company has 3 warehouses in different cities. Warehouse A can ship 100 units per day, Warehouse B can ship 150 units per day, and Warehouse C can ship 200 units per day. They need to fulfill orders for 5 customers: Customer 1 needs 80 units, Customer 2 needs 120 units, Customer 3 needs 90 units, Customer 4 needs 110 units, and Customer 5 needs 140 units. The shipping costs from each warehouse to each customer vary. Please create an optimal shipping plan that minimizes total cost while meeting all customer demands. Show your mathematical reasoning, create a cost matrix, and solve this step by step.`

	complexMessages := []llmtypes.MessageContent{
		{
			Role:  llmtypes.ChatMessageTypeHuman,
			Parts: []llmtypes.ContentPart{llmtypes.TextContent{Text: complexPrompt}},
		},
	}

	sharedutils.TestLLMTokenUsage(openrouterLLM, complexMessages, complexPrompt)

	// Test cached tokens with multi-turn conversation
	fmt.Printf("\n🧪 TEST: OpenRouter (Multi-Turn Conversation with Cache)\n")
	fmt.Printf("========================================================\n")
	sharedutils.TestLLMTokenUsageWithCache(openrouterLLM)
}
