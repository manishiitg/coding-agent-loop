package shared

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

	"github.com/spf13/cobra"

	"github.com/manishiitg/multi-llm-provider-go/internal/testing"

	"github.com/joho/godotenv"
)

// TokenUsageTestCmd represents the token usage test command
var TokenUsageTestCmd = &cobra.Command{
	Use:   "token-usage",
	Short: "Test token usage extraction from LangChain LLM calls",
	Long: `Test token usage extraction from LangChain LLM calls.
	
This command directly tests LLM providers to see if they return token usage
information in their GenerationInfo, which is crucial for proper cost tracking
and observability.

Examples:
  llm-test token-usage                                    # Test all providers
  llm-test token-usage --provider openai                 # Test only OpenAI
  llm-test token-usage --provider bedrock               # Test only Bedrock
  llm-test token-usage --provider anthropic             # Test only Anthropic
  llm-test token-usage --provider openrouter            # Test only OpenRouter
  llm-test token-usage --provider vertex                # Test only Vertex AI
  llm-test token-usage --prompt "Custom prompt"         # Test with custom prompt
  llm-test token-usage --provider openai --prompt "Hi"  # Test OpenAI with custom prompt`,
	Run: runTokenUsageTest,
}

var (
	tokenTestPrompt   string
	tokenTestProvider string
	tokenTestRecord   bool
	tokenTestReplay   bool
	tokenTestDir      string
)

func init() {
	TokenUsageTestCmd.Flags().StringVar(&tokenTestPrompt, "prompt", "Hello world", "Test prompt")
	TokenUsageTestCmd.Flags().StringVar(&tokenTestProvider, "provider", "all", "Provider to test (openai, bedrock, anthropic, openrouter, vertex, all)")
	TokenUsageTestCmd.Flags().BoolVar(&tokenTestRecord, "record", false, "Record LLM responses to testdata/")
	TokenUsageTestCmd.Flags().BoolVar(&tokenTestReplay, "replay", false, "Replay recorded responses from testdata/")
	TokenUsageTestCmd.Flags().StringVar(&tokenTestDir, "test-dir", "testdata", "Directory for test recordings")
}

func runTokenUsageTest(cmd *cobra.Command, args []string) {
	// Load .env file for API keys
	_ = godotenv.Load(".env")

	fmt.Printf("🧪 Testing Token Usage Extraction from LangChain\n")
	fmt.Printf("================================================\n\n")

	// Validate provider flag
	selectedProvider := tokenTestProvider
	if selectedProvider != "all" && selectedProvider != "openai" && selectedProvider != "bedrock" &&
		selectedProvider != "anthropic" && selectedProvider != "openrouter" && selectedProvider != "vertex" {
		fmt.Printf("❌ Invalid provider: %s\n", selectedProvider)
		fmt.Printf("   Valid providers: openai, bedrock, anthropic, openrouter, vertex, all\n")
		return
	}

	// Test configuration
	fmt.Printf("🔧 Test Configuration:\n")
	fmt.Printf("   Provider: %s\n", selectedProvider)
	fmt.Printf("   Prompt: %s\n", tokenTestPrompt)
	if tokenTestRecord {
		fmt.Printf("   Recording: enabled (saving to %s)\n", tokenTestDir)
	}
	if tokenTestReplay {
		fmt.Printf("   Replay: enabled (using recordings from %s)\n", tokenTestDir)
	}
	fmt.Printf("\n")

	// Create simple message
	messages := []llmtypes.MessageContent{
		{
			Role:  llmtypes.ChatMessageTypeHuman,
			Parts: []llmtypes.ContentPart{llmtypes.TextContent{Text: tokenTestPrompt}},
		},
	}

	// Initialize logger and tracer for providers
	logger := testing.GetTestLogger()

	// Setup recorder if recording or replaying
	ctx := context.Background()
	var rec *recorder.Recorder
	if tokenTestRecord || tokenTestReplay {
		recConfig := recorder.RecordingConfig{
			Enabled:  tokenTestRecord,
			TestName: "token_usage",
			Provider: selectedProvider, // Will be overridden per provider
			ModelID:  "",               // Will be set per provider
			BaseDir:  tokenTestDir,
		}
		rec = recorder.NewRecorder(recConfig)
		if tokenTestReplay {
			rec.SetReplayMode(true)
		}

		if tokenTestRecord {
			log.Printf("📹 Recording mode enabled - responses will be saved to %s", tokenTestDir)
		}
		if tokenTestReplay {
			log.Printf("▶️  Replay mode enabled - using recorded responses from %s", tokenTestDir)
		}

		ctx = recorder.WithRecorder(ctx, rec)
	}

	// Set environment for Langfuse tracing
	os.Setenv("TRACING_PROVIDER", "langfuse")
	os.Setenv("LANGFUSE_DEBUG", "true")

	// Initialize tracer (simple noop tracer for token usage testing)
	tracer := testing.InitializeTracer(logger)

	// Start main trace for the entire token usage test
	mainTraceID := tracer.StartTrace("Token Usage Test: Multi-Provider Validation", map[string]interface{}{
		"test_type":   "token_usage_validation",
		"description": "Testing token usage extraction across OpenAI, Bedrock, Anthropic, OpenRouter, and Vertex AI (Google GenAI) providers",
		"timestamp":   time.Now().UTC(),
		"providers":   []string{"openai", "bedrock", "anthropic", "openrouter", "vertex"},
	})

	fmt.Printf("🔍 Started trace: %s\n", mainTraceID)

	// Track which providers were tested
	providersTested := []string{}

	// Test OpenAI
	if selectedProvider == "all" || selectedProvider == "openai" {
		providersTested = append(providersTested, "openai")
		testOpenAI(ctx, messages, mainTraceID, logger, rec, "openai")
	}

	// Test Bedrock
	if selectedProvider == "all" || selectedProvider == "bedrock" {
		providersTested = append(providersTested, "bedrock")
		testBedrock(ctx, messages, mainTraceID, logger, rec, "bedrock")
	}

	// Test Anthropic
	if selectedProvider == "all" || selectedProvider == "anthropic" {
		providersTested = append(providersTested, "anthropic")
		testAnthropic(ctx, messages, mainTraceID, logger, rec, "anthropic")
	}

	// Test OpenRouter
	if selectedProvider == "all" || selectedProvider == "openrouter" {
		providersTested = append(providersTested, "openrouter")
		testOpenRouter(ctx, messages, mainTraceID, logger, rec, "openrouter")
	}

	// Test Vertex AI
	if selectedProvider == "all" || selectedProvider == "vertex" {
		providersTested = append(providersTested, "vertex")
		testVertexAI(ctx, messages, mainTraceID, logger, rec, "vertex")
	}

	// End main trace with summary
	tracer.EndTrace(mainTraceID, map[string]interface{}{
		"final_status":     "completed",
		"success":          true,
		"test_type":        "token_usage_validation",
		"providers_tested": providersTested,
		"timestamp":        time.Now().UTC(),
	})

	fmt.Printf("\n🎉 Token Usage Test Complete!\n")
	fmt.Printf("   Providers tested: %v\n", providersTested)
	fmt.Printf("🔍 Check Langfuse for trace: %s\n", mainTraceID)
}

// testOpenAI runs OpenAI token usage tests
func testOpenAI(ctx context.Context, messages []llmtypes.MessageContent, mainTraceID interfaces.TraceID, logger interfaces.Logger, rec *recorder.Recorder, provider string) {
	// Setup recorder for this provider if needed
	testCtx := ctx
	if rec != nil {
		// Update recorder config for this provider
		recConfig := rec.GetConfig()
		recConfig.Provider = provider
		recConfig.ModelID = "gpt-4.1-mini" // Will be updated per model
		rec = recorder.NewRecorder(recConfig)
		if tokenTestReplay {
			rec.SetReplayMode(true)
		}
		testCtx = recorder.WithRecorder(ctx, rec)
	}

	// Test 1: OpenAI gpt-4.1 for simple query
	fmt.Printf("\n🧪 TEST: OpenAI gpt-4.1-mini (Simple Query)\n")
	fmt.Printf("==========================================\n")

	gpt41Config := llmproviders.Config{
		Provider:     llmproviders.ProviderOpenAI,
		ModelID:      "gpt-4.1-mini",
		Temperature:  0.7,
		EventEmitter: nil,
		TraceID:      mainTraceID,
		Logger:       logger,
		Context:      testCtx,
	}

	gpt41LLM, err := llmproviders.InitializeLLM(gpt41Config)
	if err != nil {
		fmt.Printf("❌ Error creating OpenAI gpt-4.1-mini LLM: %v\n", err)
		fmt.Printf("⏭️  Skipping OpenAI gpt-4.1 test\n")
	} else {
		fmt.Printf("🔧 Created OpenAI gpt-4.1-mini LLM using providers.go\n")
		testLLMTokenUsage(testCtx, gpt41LLM, messages)
	}

	// Test 2: OpenAI gpt-4o-mini for complex reasoning query
	// Note: gpt-4o-mini does not support reasoning tokens (only o3/o3-mini models do)
	fmt.Printf("\n🧪 TEST: OpenAI gpt-4o-mini (Complex Reasoning Query)\n")
	fmt.Printf("======================================================\n")

	// Update recorder config for this model
	if rec != nil {
		recConfig := rec.GetConfig()
		recConfig.ModelID = "gpt-4o-mini"
		rec = recorder.NewRecorder(recConfig)
		if tokenTestReplay {
			rec.SetReplayMode(true)
		}
		testCtx = recorder.WithRecorder(ctx, rec)
	}

	gpt4oConfig := llmproviders.Config{
		Provider:     llmproviders.ProviderOpenAI,
		ModelID:      "gpt-4o-mini",
		Temperature:  0.7,
		EventEmitter: nil,
		TraceID:      mainTraceID,
		Logger:       logger,
		Context:      testCtx,
	}

	gpt4oLLM, err := llmproviders.InitializeLLM(gpt4oConfig)
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

		testLLMTokenUsage(testCtx, gpt4oLLM, complexMessages)
	}

	// Note: For testing reasoning tokens, use o3-mini or o3 models
	// The dedicated openai-token-usage test includes o3-mini testing
}

// testBedrock runs Bedrock token usage tests
func testBedrock(ctx context.Context, messages []llmtypes.MessageContent, mainTraceID interfaces.TraceID, logger interfaces.Logger, rec *recorder.Recorder, provider string) {
	// Setup recorder for this provider if needed
	testCtx := ctx
	if rec != nil {
		recConfig := rec.GetConfig()
		recConfig.Provider = provider
		recConfig.ModelID = "global.anthropic.claude-sonnet-4-5-20250929-v1:0"
		rec = recorder.NewRecorder(recConfig)
		if tokenTestReplay {
			rec.SetReplayMode(true)
		}
		testCtx = recorder.WithRecorder(ctx, rec)
	}

	fmt.Printf("\n🧪 TEST: Bedrock Claude (Simple Query)\n")
	fmt.Printf("=====================================\n")

	bedrockConfig := llmproviders.Config{
		Provider:     llmproviders.ProviderBedrock,
		ModelID:      "global.anthropic.claude-sonnet-4-5-20250929-v1:0",
		Temperature:  0.7,
		EventEmitter: nil,
		TraceID:      mainTraceID,
		Logger:       logger,
		Context:      testCtx,
	}

	bedrockLLM, err := llmproviders.InitializeLLM(bedrockConfig)
	if err != nil {
		fmt.Printf("❌ Error creating Bedrock Claude LLM: %v\n", err)
		fmt.Printf("⏭️  Skipping Bedrock test\n")
	} else {
		fmt.Printf("🔧 Created Bedrock Claude LLM using providers.go\n")
		testLLMTokenUsage(testCtx, bedrockLLM, messages)
	}
}

// testAnthropic runs Anthropic token usage tests
func testAnthropic(ctx context.Context, messages []llmtypes.MessageContent, mainTraceID interfaces.TraceID, logger interfaces.Logger, rec *recorder.Recorder, provider string) {
	// Setup recorder for this provider if needed
	testCtx := ctx
	if rec != nil {
		recConfig := rec.GetConfig()
		recConfig.Provider = provider
		recConfig.ModelID = "claude-haiku-4-5-20251001"
		rec = recorder.NewRecorder(recConfig)
		if tokenTestReplay {
			rec.SetReplayMode(true)
		}
		testCtx = recorder.WithRecorder(ctx, rec)
	}

	// Test: Anthropic direct API for simple query
	fmt.Printf("\n🧪 TEST: Anthropic Direct API (Simple Query)\n")
	fmt.Printf("============================================\n")

	anthropicConfig := llmproviders.Config{
		Provider:     llmproviders.ProviderAnthropic,
		ModelID:      "claude-haiku-4-5-20251001",
		Temperature:  0.7,
		EventEmitter: nil,
		TraceID:      mainTraceID,
		Logger:       logger,
		Context:      testCtx,
	}

	anthropicLLM, err := llmproviders.InitializeLLM(anthropicConfig)
	if err != nil {
		fmt.Printf("❌ Error creating Anthropic Claude LLM: %v\n", err)
		fmt.Printf("⏭️  Skipping Anthropic test\n")
		fmt.Printf("   Note: Make sure ANTHROPIC_API_KEY is set\n")
		return
	}

	fmt.Printf("🔧 Created Anthropic Claude LLM using providers.go (Anthropic SDK)\n")
	testLLMTokenUsage(testCtx, anthropicLLM, messages)

	// Test cached tokens with multi-turn conversation
	fmt.Printf("\n🧪 TEST: Anthropic (Multi-Turn Conversation with Cache)\n")
	fmt.Printf("======================================================\n")
	testLLMTokenUsageWithCache(testCtx, anthropicLLM)

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
	testLLMTokenUsageWithTools(testCtx, anthropicLLM, toolMessages, []llmtypes.Tool{weatherTool})
}

// testOpenRouter runs OpenRouter token usage tests
func testOpenRouter(ctx context.Context, messages []llmtypes.MessageContent, mainTraceID interfaces.TraceID, logger interfaces.Logger, rec *recorder.Recorder, provider string) {
	// Setup recorder for this provider if needed
	testCtx := ctx
	if rec != nil {
		recConfig := rec.GetConfig()
		recConfig.Provider = provider
		recConfig.ModelID = "moonshotai/kimi-k2"
		rec = recorder.NewRecorder(recConfig)
		if tokenTestReplay {
			rec.SetReplayMode(true)
		}
		testCtx = recorder.WithRecorder(ctx, rec)
	}

	fmt.Printf("\n🧪 TEST: OpenRouter (Simple Query)\n")
	fmt.Printf("==================================\n")

	openrouterConfig := llmproviders.Config{
		Provider:     llmproviders.ProviderOpenRouter,
		ModelID:      "moonshotai/kimi-k2",
		Temperature:  0.7,
		EventEmitter: nil,
		TraceID:      mainTraceID,
		Logger:       logger,
		Context:      testCtx,
	}

	openrouterLLM, err := llmproviders.InitializeLLM(openrouterConfig)
	if err != nil {
		fmt.Printf("❌ Error creating OpenRouter LLM: %v\n", err)
		fmt.Printf("⏭️  Skipping OpenRouter test\n")
	} else {
		fmt.Printf("🔧 Created OpenRouter LLM using providers.go\n")
		testLLMTokenUsage(testCtx, openrouterLLM, messages)

		// Test cached tokens with multi-turn conversation
		fmt.Printf("\n🧪 TEST: OpenRouter (Multi-Turn Conversation with Cache)\n")
		fmt.Printf("========================================================\n")
		testLLMTokenUsageWithCache(testCtx, openrouterLLM)
	}
}

// testVertexAI runs Vertex AI token usage tests
func testVertexAI(ctx context.Context, messages []llmtypes.MessageContent, mainTraceID interfaces.TraceID, logger interfaces.Logger, rec *recorder.Recorder, provider string) {
	// Setup recorder for this provider if needed
	testCtx := ctx
	if rec != nil {
		recConfig := rec.GetConfig()
		recConfig.Provider = provider
		recConfig.ModelID = "gemini-2.5-flash"
		rec = recorder.NewRecorder(recConfig)
		if tokenTestReplay {
			rec.SetReplayMode(true)
		}
		testCtx = recorder.WithRecorder(ctx, rec)
	}

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
		Context:      testCtx,
	}

	vertexLLM, err := llmproviders.InitializeLLM(vertexConfig)
	if err != nil {
		fmt.Printf("❌ Error creating Vertex AI LLM: %v\n", err)
		fmt.Printf("⏭️  Skipping Vertex AI test\n")
		fmt.Printf("   Note: Make sure VERTEX_API_KEY or GOOGLE_API_KEY is set\n")
		return
	}

	fmt.Printf("🔧 Created Vertex AI LLM using providers.go (Google GenAI SDK)\n")
	testLLMTokenUsage(testCtx, vertexLLM, messages)

	// Test cached tokens with multi-turn conversation
	fmt.Printf("\n🧪 TEST: Vertex AI (Multi-Turn Conversation with Cache)\n")
	fmt.Printf("=======================================================\n")
	testLLMTokenUsageWithCache(testCtx, vertexLLM)

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
	testLLMTokenUsageWithTools(testCtx, vertexLLM, toolMessages, []llmtypes.Tool{weatherTool})
}

func testLLMTokenUsage(ctx context.Context, llm llmtypes.Model, messages []llmtypes.MessageContent) {
	startTime := time.Now()

	fmt.Printf("⏱️  Starting LLM call...\n")
	fmt.Printf("📝 Sending message: %s\n", tokenTestPrompt)

	// Make the LLM call
	resp, err := llm.GenerateContent(ctx, messages)

	duration := time.Since(startTime)

	fmt.Printf("\n📊 Token Usage Test Results:\n")
	fmt.Printf("============================\n")

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
	fmt.Printf("   Response length: %d chars\n", len(content))
	fmt.Printf("   Content: %s\n\n", content)

	// Check for token usage information (using unified Usage field)
	fmt.Printf("🔍 Token Usage Analysis:\n")
	fmt.Printf("========================\n")

	// First check the unified Usage field
	if resp.Usage != nil {
		fmt.Printf("✅ Unified Usage field found!\n")
		fmt.Printf("   Input tokens:  %d\n", resp.Usage.InputTokens)
		fmt.Printf("   Output tokens: %d\n", resp.Usage.OutputTokens)
		fmt.Printf("   Total tokens:  %d\n", resp.Usage.TotalTokens)
		fmt.Printf("\n✅ Token usage data is available via unified interface!\n")
		fmt.Printf("   This means proper cost tracking and observability will work\n")
	} else if choice.GenerationInfo != nil {
		fmt.Printf("⚠️  Unified Usage field not found, but GenerationInfo is available\n")
		fmt.Printf("   Falling back to GenerationInfo extraction...\n\n")
	} else {
		fmt.Printf("❌ No token usage found in response (neither Usage nor GenerationInfo)\n")
		fmt.Printf("   This means the LLM provider is not providing token usage data\n")
		fmt.Printf("   Token usage will need to be estimated\n")
		return
	}

	// Still check GenerationInfo for advanced metadata
	var foundTokens bool
	var info *llmtypes.GenerationInfo
	if choice.GenerationInfo != nil {
		fmt.Printf("\n🔍 Checking GenerationInfo for advanced metadata...\n\n")

		// Check for specific token fields
		tokenFields := map[string]string{
			"input_tokens":      "Input tokens",
			"output_tokens":     "Output tokens",
			"total_tokens":      "Total tokens",
			"prompt_tokens":     "Prompt tokens",
			"completion_tokens": "Completion tokens",
			// OpenAI-specific field names
			"PromptTokens":     "Prompt tokens (OpenAI)",
			"CompletionTokens": "Completion tokens (OpenAI)",
			"TotalTokens":      "Total tokens (OpenAI)",
			"ReasoningTokens":  "Reasoning tokens (OpenAI o3)",
			// Anthropic-specific field names
			"InputTokens":  "Input tokens (Anthropic)",
			"OutputTokens": "Output tokens (Anthropic)",
			// OpenRouter cache token fields
			"cache_tokens":     "Cache tokens (OpenRouter)",
			"cache_discount":   "Cache discount (OpenRouter)",
			"cache_write_cost": "Cache write cost (OpenRouter)",
			"cache_read_cost":  "Cache read cost (OpenRouter)",
		}

		foundTokens = false
		info = choice.GenerationInfo
		if info != nil {
			// Check typed fields
			if info.InputTokens != nil {
				fmt.Printf("✅ %s: %v\n", tokenFields["input_tokens"], *info.InputTokens)
				foundTokens = true
			}
			if info.OutputTokens != nil {
				fmt.Printf("✅ %s: %v\n", tokenFields["output_tokens"], *info.OutputTokens)
				foundTokens = true
			}
			if info.TotalTokens != nil {
				fmt.Printf("✅ %s: %v\n", tokenFields["total_tokens"], *info.TotalTokens)
				foundTokens = true
			}
			// Check for cached tokens
			if info.CachedContentTokens != nil {
				fmt.Printf("✅ Cached Content Tokens: %d\n", *info.CachedContentTokens)
				foundTokens = true
			}
			if info.CacheDiscount != nil {
				fmt.Printf("✅ Cache Discount: %.4f (%.2f%%)\n", *info.CacheDiscount, *info.CacheDiscount*100)
				foundTokens = true
			}
			// Check Additional map for other fields
			if info.Additional != nil {
				for field, label := range tokenFields {
					if field != "input_tokens" && field != "output_tokens" && field != "total_tokens" {
						if value, ok := info.Additional[field]; ok {
							fmt.Printf("✅ %s: %v\n", label, value)
							foundTokens = true
						}
					}
				}
				// Check for cache-related fields in Additional
				cacheFields := []string{"cache_tokens", "cache_read_tokens", "cache_write_tokens", "CacheReadInputTokens", "CacheCreationInputTokens"}
				for _, field := range cacheFields {
					if value, ok := info.Additional[field]; ok {
						fmt.Printf("✅ Cache field (%s): %v\n", field, value)
						foundTokens = true
					}
				}
			}
		}
	}

	// Summary - already printed Usage if available above
	if resp.Usage == nil && !foundTokens {
		fmt.Printf("❌ No standard token fields found\n")
		fmt.Printf("   GenerationInfo: %+v\n", info)
		fmt.Printf("\n   This suggests the LLM provider doesn't return token usage\n")
	}

	// Show all available GenerationInfo for debugging
	fmt.Printf("\n🔍 Complete GenerationInfo:\n")
	fmt.Printf("==========================\n")
	if info != nil {
		fmt.Printf("   InputTokens: %v\n", info.InputTokens)
		fmt.Printf("   OutputTokens: %v\n", info.OutputTokens)
		fmt.Printf("   TotalTokens: %v\n", info.TotalTokens)
		if info.Additional != nil {
			for key, value := range info.Additional {
				fmt.Printf("   %s: %v (type: %T)\n", key, value, value)
			}
		}
	} else {
		fmt.Printf("   GenerationInfo is nil\n")
	}

	// Show raw response structure for debugging
	fmt.Printf("\n🔍 Raw Response Structure:\n")
	fmt.Printf("==========================\n")
	fmt.Printf("   Response type: %T\n", resp)
	fmt.Printf("   Choices count: %d\n", len(resp.Choices))
	if len(resp.Choices) > 0 {
		choice := resp.Choices[0]
		fmt.Printf("   Choice type: %T\n", choice)
		fmt.Printf("   Content type: %T\n", choice.Content)
		fmt.Printf("   GenerationInfo type: %T\n", choice.GenerationInfo)
		if choice.GenerationInfo != nil {
			info := choice.GenerationInfo
			fmt.Printf("   GenerationInfo: InputTokens=%v, OutputTokens=%v, TotalTokens=%v\n",
				info.InputTokens, info.OutputTokens, info.TotalTokens)
		}
	}
}

// testLLMTokenUsageWithTools tests token usage extraction when using tools
func testLLMTokenUsageWithTools(ctx context.Context, llm llmtypes.Model, messages []llmtypes.MessageContent, tools []llmtypes.Tool) {
	startTime := time.Now()

	fmt.Printf("⏱️  Starting LLM call with tools...\n")
	fmt.Printf("📝 Sending message: %s\n", extractMessageText(messages))
	fmt.Printf("🔧 Tools count: %d\n", len(tools))

	// Make the LLM call with tools
	resp, err := llm.GenerateContent(ctx, messages, llmtypes.WithTools(tools))

	duration := time.Since(startTime)

	fmt.Printf("\n📊 Token Usage Test Results (with tools):\n")
	fmt.Printf("==========================================\n")

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
	hasToolCalls := len(choice.ToolCalls) > 0

	fmt.Printf("✅ Response received successfully!\n")
	fmt.Printf("   Duration: %v\n", duration)
	if hasToolCalls {
		fmt.Printf("   Tool calls: %d\n", len(choice.ToolCalls))
		for i, tc := range choice.ToolCalls {
			fmt.Printf("      Tool %d: %s\n", i+1, tc.FunctionCall.Name)
		}
	} else {
		fmt.Printf("   Response length: %d chars\n", len(content))
		if len(content) > 0 {
			preview := content
			if len(preview) > 100 {
				preview = preview[:100] + "..."
			}
			fmt.Printf("   Content: %s\n", preview)
		}
	}
	fmt.Printf("\n")

	// Check for token usage information (using unified Usage field)
	fmt.Printf("🔍 Token Usage Analysis (with tools):\n")
	fmt.Printf("======================================\n")

	// First check the unified Usage field
	if resp.Usage != nil {
		fmt.Printf("✅ Unified Usage field found!\n")
		fmt.Printf("   Input tokens:  %d\n", resp.Usage.InputTokens)
		fmt.Printf("   Output tokens: %d\n", resp.Usage.OutputTokens)
		fmt.Printf("   Total tokens:  %d\n", resp.Usage.TotalTokens)
		fmt.Printf("\n✅ Token usage data extracted successfully!\n")
	} else if choice.GenerationInfo != nil {
		fmt.Printf("⚠️  Unified Usage field not found, but GenerationInfo is available\n")
		fmt.Printf("   Falling back to GenerationInfo extraction...\n\n")
	} else {
		fmt.Printf("❌ No token usage found in response\n")
		fmt.Printf("   Token usage extraction failed\n")
		return
	}

	// Still check GenerationInfo for advanced metadata
	var foundTokens bool
	var info *llmtypes.GenerationInfo
	if choice.GenerationInfo != nil {
		fmt.Printf("\n🔍 Checking GenerationInfo for advanced metadata...\n\n")

		// Check for specific token fields (Google GenAI uses these field names)
		tokenFields := map[string]string{
			"input_tokens":  "Input tokens",
			"output_tokens": "Output tokens",
			"total_tokens":  "Total tokens",
		}

		foundTokens = false
		var inputTokens, outputTokens, totalTokens interface{}
		info = choice.GenerationInfo

		if info != nil {
			// Check typed fields
			if info.InputTokens != nil {
				inputTokens = *info.InputTokens
				fmt.Printf("✅ %s: %v\n", tokenFields["input_tokens"], inputTokens)
				foundTokens = true
			}
			if info.OutputTokens != nil {
				outputTokens = *info.OutputTokens
				fmt.Printf("✅ %s: %v\n", tokenFields["output_tokens"], outputTokens)
				foundTokens = true
			}
			if info.TotalTokens != nil {
				totalTokens = *info.TotalTokens
				fmt.Printf("✅ %s: %v\n", tokenFields["total_tokens"], totalTokens)
				foundTokens = true
			}
			// Check Additional map for other fields
			if info.Additional != nil {
				for field, label := range tokenFields {
					if field != "input_tokens" && field != "output_tokens" && field != "total_tokens" {
						if value, ok := info.Additional[field]; ok {
							fmt.Printf("✅ %s: %v\n", label, value)
							foundTokens = true
						}
					}
				}
			}
		}
	}

	// Validate token counts using unified Usage field if available
	if resp.Usage != nil {
		fmt.Printf("\n🔍 Token Usage Validation (from unified Usage field):\n")
		fmt.Printf("   Input tokens:  %d\n", resp.Usage.InputTokens)
		fmt.Printf("   Output tokens: %d\n", resp.Usage.OutputTokens)
		fmt.Printf("   Total tokens:  %d\n", resp.Usage.TotalTokens)

		// Check if total matches sum (allowing for slight discrepancies)
		calculatedTotal := resp.Usage.InputTokens + resp.Usage.OutputTokens
		if resp.Usage.TotalTokens > 0 {
			diff := resp.Usage.TotalTokens - calculatedTotal
			if diff < 0 {
				diff = -diff
			}
			if resp.Usage.TotalTokens == calculatedTotal {
				fmt.Printf("   ✅ Total tokens matches input + output\n")
			} else if diff <= 2 {
				fmt.Printf("   ⚠️  Total tokens differs from input+output by %d (acceptable)\n", diff)
			} else {
				fmt.Printf("   ⚠️  Total tokens (%d) differs significantly from input+output (%d)\n", resp.Usage.TotalTokens, calculatedTotal)
			}
		}

		// Check for reasonable token counts
		if resp.Usage.InputTokens > 0 && resp.Usage.OutputTokens >= 0 {
			fmt.Printf("   ✅ Token counts are reasonable\n")
		} else {
			fmt.Printf("   ⚠️  Unusual token counts detected\n")
		}
	} else if !foundTokens {
		fmt.Printf("❌ No standard token fields found in GenerationInfo\n")
		fmt.Printf("   GenerationInfo: %+v\n", info)
		fmt.Printf("\n   This suggests the adapter is not extracting token usage correctly\n")
	}

	// Show all available GenerationInfo for debugging
	fmt.Printf("\n🔍 Complete GenerationInfo:\n")
	fmt.Printf("==========================\n")
	if info != nil {
		fmt.Printf("   InputTokens: %v\n", info.InputTokens)
		fmt.Printf("   OutputTokens: %v\n", info.OutputTokens)
		fmt.Printf("   TotalTokens: %v\n", info.TotalTokens)
		if info.Additional != nil {
			for key, value := range info.Additional {
				fmt.Printf("   %s: %v (type: %T)\n", key, value, value)
			}
		}
	} else {
		fmt.Printf("   GenerationInfo is nil\n")
	}
}

// extractMessageText extracts text from messages for logging
func extractMessageText(messages []llmtypes.MessageContent) string {
	if len(messages) == 0 {
		return ""
	}
	firstMsg := messages[0]
	for _, part := range firstMsg.Parts {
		if textPart, ok := part.(llmtypes.TextContent); ok {
			text := textPart.Text
			if len(text) > 100 {
				return text[:100] + "..."
			}
			return text
		}
	}
	return ""
}

// testLLMTokenUsageWithCache tests token usage with multi-turn conversation to verify cache token extraction
// This creates a large context that gets cached, then makes a follow-up request that should use cached tokens
func testLLMTokenUsageWithCache(ctx context.Context, llm llmtypes.Model) {

	// Create a large context document that will be cached
	// Making it large enough (15000+ chars ≈ 3750+ tokens) to well exceed Anthropic's 2048 token minimum for Claude Haiku
	// Claude Haiku requires 2048 tokens minimum, while Claude 3.5 Sonnet/Opus require 1024 tokens
	// We use a much larger context to ensure we're well above the threshold even with actual tokenization
	largeContext := `The following is a comprehensive guide to software engineering best practices and methodologies:

1. **Version Control Systems**: Always use version control systems like Git for all projects. Commit frequently with meaningful, descriptive commit messages that explain what changed and why. Use branches for features, bug fixes, and experiments. Never commit directly to main/master branch. Use pull requests or merge requests for code review before merging. Tag releases appropriately. Keep commit history clean and organized. Use .gitignore to exclude unnecessary files. Understand branching strategies like Git Flow or GitHub Flow.

2. **Code Review Process**: All code changes should be reviewed by at least one other developer before merging. Code reviews help catch bugs early, improve code quality, share knowledge across the team, and ensure consistency. Reviewers should check for correctness, performance, security, maintainability, and adherence to coding standards. Provide constructive feedback. Respond to review comments promptly. Use automated tools to catch common issues before human review.

3. **Testing Strategies**: Write comprehensive tests for your code. Aim for high test coverage, especially for critical business logic. Use unit tests for individual functions and methods. Use integration tests for component interactions. Use end-to-end tests for complete workflows. Write tests before or alongside code (TDD/BDD). Keep tests fast, independent, and maintainable. Use mocking and stubbing appropriately. Test edge cases and error conditions. Automate test execution in CI/CD pipelines.

4. **Documentation Standards**: Document your code, APIs, architecture decisions, and processes. Good documentation helps new team members onboard quickly and serves as a reference for future work. Write clear comments explaining why, not what. Keep README files up to date. Document API endpoints with examples. Maintain architecture decision records (ADRs). Keep documentation close to code when possible. Use tools like JSDoc, GoDoc, or Sphinx for API documentation.

5. **Error Handling Patterns**: Always handle errors properly and explicitly. Don't ignore errors or use empty catch blocks. Provide meaningful error messages that help with debugging. Log errors with appropriate context. Use structured error types. Return errors early (fail fast). Handle errors at the appropriate level. Don't expose internal errors to users. Use error wrapping to preserve error context. Implement retry logic for transient failures.

6. **Security Best Practices**: Follow security best practices throughout development. Validate and sanitize all user inputs. Use parameterized queries to prevent SQL injection. Keep dependencies updated to patch security vulnerabilities. Use HTTPS for all network communication. Implement proper authentication and authorization. Follow principle of least privilege. Encrypt sensitive data at rest and in transit. Regular security audits and penetration testing. Stay informed about security advisories.

7. **Performance Optimization**: Write efficient code, but don't optimize prematurely. Profile your code to identify actual bottlenecks before optimizing. Use caching appropriately to improve performance. Optimize database queries. Use connection pooling. Implement pagination for large datasets. Use CDNs for static assets. Minimize network round trips. Consider async processing for long-running tasks. Monitor performance metrics in production.

8. **Code Quality Standards**: Follow coding standards and style guides consistently. Use linters and formatters to maintain consistency automatically. Refactor code regularly to keep it maintainable. Remove dead code. Keep functions small and focused (single responsibility). Use meaningful variable and function names. Avoid deep nesting. Keep cyclomatic complexity low. Use design patterns appropriately. Write self-documenting code.

9. **Continuous Integration and Deployment**: Automate your build, test, and deployment processes completely. Use CI/CD pipelines to catch issues early and deploy frequently. Run tests automatically on every commit. Use feature flags for gradual rollouts. Implement blue-green or canary deployments. Monitor deployments closely. Have rollback procedures ready. Automate infrastructure provisioning. Use infrastructure as code (IaC).

10. **Monitoring and Logging**: Implement comprehensive logging and monitoring systems. Log important events, errors, and state changes with appropriate log levels. Use structured logging with consistent formats. Monitor application performance metrics, error rates, and business metrics. Set up alerts for critical issues. Use distributed tracing for microservices. Keep logs searchable and analyzable. Implement log rotation and retention policies. Use monitoring tools like Prometheus, Grafana, or Datadog.

11. **Database Management**: Design database schemas carefully with proper normalization. Use indexes appropriately for query performance. Write efficient queries and avoid N+1 problems. Use transactions for data consistency. Implement proper backup and recovery procedures. Monitor database performance. Use connection pooling. Consider read replicas for scaling. Plan for database migrations carefully.

12. **API Design Principles**: Design RESTful APIs with clear, consistent naming conventions. Use appropriate HTTP methods and status codes. Version your APIs properly. Document APIs thoroughly with examples. Implement rate limiting and throttling. Use pagination for list endpoints. Return consistent response formats. Handle errors gracefully with proper error responses. Consider GraphQL for complex data requirements.

13. **Microservices Architecture**: When building large-scale applications, consider microservices architecture for better scalability and maintainability. Each service should have a single responsibility and be independently deployable. Use API gateways for routing and load balancing. Implement service discovery mechanisms. Use message queues for asynchronous communication between services. Design for failure and implement circuit breakers. Monitor service health and dependencies. Use containerization with Docker and orchestration with Kubernetes for deployment. Implement distributed tracing to track requests across services. Design APIs with backward compatibility in mind.

14. **Code Organization and Structure**: Organize code in a logical and maintainable structure. Follow language-specific conventions and best practices. Separate concerns into different modules or packages. Use dependency injection to reduce coupling. Keep business logic separate from infrastructure code. Use design patterns appropriately but don't over-engineer. Create clear interfaces between components. Document architectural decisions. Keep related code together. Use meaningful directory structures that reflect the application's domain.

15. **Performance Monitoring and Optimization**: Continuously monitor application performance in production environments. Use application performance monitoring (APM) tools to identify bottlenecks. Track key metrics like response times, throughput, error rates, and resource utilization. Set up alerts for performance degradation. Profile applications regularly to identify optimization opportunities. Use caching strategies effectively. Optimize database queries and indexes. Implement connection pooling. Use CDN for static assets. Consider lazy loading for non-critical resources. Monitor memory usage and prevent memory leaks.

16. **Security Best Practices Extended**: Implement comprehensive security measures throughout the development lifecycle. Use secure coding practices to prevent common vulnerabilities like SQL injection, XSS, and CSRF attacks. Implement proper input validation and sanitization. Use parameterized queries for database operations. Keep all dependencies and libraries up to date. Use security scanning tools in CI/CD pipelines. Implement proper authentication mechanisms like OAuth2 or JWT. Use HTTPS for all communications. Encrypt sensitive data at rest and in transit. Implement rate limiting to prevent abuse. Regular security audits and penetration testing. Follow the principle of least privilege for access control.

17. **Cloud Computing and Infrastructure**: Leverage cloud computing platforms to build scalable and resilient applications. Understand different cloud service models including Infrastructure as a Service (IaaS), Platform as a Service (PaaS), and Software as a Service (SaaS). Use cloud-native services for databases, storage, messaging, and compute. Implement auto-scaling to handle variable workloads. Use load balancers to distribute traffic efficiently. Implement disaster recovery and backup strategies. Monitor cloud resource usage and costs. Use infrastructure as code tools like Terraform or CloudFormation. Implement proper security groups and network policies. Use managed services to reduce operational overhead.

18. **Containerization and Orchestration**: Use containerization technologies like Docker to package applications and their dependencies. Create efficient Dockerfiles with multi-stage builds. Use container registries to store and distribute images. Implement container orchestration with Kubernetes for managing containerized applications at scale. Understand Kubernetes concepts including pods, services, deployments, and ingress controllers. Use Helm charts for application deployment. Implement health checks and readiness probes. Use ConfigMaps and Secrets for configuration management. Monitor container resource usage and set appropriate limits. Implement proper logging and monitoring for containerized applications.

19. **DevOps Practices**: Integrate development and operations teams to improve collaboration and efficiency. Automate infrastructure provisioning and configuration. Use configuration management tools like Ansible, Puppet, or Chef. Implement continuous integration and continuous deployment pipelines. Use infrastructure as code to manage cloud resources. Implement monitoring and alerting for production systems. Use log aggregation tools like ELK stack or Splunk. Implement blue-green and canary deployment strategies. Use feature flags for gradual feature rollouts. Automate testing at all levels including unit, integration, and end-to-end tests.

20. **Agile and Scrum Methodologies**: Follow agile development methodologies to deliver software incrementally and iteratively. Use Scrum framework with sprints, daily standups, sprint planning, and retrospectives. Break down work into user stories with clear acceptance criteria. Use story points or time estimates for planning. Maintain a prioritized product backlog. Conduct regular sprint reviews and retrospectives. Adapt and improve processes based on feedback. Use tools like Jira, Trello, or Azure DevOps for project management. Foster collaboration between team members. Focus on delivering value to customers quickly and frequently.

21. **Code Refactoring Techniques**: Regularly refactor code to improve its structure and maintainability without changing its external behavior. Identify code smells like long methods, large classes, duplicate code, and feature envy. Apply refactoring patterns like extract method, extract class, move method, and rename. Use automated refactoring tools when available. Write tests before refactoring to ensure behavior is preserved. Refactor in small, incremental steps. Keep refactoring separate from feature additions. Review refactored code to ensure improvements. Document the reasons for refactoring. Measure code quality metrics before and after refactoring.

22. **Design Patterns and Principles**: Understand and apply common design patterns to solve recurring problems. Learn creational patterns like Singleton, Factory, and Builder. Study structural patterns like Adapter, Decorator, and Facade. Master behavioral patterns like Observer, Strategy, and Command. Follow SOLID principles: Single Responsibility, Open/Closed, Liskov Substitution, Interface Segregation, and Dependency Inversion. Use design patterns appropriately but avoid over-engineering. Understand when not to use patterns. Keep patterns simple and maintainable. Document pattern usage in code comments. Review and refactor pattern implementations regularly.

23. **API Development and Integration**: Design and develop robust APIs that are easy to use and maintain. Follow RESTful principles for HTTP APIs. Use proper HTTP methods and status codes. Implement API versioning strategies. Document APIs with OpenAPI/Swagger specifications. Implement authentication and authorization mechanisms. Use rate limiting to prevent abuse. Handle errors gracefully with proper error responses. Implement request validation and sanitization. Use API gateways for routing and management. Monitor API usage and performance. Implement caching strategies for frequently accessed data. Use webhooks for event-driven integrations.

24. **Data Structures and Algorithms**: Understand fundamental data structures and algorithms for efficient problem-solving. Master arrays, linked lists, stacks, queues, trees, and graphs. Learn common algorithms including sorting, searching, and graph traversal. Analyze time and space complexity using Big O notation. Choose appropriate data structures for specific use cases. Optimize algorithms for performance when necessary. Practice solving algorithmic problems regularly. Understand trade-offs between different approaches. Use standard library implementations when available. Document algorithm choices and complexity analysis.

25. **Software Architecture Patterns**: Design scalable and maintainable software architectures. Understand monolithic, microservices, and serverless architectures. Use layered architecture for separation of concerns. Implement event-driven architecture for loose coupling. Use domain-driven design for complex business domains. Apply clean architecture principles. Use hexagonal architecture for testability. Implement CQRS pattern for read/write separation. Use API-first design approach. Document architectural decisions and trade-offs. Review and evolve architecture over time.

This comprehensive guide should be followed by all software engineers to ensure high-quality, maintainable, secure, and performant software systems. These practices form the foundation of professional software development and are essential for building reliable applications that can scale and evolve over time. The principles outlined here cover the entire software development lifecycle from initial design through deployment and maintenance. The guide emphasizes the importance of continuous learning, collaboration, and adaptation to new technologies and methodologies. Software engineering is a constantly evolving field, and staying current with best practices, tools, and techniques is crucial for long-term success.`

	fmt.Printf("📚 Creating multi-turn conversation with large context for cache testing...\n")
	fmt.Printf("   Context length: %d characters\n", len(largeContext))

	// First turn: Send the large context with an initial question
	// Store the exact text for reuse in turn 2
	fmt.Printf("\n🔄 Turn 1: Initial request with large context\n")
	fmt.Printf("============================================\n")
	fmt.Printf("   This request will create the cache for the large context\n")

	turn1Context := largeContext + "\n\nBased on this guide, what are the key principles for code quality?"
	turn1Messages := []llmtypes.MessageContent{
		{
			Role: llmtypes.ChatMessageTypeHuman,
			Parts: []llmtypes.ContentPart{
				llmtypes.TextContent{Text: turn1Context},
			},
		},
	}

	startTime := time.Now()
	resp1, err := llm.GenerateContent(ctx, turn1Messages)
	duration1 := time.Since(startTime)

	if err != nil {
		fmt.Printf("❌ Turn 1 Error: %v\n", err)
		return
	}

	if resp1 == nil || resp1.Choices == nil || len(resp1.Choices) == 0 {
		fmt.Printf("❌ Turn 1: No response received\n")
		return
	}

	choice1 := resp1.Choices[0]
	fmt.Printf("✅ Turn 1 completed in %v\n", duration1)
	if choice1.GenerationInfo != nil {
		input1 := 0
		output1 := 0
		if choice1.GenerationInfo.InputTokens != nil {
			input1 = *choice1.GenerationInfo.InputTokens
		}
		if choice1.GenerationInfo.OutputTokens != nil {
			output1 = *choice1.GenerationInfo.OutputTokens
		}
		fmt.Printf("   Turn 1 Tokens - Input: %d, Output: %d\n", input1, output1)

		// Check for cache creation tokens in Turn 1
		if choice1.GenerationInfo.Additional != nil {
			// Debug: Check raw values
			if rawRead, ok := choice1.GenerationInfo.Additional["_debug_cache_read_raw"]; ok {
				fmt.Printf("   🔍 Turn 1 Raw CacheReadInputTokens: %v\n", rawRead)
			}
			if rawCreate, ok := choice1.GenerationInfo.Additional["_debug_cache_creation_raw"]; ok {
				fmt.Printf("   🔍 Turn 1 Raw CacheCreationInputTokens: %v\n", rawCreate)
			}

			if cacheCreate, ok := choice1.GenerationInfo.Additional["CacheCreationInputTokens"]; ok {
				fmt.Printf("   ✅ Turn 1 Cache Creation Tokens: %v (cache was created!)\n", cacheCreate)
			} else {
				fmt.Printf("   ⚠️  Turn 1: No cache creation tokens found (raw value was 0)\n")
			}
		} else {
			fmt.Printf("   ⚠️  Turn 1: Additional map is nil\n")
		}
		if choice1.GenerationInfo.CachedContentTokens != nil {
			fmt.Printf("   ✅ Turn 1 CachedContentTokens: %d\n", *choice1.GenerationInfo.CachedContentTokens)
		}
	} else {
		fmt.Printf("   ⚠️  Turn 1: GenerationInfo is nil\n")
	}

	// Second turn: Send the EXACT same message structure as Turn 1 to trigger caching
	// IMPORTANT: OpenRouter requires exact prefix matching for cache hits
	// If we change the message structure (e.g., add conversation history), the cache won't match
	// So we send the exact same single message to verify caching works
	fmt.Printf("\n🔄 Turn 2: Follow-up request (should use cached context)\n")
	fmt.Printf("======================================================\n")
	fmt.Printf("   Sending EXACT same message structure as Turn 1 to trigger caching...\n")
	fmt.Printf("   OpenRouter requires exact prefix matching - same message structure is critical\n")
	fmt.Printf("   Waiting 2 seconds to ensure cache is ready...\n")
	time.Sleep(2 * time.Second) // Small delay to ensure cache is processed

	// Use EXACT same context text and message structure as Turn 1
	// This ensures the prefix matches exactly for cache hit
	turn2Context := largeContext + "\n\nBased on this guide, what are the key principles for code quality?"

	// Send the EXACT same single-message structure as Turn 1
	// This is required for OpenRouter's exact prefix matching cache mechanism
	turn2Messages := []llmtypes.MessageContent{
		{
			Role: llmtypes.ChatMessageTypeHuman,
			Parts: []llmtypes.ContentPart{
				llmtypes.TextContent{Text: turn2Context},
			},
		},
	}

	startTime = time.Now()
	resp2, err := llm.GenerateContent(ctx, turn2Messages)
	duration2 := time.Since(startTime)

	if err != nil {
		fmt.Printf("❌ Turn 2 Error: %v\n", err)
		return
	}

	if resp2 == nil || resp2.Choices == nil || len(resp2.Choices) == 0 {
		fmt.Printf("❌ Turn 2: No response received\n")
		return
	}

	choice2 := resp2.Choices[0]
	fmt.Printf("✅ Turn 2 completed in %v\n", duration2)

	// Analyze token usage and cache information
	fmt.Printf("\n📊 Cache Token Analysis:\n")
	fmt.Printf("========================\n")

	// First, show Turn 2 basic token info
	if choice2.GenerationInfo != nil {
		input2 := 0
		output2 := 0
		if choice2.GenerationInfo.InputTokens != nil {
			input2 = *choice2.GenerationInfo.InputTokens
		}
		if choice2.GenerationInfo.OutputTokens != nil {
			output2 = *choice2.GenerationInfo.OutputTokens
		}
		fmt.Printf("   Turn 2 Tokens - Input: %d, Output: %d\n", input2, output2)
	}

	if choice2.GenerationInfo == nil {
		fmt.Printf("❌ No GenerationInfo found in Turn 2 response\n")
		return
	}

	info := choice2.GenerationInfo
	foundCacheInfo := false

	// Debug: Print raw Usage values if available
	fmt.Printf("\n🔍 Debug: Checking raw response structure...\n")
	if info.Additional != nil {
		if rawRead, ok := info.Additional["_debug_cache_read_raw"]; ok {
			fmt.Printf("   Raw CacheReadInputTokens from API: %v\n", rawRead)
		}
		if rawCreate, ok := info.Additional["_debug_cache_creation_raw"]; ok {
			fmt.Printf("   Raw CacheCreationInputTokens from API: %v\n", rawCreate)
		}
	}

	// Check for cached content tokens (primary field)
	if info.CachedContentTokens != nil {
		cachedTokens := *info.CachedContentTokens
		fmt.Printf("✅ Cached Content Tokens: %d\n", cachedTokens)
		foundCacheInfo = true

		// Calculate cache percentage if we have input tokens
		if info.InputTokens != nil {
			inputTokens := *info.InputTokens
			if inputTokens > 0 {
				cachePercentage := float64(cachedTokens) / float64(inputTokens) * 100
				fmt.Printf("   Cache Hit Rate: %.2f%% (%d of %d input tokens were cached)\n", cachePercentage, cachedTokens, inputTokens)

				// Calculate actual non-cached tokens
				nonCachedTokens := inputTokens - cachedTokens
				fmt.Printf("   Non-cached tokens: %d\n", nonCachedTokens)
			}
		}
	} else {
		fmt.Printf("⚠️  CachedContentTokens field is nil\n")
	}

	// Check for cache discount
	if info.CacheDiscount != nil {
		discount := *info.CacheDiscount
		fmt.Printf("✅ Cache Discount: %.4f (%.2f%%)\n", discount, discount*100)
		foundCacheInfo = true
	}

	// Check Additional map for cache-related fields
	if info.Additional != nil {
		fmt.Printf("   Checking Additional map for cache fields...\n")
		cacheFields := map[string]string{
			"cache_tokens":                "Cache tokens",
			"cache_read_tokens":           "Cache read tokens",
			"cache_write_tokens":          "Cache write tokens",
			"cache_discount":              "Cache discount",
			"cache_read_cost":             "Cache read cost",
			"cache_write_cost":            "Cache write cost",
			"CacheReadInputTokens":        "Cache read input tokens (Anthropic)",
			"CacheCreationInputTokens":    "Cache creation input tokens (Anthropic)",
			"cache_read_input_tokens":     "Cache read input tokens (lowercase)",
			"cache_creation_input_tokens": "Cache creation input tokens (lowercase)",
		}

		foundInAdditional := false
		for field, label := range cacheFields {
			if value, ok := info.Additional[field]; ok {
				fmt.Printf("✅ %s: %v\n", label, value)
				foundCacheInfo = true
				foundInAdditional = true
			}
		}
		if !foundInAdditional {
			fmt.Printf("   ⚠️  No cache-related fields found in Additional map\n")
			fmt.Printf("   Available Additional fields: ")
			fieldCount := 0
			for key := range info.Additional {
				if fieldCount > 0 {
					fmt.Printf(", ")
				}
				fmt.Printf("%s", key)
				fieldCount++
				if fieldCount >= 10 { // Limit output
					fmt.Printf("...")
					break
				}
			}
			fmt.Printf("\n")
		}
	} else {
		fmt.Printf("⚠️  Additional map is nil\n")
	}

	// Display full token breakdown (prefer unified Usage field)
	fmt.Printf("\n📊 Full Token Breakdown (Turn 2):\n")
	fmt.Printf("=================================\n")
	// Note: resp2 is not available in this function scope, so we check GenerationInfo
	// The calling function should display Usage if available
	if info.InputTokens != nil {
		fmt.Printf("   Input tokens: %d\n", *info.InputTokens)
	}
	if info.OutputTokens != nil {
		fmt.Printf("   Output tokens: %d\n", *info.OutputTokens)
	}
	if info.TotalTokens != nil {
		fmt.Printf("   Total tokens: %d\n", *info.TotalTokens)
	}

	// Compare Turn 1 vs Turn 2
	if choice1.GenerationInfo != nil && choice2.GenerationInfo != nil {
		fmt.Printf("\n📊 Comparison: Turn 1 vs Turn 2\n")
		fmt.Printf("===============================\n")

		input1 := 0
		input2 := 0
		if choice1.GenerationInfo.InputTokens != nil {
			input1 = *choice1.GenerationInfo.InputTokens
		}
		if choice2.GenerationInfo.InputTokens != nil {
			input2 = *choice2.GenerationInfo.InputTokens
		}

		if input1 > 0 && input2 > 0 {
			savings := input1 - input2
			if savings > 0 {
				savingsPercent := float64(savings) / float64(input1) * 100
				fmt.Printf("   Turn 1 Input: %d tokens\n", input1)
				fmt.Printf("   Turn 2 Input: %d tokens\n", input2)
				fmt.Printf("   💰 Token Savings: %d tokens (%.2f%% reduction)\n", savings, savingsPercent)
			} else if foundCacheInfo {
				fmt.Printf("   Turn 1 Input: %d tokens\n", input1)
				fmt.Printf("   Turn 2 Input: %d tokens\n", input2)
				fmt.Printf("   ℹ️  Cache detected but input tokens similar (may include cached tokens in count)\n")
			}
		}
	}

	if !foundCacheInfo {
		fmt.Printf("⚠️  No cache token information found in GenerationInfo\n")
		fmt.Printf("\n   Provider-specific caching requirements:\n")
		fmt.Printf("   - Anthropic: Requires explicit Cache API usage (create cache, then reference it)\n")
		fmt.Printf("     Current test sends repeated context but doesn't use Cache API\n")
		fmt.Printf("     To test: Need to implement Cache API support in adapter\n")
		fmt.Printf("   - Vertex AI (Gemini): Automatic caching when context is repeated\n")
		fmt.Printf("     Should work with this test once OAuth2 auth is configured\n")
		fmt.Printf("   - OpenRouter: Requires EXACT prefix matching for cache hits\n")
		fmt.Printf("     Turn 2 now sends the exact same message structure as Turn 1\n")
		fmt.Printf("     Cache should work if provider supports it and content matches exactly\n")
		fmt.Printf("\n   This test framework is ready - cache tokens will be detected when:\n")
		fmt.Printf("   1. Provider supports caching\n")
		fmt.Printf("   2. Caching is properly configured/triggered\n")
		fmt.Printf("   3. Message structure matches exactly (for OpenRouter)\n")
		fmt.Printf("   4. Provider returns cache token information in response\n")
	} else {
		fmt.Printf("\n✅ Cache token information successfully extracted!\n")
		fmt.Printf("   The test is working correctly - cached tokens detected!\n")
	}

	// Show complete GenerationInfo for debugging
	fmt.Printf("\n🔍 Complete GenerationInfo (Turn 2):\n")
	fmt.Printf("===================================\n")
	if info != nil {
		fmt.Printf("   InputTokens: %v\n", info.InputTokens)
		fmt.Printf("   OutputTokens: %v\n", info.OutputTokens)
		fmt.Printf("   TotalTokens: %v\n", info.TotalTokens)
		fmt.Printf("   CachedContentTokens: %v\n", info.CachedContentTokens)
		fmt.Printf("   CacheDiscount: %v\n", info.CacheDiscount)
		if info.Additional != nil {
			for key, value := range info.Additional {
				fmt.Printf("   %s: %v (type: %T)\n", key, value, value)
			}
		}
	}
}
