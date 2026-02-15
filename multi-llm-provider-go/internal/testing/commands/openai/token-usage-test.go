package openai

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	llmproviders "github.com/manishiitg/multi-llm-provider-go"
	"github.com/manishiitg/multi-llm-provider-go/interfaces"
	"github.com/manishiitg/multi-llm-provider-go/internal/recorder"
	"github.com/manishiitg/multi-llm-provider-go/llmtypes"

	"github.com/joho/godotenv"
	"github.com/spf13/cobra"

	"github.com/manishiitg/multi-llm-provider-go/internal/testing"
	sharedutils "github.com/manishiitg/multi-llm-provider-go/internal/testing/commands/shared"
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
	openaiTokenTestRecord bool
	openaiTokenTestReplay bool
	openaiTokenTestDir    string
)

func init() {
	OpenAITokenUsageTestCmd.Flags().StringVar(&openaiTokenTestPrompt, "prompt", "Hello world", "Test prompt")
	OpenAITokenUsageTestCmd.Flags().BoolVar(&openaiTokenTestRecord, "record", false, "Record LLM responses to testdata/")
	OpenAITokenUsageTestCmd.Flags().BoolVar(&openaiTokenTestReplay, "replay", false, "Replay recorded responses from testdata/")
	OpenAITokenUsageTestCmd.Flags().StringVar(&openaiTokenTestDir, "test-dir", "testdata", "Directory for test recordings")
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

	// Setup recorder if recording or replaying
	ctx := context.Background()
	var rec *recorder.Recorder
	if openaiTokenTestRecord || openaiTokenTestReplay {
		recConfig := recorder.RecordingConfig{
			Enabled:  openaiTokenTestRecord,
			TestName: "token_usage",
			Provider: "openai",
			ModelID:  "gpt-5.1", // Default for simple test
			BaseDir:  openaiTokenTestDir,
		}
		rec = recorder.NewRecorder(recConfig)
		if openaiTokenTestReplay {
			rec.SetReplayMode(true)
		}

		if openaiTokenTestRecord {
			log.Printf("📹 Recording mode enabled - responses will be saved to %s", openaiTokenTestDir)
		}
		if openaiTokenTestReplay {
			log.Printf("▶️  Replay mode enabled - using recorded responses from %s", openaiTokenTestDir)
		}

		ctx = recorder.WithRecorder(ctx, rec)
	}

	// Test OpenAI
	testOpenAITokenUsage(ctx, messages, mainTraceID, logger, rec)

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
func testOpenAITokenUsage(ctx context.Context, messages []llmtypes.MessageContent, mainTraceID interfaces.TraceID, logger interfaces.Logger, rec *recorder.Recorder) {
	// First, run a simple test with gpt-5.1 using reasoning_effort and verbosity
	testSimpleReasoningTest(ctx, mainTraceID, logger, rec)

	// Then run the existing comprehensive tests
	// Define models to test
	models := []struct {
		name        string
		modelID     string
		description string
	}{
		{"gpt-4.1-mini", "gpt-4.1-mini", "GPT-4.1 Mini model"},
		{"gpt-5", "gpt-5", "GPT-5 model"},
		{"gpt-5.1", "gpt-5.1", "GPT-5.1 model (supports reasoning tokens with high thinking)"},
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

		// Setup recorder for this model if needed
		modelCtx := ctx
		if rec != nil {
			recConfig := rec.GetConfig()
			recConfig.ModelID = model.modelID
			rec = recorder.NewRecorder(recConfig)
			if openaiTokenTestReplay {
				rec.SetReplayMode(true)
			}
			modelCtx = recorder.WithRecorder(ctx, rec)
		}

		config := llmproviders.Config{
			Provider:     llmproviders.ProviderOpenAI,
			ModelID:      model.modelID,
			Temperature:  0.7,
			EventEmitter: nil,
			TraceID:      mainTraceID,
			Logger:       logger,
			Context:      modelCtx,
		}

		llm, err := llmproviders.InitializeLLM(config)
		if err != nil {
			fmt.Printf("❌ Error creating OpenAI %s LLM: %v\n", model.name, err)
			fmt.Printf("⏭️  Skipping OpenAI %s test\n", model.name)
			continue
		}

		fmt.Printf("🔧 Created OpenAI %s LLM using providers.go\n", model.name)
		sharedutils.TestLLMTokenUsage(modelCtx, llm, messages, openaiTokenTestPrompt)
		llmInstances[model.name] = llm
	}

	// Test 4: Complex reasoning query for gpt-5.1 with high thinking (to validate reasoning tokens)
	fmt.Printf("\n🧪 TEST: OpenAI gpt-5.1 (Complex Reasoning Query with High Thinking - Testing Reasoning Tokens)\n")
	fmt.Printf("==================================================================================================\n")

	// Setup recorder for gpt-5.1 if needed
	testCtx := ctx
	if rec != nil {
		recConfig := rec.GetConfig()
		recConfig.ModelID = "gpt-5.1"
		rec = recorder.NewRecorder(recConfig)
		if openaiTokenTestReplay {
			rec.SetReplayMode(true)
		}
		testCtx = recorder.WithRecorder(ctx, rec)
	}

	gpt51Config := llmproviders.Config{
		Provider:     llmproviders.ProviderOpenAI,
		ModelID:      "gpt-5.1",
		Temperature:  0.7,
		EventEmitter: nil,
		TraceID:      mainTraceID,
		Logger:       logger,
		Context:      testCtx,
	}

	gpt51LLM, err := llmproviders.InitializeLLM(gpt51Config)
	if err != nil {
		fmt.Printf("❌ Error creating OpenAI gpt-5.1 LLM: %v\n", err)
		fmt.Printf("⏭️  Skipping OpenAI gpt-5.1 reasoning test\n")
	} else {
		fmt.Printf("🔧 Created OpenAI gpt-5.1 LLM using providers.go\n")
		fmt.Printf("   Testing with complex reasoning prompt and high thinking to validate reasoning tokens extraction\n")
		fmt.Printf("   Using reasoning_effort=high for maximum reasoning depth\n")
		fmt.Printf("   Using verbosity=high for detailed verbose responses\n")
		sharedutils.TestLLMTokenUsage(testCtx, gpt51LLM, complexMessages, complexPrompt,
			llmtypes.WithReasoningEffort("high"),
			llmtypes.WithVerbosity("high"))
		llmInstances["gpt-5.1"] = gpt51LLM
	}

	// Test 5: Complex reasoning query for o3-mini (to validate reasoning tokens)
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
		sharedutils.TestLLMTokenUsage(context.Background(), o3LLM, complexMessages, complexPrompt)
		llmInstances["o3-mini"] = o3LLM
	}

	// Test 6-9: Cache tests for all models
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

		// Setup recorder for cache test if needed
		cacheCtx := ctx
		if rec != nil {
			recConfig := rec.GetConfig()
			recConfig.ModelID = model.modelID
			recConfig.TestName = "token_usage_cache"
			rec = recorder.NewRecorder(recConfig)
			if openaiTokenTestReplay {
				rec.SetReplayMode(true)
			}
			cacheCtx = recorder.WithRecorder(ctx, rec)
		}
		sharedutils.TestLLMTokenUsageWithCache(cacheCtx, llm)
	}
}

// testSimpleReasoningTest runs a simple test with "Hi" message using gpt-5.1 with reasoning_effort and verbosity
func testSimpleReasoningTest(ctx context.Context, mainTraceID interfaces.TraceID, logger interfaces.Logger, rec *recorder.Recorder) {
	fmt.Printf("\n🧪 SIMPLE TEST: OpenAI gpt-5.1 with Reasoning & Verbosity (Simple 'Hi' Message)\n")
	fmt.Printf("===============================================================================\n")

	// Setup recorder for this test if needed
	testCtx := ctx
	if rec != nil {
		recConfig := rec.GetConfig()
		recConfig.ModelID = "gpt-5.1"
		recConfig.TestName = "simple_reasoning"
		rec = recorder.NewRecorder(recConfig)
		if openaiTokenTestReplay {
			rec.SetReplayMode(true)
		}
		testCtx = recorder.WithRecorder(ctx, rec)
	}

	// Create simple "Hi" message
	simpleMessage := []llmtypes.MessageContent{
		{
			Role:  llmtypes.ChatMessageTypeHuman,
			Parts: []llmtypes.ContentPart{llmtypes.TextContent{Text: "Hi"}},
		},
	}

	// Initialize gpt-5.1 LLM
	gpt51Config := llmproviders.Config{
		Provider:     llmproviders.ProviderOpenAI,
		ModelID:      "gpt-5.1",
		Temperature:  0.7,
		EventEmitter: nil,
		TraceID:      mainTraceID,
		Logger:       logger,
		Context:      testCtx,
	}

	gpt51LLM, err := llmproviders.InitializeLLM(gpt51Config)
	if err != nil {
		fmt.Printf("❌ Error creating OpenAI gpt-5.1 LLM: %v\n", err)
		fmt.Printf("⏭️  Skipping simple reasoning test\n")
		return
	}

	fmt.Printf("🔧 Created OpenAI gpt-5.1 LLM\n")
	fmt.Printf("📝 Sending simple message: 'Hi'\n")
	fmt.Printf("⚙️  Configuration:\n")
	fmt.Printf("   - reasoning_effort: high\n")
	fmt.Printf("   - verbosity: high\n")
	if openaiTokenTestRecord {
		fmt.Printf("   - recording: enabled\n")
	}
	if openaiTokenTestReplay {
		fmt.Printf("   - replay: enabled\n")
	}
	fmt.Printf("\n")

	// Make the LLM call with reasoning_effort and verbosity
	startTime := time.Now()
	resp, err := gpt51LLM.GenerateContent(testCtx, simpleMessage,
		llmtypes.WithReasoningEffort("high"),
		llmtypes.WithVerbosity("high"))
	duration := time.Since(startTime)

	fmt.Printf("📊 Test Results:\n")
	fmt.Printf("================\n")

	if err != nil {
		fmt.Printf("❌ Error: %v\n", err)
		return
	}

	if resp == nil || resp.Choices == nil || len(resp.Choices) == 0 {
		fmt.Printf("❌ No response received\n")
		return
	}

	choice := resp.Choices[0]
	content := choice.Content

	fmt.Printf("✅ Response received successfully!\n")
	fmt.Printf("   Duration: %v\n", duration)
	fmt.Printf("   Response: %s\n\n", content)

	// Check token usage
	fmt.Printf("🔍 Token Usage Analysis:\n")
	fmt.Printf("========================\n")

	// Check unified Usage field
	if resp.Usage != nil {
		fmt.Printf("✅ Unified Usage field found!\n")
		fmt.Printf("   Input tokens:  %d\n", resp.Usage.InputTokens)
		fmt.Printf("   Output tokens: %d\n", resp.Usage.OutputTokens)
		fmt.Printf("   Total tokens:  %d\n", resp.Usage.TotalTokens)

		// Validate ReasoningTokens in unified Usage field (for gpt-5.1 with reasoning_effort=high)
		fmt.Printf("\n🔍 Validating ReasoningTokens in unified Usage field:\n")
		validated := sharedutils.ValidateReasoningTokensInUsage(resp.Usage, "gpt-5.1")
		if validated {
			fmt.Printf("   ✅ This confirms that reasoning_effort=high is working and tokens are extracted correctly!\n")
		}
	} else {
		fmt.Printf("⚠️  Unified Usage field not found\n")
	}

	// Check GenerationInfo for reasoning tokens (for detailed validation)
	if choice.GenerationInfo != nil {
		fmt.Printf("\n🔍 GenerationInfo Details (for reference):\n")
		info := choice.GenerationInfo

		// Check for reasoning tokens
		if info.ReasoningTokens != nil {
			fmt.Printf("✅ Reasoning tokens in GenerationInfo: %d\n", *info.ReasoningTokens)
		} else {
			fmt.Printf("⚠️  Reasoning tokens not found in GenerationInfo\n")
			// Check Additional map as fallback
			if info.Additional != nil {
				if value, ok := info.Additional["ReasoningTokens"]; ok {
					fmt.Printf("✅ Reasoning tokens found in Additional map: %v\n", value)
				} else if value, ok := info.Additional["reasoning_tokens"]; ok {
					fmt.Printf("✅ Reasoning tokens found in Additional map (lowercase): %v\n", value)
				}
			}
		}

		// Display other token info if available
		if info.InputTokens != nil {
			fmt.Printf("   Input tokens: %d\n", *info.InputTokens)
		}
		if info.OutputTokens != nil {
			fmt.Printf("   Output tokens: %d\n", *info.OutputTokens)
		}
		if info.TotalTokens != nil {
			fmt.Printf("   Total tokens: %d\n", *info.TotalTokens)
		}
	} else {
		fmt.Printf("⚠️  GenerationInfo not available\n")
	}

	fmt.Printf("\n")
}
