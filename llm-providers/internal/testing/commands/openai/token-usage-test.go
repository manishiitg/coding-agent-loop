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

// testOpenAITokenUsage runs OpenAI token usage tests for multiple models
func testOpenAITokenUsage(messages []llmtypes.MessageContent, mainTraceID interfaces.TraceID, logger interfaces.Logger) {
	// Define models to test
	models := []struct {
		name        string
		modelID     string
		description string
	}{
		{"gpt-4.1-mini", "gpt-4.1-mini", "GPT-4.1 Mini model"},
		{"gpt-5", "gpt-5", "GPT-5 model"},
		{"o3-mini", "o3-mini", "O3 Mini model (supports reasoning tokens)"},
	}

	// Complex reasoning prompt for o3-mini (to test reasoning tokens)
	complexPrompt := `Please analyze the following complex scenario step by step: A company has 3 warehouses in different cities. Warehouse A can ship 100 units per day, Warehouse B can ship 150 units per day, and Warehouse C can ship 200 units per day. They need to fulfill orders for 5 customers: Customer 1 needs 80 units, Customer 2 needs 120 units, Customer 3 needs 90 units, Customer 4 needs 110 units, and Customer 5 needs 140 units. The shipping costs from each warehouse to each customer vary. Please create an optimal shipping plan that minimizes total cost while meeting all customer demands. Show your mathematical reasoning, create a cost matrix, and solve this step by step.`

	complexMessages := []llmtypes.MessageContent{
		{
			Role:  llmtypes.ChatMessageTypeHuman,
			Parts: []llmtypes.ContentPart{llmtypes.TextContent{Text: complexPrompt}},
		},
	}

	// Store LLM instances for cache tests
	llmInstances := make(map[string]llmtypes.Model)

	// Test 1-3: Simple query tests for all models
	for _, model := range models {
		fmt.Printf("\n🧪 TEST: OpenAI %s (Simple Query)\n", model.name)
		fmt.Printf("==========================================\n")

		config := llmproviders.Config{
			Provider:     llmproviders.ProviderOpenAI,
			ModelID:      model.modelID,
			Temperature:  0.7,
			EventEmitter: nil,
			TraceID:      mainTraceID,
			Logger:       logger,
		}

		llm, err := llmproviders.InitializeLLM(config)
		if err != nil {
			fmt.Printf("❌ Error creating OpenAI %s LLM: %v\n", model.name, err)
			fmt.Printf("⏭️  Skipping OpenAI %s test\n", model.name)
			continue
		}

		fmt.Printf("🔧 Created OpenAI %s LLM using providers.go\n", model.name)
		sharedutils.TestLLMTokenUsage(llm, messages, openaiTokenTestPrompt)
		llmInstances[model.name] = llm
	}

	// Test 4: Complex reasoning query for o3-mini (to validate reasoning tokens)
	fmt.Printf("\n🧪 TEST: OpenAI o3-mini (Complex Reasoning Query - Testing Reasoning Tokens)\n")
	fmt.Printf("===========================================================================\n")

	o3Config := llmproviders.Config{
		Provider:     llmproviders.ProviderOpenAI,
		ModelID:      "o3-mini",
		Temperature:  0.7,
		EventEmitter: nil,
		TraceID:      mainTraceID,
		Logger:       logger,
	}

	o3LLM, err := llmproviders.InitializeLLM(o3Config)
	if err != nil {
		fmt.Printf("❌ Error creating OpenAI o3-mini LLM: %v\n", err)
		fmt.Printf("⏭️  Skipping OpenAI o3-mini reasoning test\n")
	} else {
		fmt.Printf("🔧 Created OpenAI o3-mini LLM using providers.go\n")
		fmt.Printf("   Testing with complex reasoning prompt to validate reasoning tokens extraction\n")
		sharedutils.TestLLMTokenUsage(o3LLM, complexMessages, complexPrompt)
		llmInstances["o3-mini"] = o3LLM
	}

	// Test 5-7: Cache tests for all models
	for _, model := range models {
		fmt.Printf("\n🧪 TEST: OpenAI %s (Multi-Turn Conversation with Cache)\n", model.name)
		fmt.Printf("===================================================\n")

		llm, exists := llmInstances[model.name]
		if !exists {
			// Recreate LLM if it wasn't created earlier
			config := llmproviders.Config{
				Provider:     llmproviders.ProviderOpenAI,
				ModelID:      model.modelID,
				Temperature:  0.7,
				EventEmitter: nil,
				TraceID:      mainTraceID,
				Logger:       logger,
			}

			var err error
			llm, err = llmproviders.InitializeLLM(config)
			if err != nil {
				fmt.Printf("❌ Error creating OpenAI %s LLM: %v\n", model.name, err)
				fmt.Printf("⏭️  Skipping OpenAI %s cache test\n", model.name)
				continue
			}
		}

		sharedutils.TestLLMTokenUsageWithCache(llm)
	}
}
