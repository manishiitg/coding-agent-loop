package openai

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

var OpenAITokenUsageTestCmd = &cobra.Command{
	Use:   "openai-token-usage",
	Short: "Test OpenAI token usage extraction",
	Long: `Test token usage extraction from OpenAI LLM calls.
	
This command tests if OpenAI returns token usage information in their GenerationInfo.`,
	Run: runOpenAITokenUsageTest,
}

var (
	openaiTokenTestPrompt string
)

func init() {
	OpenAITokenUsageTestCmd.Flags().StringVar(&openaiTokenTestPrompt, "prompt", "Hello world", "Test prompt")
}

func runOpenAITokenUsageTest(cmd *cobra.Command, args []string) {
	_ = godotenv.Load(".env")

	fmt.Printf("🧪 Testing OpenAI Token Usage Extraction\n")
	fmt.Printf("========================================\n\n")

	// Create simple message
	messages := []llmtypes.MessageContent{
		{
			Role:  llmtypes.ChatMessageTypeHuman,
			Parts: []llmtypes.ContentPart{llmtypes.TextContent{Text: openaiTokenTestPrompt}},
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
	mainTraceID := tracer.StartTrace("OpenAI Token Usage Test", map[string]interface{}{
		"test_type": "token_usage_validation",
		"provider":  "openai",
		"timestamp": time.Now().UTC(),
	})

	fmt.Printf("🔍 Started trace: %s\n", mainTraceID)

	// Test OpenAI
	testOpenAITokenUsage(messages, mainTraceID, logger)

	// End trace
	tracer.EndTrace(mainTraceID, map[string]interface{}{
		"final_status": "completed",
		"success":      true,
		"test_type":    "token_usage_validation",
		"timestamp":    time.Now().UTC(),
	})

	fmt.Printf("\n🎉 OpenAI Token Usage Test Complete!\n")
	fmt.Printf("🔍 Check Langfuse for trace: %s\n", mainTraceID)
}

// testOpenAITokenUsage runs OpenAI token usage tests
func testOpenAITokenUsage(messages []llmtypes.MessageContent, mainTraceID interfaces.TraceID, logger interfaces.Logger) {
	// Test 1: OpenAI gpt-4.1-mini for simple query
	fmt.Printf("\n🧪 TEST: OpenAI gpt-4.1-mini (Simple Query)\n")
	fmt.Printf("==========================================\n")

	gpt41Config := llmproviders.Config{
		Provider:     llmproviders.ProviderOpenAI,
		ModelID:      "gpt-4.1-mini",
		Temperature:  0.7,
		EventEmitter: nil,
		TraceID:      mainTraceID,
		Logger:       logger,
	}

	gpt41LLM, err := llmproviders.InitializeLLM(gpt41Config)
	if err != nil {
		fmt.Printf("❌ Error creating OpenAI gpt-4.1-mini LLM: %v\n", err)
		fmt.Printf("⏭️  Skipping OpenAI gpt-4.1-mini test\n")
	} else {
		fmt.Printf("🔧 Created OpenAI gpt-4.1-mini LLM using providers.go\n")
		sharedutils.TestLLMTokenUsage(gpt41LLM, messages, openaiTokenTestPrompt)
	}

	// Test 2: OpenAI gpt-4o-mini for complex reasoning query
	fmt.Printf("\n🧪 TEST: OpenAI gpt-4o-mini (Complex Reasoning Query)\n")
	fmt.Printf("======================================================\n")

	o3Config := llmproviders.Config{
		Provider:     llmproviders.ProviderOpenAI,
		ModelID:      "gpt-4o-mini",
		Temperature:  0.7,
		EventEmitter: nil,
		TraceID:      mainTraceID,
		Logger:       logger,
	}

	o3LLM, err := llmproviders.InitializeLLM(o3Config)
	if err != nil {
		fmt.Printf("❌ Error creating OpenAI gpt-4o-mini LLM: %v\n", err)
		fmt.Printf("⏭️  Skipping OpenAI gpt-4o-mini test\n")
	} else {
		fmt.Printf("🔧 Created OpenAI gpt-4o-mini LLM using providers.go\n")

		complexPrompt := `Please analyze the following complex scenario step by step: A company has 3 warehouses in different cities. Warehouse A can ship 100 units per day, Warehouse B can ship 150 units per day, and Warehouse C can ship 200 units per day. They need to fulfill orders for 5 customers: Customer 1 needs 80 units, Customer 2 needs 120 units, Customer 3 needs 90 units, Customer 4 needs 110 units, and Customer 5 needs 140 units. The shipping costs from each warehouse to each customer vary. Please create an optimal shipping plan that minimizes total cost while meeting all customer demands. Show your mathematical reasoning, create a cost matrix, and solve this step by step.`

		complexMessages := []llmtypes.MessageContent{
			{
				Role:  llmtypes.ChatMessageTypeHuman,
				Parts: []llmtypes.ContentPart{llmtypes.TextContent{Text: complexPrompt}},
			},
		}

		sharedutils.TestLLMTokenUsage(o3LLM, complexMessages, complexPrompt)
	}

	// Test 3: Multi-turn conversation with cache
	fmt.Printf("\n🧪 TEST: OpenAI (Multi-Turn Conversation with Cache)\n")
	fmt.Printf("===================================================\n")

	if o3LLM == nil {
		o3LLM, err = llmproviders.InitializeLLM(o3Config)
		if err != nil {
			fmt.Printf("❌ Error creating OpenAI LLM: %v\n", err)
			fmt.Printf("⏭️  Skipping OpenAI cache test\n")
			return
		}
	}

	sharedutils.TestLLMTokenUsageWithCache(o3LLM)
}
