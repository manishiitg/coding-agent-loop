package bedrock

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

var BedrockTokenUsageTestCmd = &cobra.Command{
	Use:   "bedrock-token-usage",
	Short: "Test Bedrock token usage extraction",
	Long: `Test token usage extraction from AWS Bedrock LLM calls.
	
This command tests if Bedrock returns token usage information in their GenerationInfo.`,
	Run: runBedrockTokenUsageTest,
}

var (
	bedrockTokenTestPrompt string
)

func init() {
	BedrockTokenUsageTestCmd.Flags().StringVar(&bedrockTokenTestPrompt, "prompt", "Hello world", "Test prompt")
}

func runBedrockTokenUsageTest(cmd *cobra.Command, args []string) {
	_ = godotenv.Load(".env")

	fmt.Printf("🧪 Testing Bedrock Token Usage Extraction\n")
	fmt.Printf("=========================================\n\n")

	// Create simple message
	messages := []llmtypes.MessageContent{
		{
			Role:  llmtypes.ChatMessageTypeHuman,
			Parts: []llmtypes.ContentPart{llmtypes.TextContent{Text: bedrockTokenTestPrompt}},
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
	mainTraceID := tracer.StartTrace("Bedrock Token Usage Test", map[string]interface{}{
		"test_type": "token_usage_validation",
		"provider":  "bedrock",
		"timestamp": time.Now().UTC(),
	})

	fmt.Printf("🔍 Started trace: %s\n", mainTraceID)

	// Test Bedrock
	testBedrockTokenUsage(messages, mainTraceID, logger)

	// End trace
	tracer.EndTrace(mainTraceID, map[string]interface{}{
		"final_status": "completed",
		"success":      true,
		"test_type":    "token_usage_validation",
		"timestamp":    time.Now().UTC(),
	})

	fmt.Printf("\n🎉 Bedrock Token Usage Test Complete!\n")
	fmt.Printf("🔍 Check Langfuse for trace: %s\n", mainTraceID)
}

// testBedrockTokenUsage runs Bedrock token usage tests
func testBedrockTokenUsage(messages []llmtypes.MessageContent, mainTraceID interfaces.TraceID, logger interfaces.Logger) {
	// Test 1: Bedrock Claude Sonnet for simple query
	fmt.Printf("\n🧪 TEST: Bedrock Claude Sonnet (Simple Query)\n")
	fmt.Printf("==============================================\n")

	bedrockConfig := llmproviders.Config{
		Provider:     llmproviders.ProviderBedrock,
		ModelID:      "global.anthropic.claude-sonnet-4-5-20250929-v1:0",
		Temperature:  0.7,
		EventEmitter: nil,
		TraceID:      mainTraceID,
		Logger:       logger,
	}

	bedrockLLM, err := llmproviders.InitializeLLM(bedrockConfig)
	if err != nil {
		fmt.Printf("❌ Error creating Bedrock Claude Sonnet LLM: %v\n", err)
		fmt.Printf("⏭️  Skipping Bedrock Claude Sonnet test\n")
	} else {
		fmt.Printf("🔧 Created Bedrock Claude Sonnet LLM using providers.go\n")
		sharedutils.TestLLMTokenUsage(context.Background(), bedrockLLM, messages, bedrockTokenTestPrompt)
	}

	// Test 2: Bedrock Claude Sonnet for complex reasoning query
	fmt.Printf("\n🧪 TEST: Bedrock Claude Sonnet (Complex Reasoning Query)\n")
	fmt.Printf("========================================================\n")

	if bedrockLLM == nil {
		bedrockLLM, err = llmproviders.InitializeLLM(bedrockConfig)
		if err != nil {
			fmt.Printf("❌ Error creating Bedrock Claude Sonnet LLM: %v\n", err)
			fmt.Printf("⏭️  Skipping Bedrock complex reasoning test\n")
			return
		}
	}

	complexPrompt := `Please analyze the following complex scenario step by step: A company has 3 warehouses in different cities. Warehouse A can ship 100 units per day, Warehouse B can ship 150 units per day, and Warehouse C can ship 200 units per day. They need to fulfill orders for 5 customers: Customer 1 needs 80 units, Customer 2 needs 120 units, Customer 3 needs 90 units, Customer 4 needs 110 units, and Customer 5 needs 140 units. The shipping costs from each warehouse to each customer vary. Please create an optimal shipping plan that minimizes total cost while meeting all customer demands. Show your mathematical reasoning, create a cost matrix, and solve this step by step.`

	complexMessages := []llmtypes.MessageContent{
		{
			Role:  llmtypes.ChatMessageTypeHuman,
			Parts: []llmtypes.ContentPart{llmtypes.TextContent{Text: complexPrompt}},
		},
	}

	sharedutils.TestLLMTokenUsage(context.Background(), bedrockLLM, complexMessages, complexPrompt)

	// Test 3: Multi-turn conversation with cache
	fmt.Printf("\n🧪 TEST: Bedrock (Multi-Turn Conversation with Cache)\n")
	fmt.Printf("====================================================\n")

	if bedrockLLM == nil {
		bedrockLLM, err = llmproviders.InitializeLLM(bedrockConfig)
		if err != nil {
			fmt.Printf("❌ Error creating Bedrock LLM: %v\n", err)
			fmt.Printf("⏭️  Skipping Bedrock cache test\n")
			return
		}
	}

	sharedutils.TestLLMTokenUsageWithCache(context.Background(), bedrockLLM)
}
