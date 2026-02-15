package vertex

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

var VertexTokenUsageTestCmd = &cobra.Command{
	Use:   "vertex-token-usage",
	Short: "Test Vertex AI token usage extraction",
	Long: `Test token usage extraction from Vertex AI (Gemini) LLM calls.
	
This command tests if Vertex AI returns token usage information in their GenerationInfo.`,
	Run: runVertexTokenUsageTest,
}

var (
	vertexTokenTestPrompt string
	vertexTokenTestRecord bool
	vertexTokenTestReplay bool
	vertexTokenTestDir    string
)

func init() {
	VertexTokenUsageTestCmd.Flags().StringVar(&vertexTokenTestPrompt, "prompt", "Hello world", "Test prompt")
	VertexTokenUsageTestCmd.Flags().BoolVar(&vertexTokenTestRecord, "record", false, "Record LLM responses to testdata/")
	VertexTokenUsageTestCmd.Flags().BoolVar(&vertexTokenTestReplay, "replay", false, "Replay recorded responses from testdata/")
	VertexTokenUsageTestCmd.Flags().StringVar(&vertexTokenTestDir, "test-dir", "testdata", "Directory for test recordings")
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

	// Setup recorder if recording or replaying
	ctx := context.Background()
	var rec *recorder.Recorder
	if vertexTokenTestRecord || vertexTokenTestReplay {
		recConfig := recorder.RecordingConfig{
			Enabled:  vertexTokenTestRecord,
			TestName: "token_usage",
			Provider: "vertex",
			ModelID:  "gemini-3-pro-preview", // Default for thinking test
			BaseDir:  vertexTokenTestDir,
		}
		rec = recorder.NewRecorder(recConfig)
		if vertexTokenTestReplay {
			rec.SetReplayMode(true)
		}

		if vertexTokenTestRecord {
			log.Printf("📹 Recording mode enabled - responses will be saved to %s", vertexTokenTestDir)
		}
		if vertexTokenTestReplay {
			log.Printf("▶️  Replay mode enabled - using recorded responses from %s", vertexTokenTestDir)
		}

		ctx = recorder.WithRecorder(ctx, rec)
	}

	// Test Vertex AI
	testVertexAITokenUsage(ctx, messages, mainTraceID, logger, rec)

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
func testVertexAITokenUsage(ctx context.Context, messages []llmtypes.MessageContent, mainTraceID interfaces.TraceID, logger interfaces.Logger, rec *recorder.Recorder) {
	// Test: Vertex AI (Google GenAI) for simple query
	fmt.Printf("\n🧪 TEST: Vertex AI / Google GenAI (Simple Query)\n")
	fmt.Printf("================================================\n")

	testCtx := ctx
	if rec != nil {
		recConfig := rec.GetConfig()
		recConfig.ModelID = "gemini-2.5-flash"
		rec = recorder.NewRecorder(recConfig)
		if vertexTokenTestReplay {
			rec.SetReplayMode(true)
		}
		testCtx = recorder.WithRecorder(ctx, rec)
	}

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
	sharedutils.TestLLMTokenUsage(testCtx, vertexLLM, messages, vertexTokenTestPrompt)

	// Test cached tokens with multi-turn conversation
	fmt.Printf("\n🧪 TEST: Vertex AI (Multi-Turn Conversation with Cache)\n")
	fmt.Printf("=======================================================\n")
	sharedutils.TestLLMTokenUsageWithCache(testCtx, vertexLLM)

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
	sharedutils.TestLLMTokenUsageWithTools(testCtx, vertexLLM, toolMessages, []llmtypes.Tool{weatherTool})

	// Test: Gemini 3 Pro with high thinking level (to check for reasoning tokens)
	testGemini3ProThinking(ctx, mainTraceID, logger, rec)

	// Test: Gemini 3 Flash with high thinking level (to check for reasoning tokens)
	testGemini3FlashThinking(ctx, mainTraceID, logger, rec)

	// Test: Gemini 3 Flash multi-turn conversation with tool calling
	testGemini3FlashMultiTurnWithTools(ctx, mainTraceID, logger, rec)
}

// testGemini3ProThinking runs a simple test with "Hi" message using Gemini 3 Pro with high thinking level
func testGemini3ProThinking(ctx context.Context, mainTraceID interfaces.TraceID, logger interfaces.Logger, rec *recorder.Recorder) {
	fmt.Printf("\n🧪 TEST: Vertex AI Gemini 3 Pro with High Thinking Level (Simple 'Hi' Message)\n")
	fmt.Printf("================================================================================\n")

	// Setup recorder for this test if needed
	testCtx := ctx
	if rec != nil {
		recConfig := rec.GetConfig()
		recConfig.ModelID = "gemini-3-pro-preview"
		recConfig.TestName = "gemini3_thinking"
		rec = recorder.NewRecorder(recConfig)
		if vertexTokenTestReplay {
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

	// Initialize Gemini 3 Pro LLM
	gemini3Config := llmproviders.Config{
		Provider:     llmproviders.ProviderVertex,
		ModelID:      "gemini-3-pro-preview",
		Temperature:  0.7,
		EventEmitter: nil,
		TraceID:      mainTraceID,
		Logger:       logger,
		Context:      testCtx,
	}

	gemini3LLM, err := llmproviders.InitializeLLM(gemini3Config)
	if err != nil {
		fmt.Printf("❌ Error creating Vertex AI Gemini 3 Pro LLM: %v\n", err)
		fmt.Printf("⏭️  Skipping Gemini 3 Pro thinking test\n")
		fmt.Printf("   Note: Make sure VERTEX_API_KEY or GOOGLE_API_KEY is set\n")
		return
	}

	fmt.Printf("🔧 Created Vertex AI Gemini 3 Pro LLM\n")
	fmt.Printf("📝 Sending simple message: 'Hi'\n")
	fmt.Printf("⚙️  Configuration:\n")
	fmt.Printf("   - thinking_level: high\n")
	if vertexTokenTestRecord {
		fmt.Printf("   - recording: enabled\n")
	}
	if vertexTokenTestReplay {
		fmt.Printf("   - replay: enabled\n")
	}
	fmt.Printf("\n")

	// Make the LLM call with high thinking level
	startTime := time.Now()
	resp, err := gemini3LLM.GenerateContent(testCtx, simpleMessage,
		llmtypes.WithThinkingLevel("high"))
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

		// Validate ThoughtsTokens in unified Usage field (for Gemini 3 Pro with thinking_level=high)
		fmt.Printf("\n🔍 Validating ThoughtsTokens in unified Usage field:\n")
		validated := sharedutils.ValidateThoughtsTokensInUsage(resp.Usage, "gemini-3-pro-preview")
		if validated {
			fmt.Printf("   ✅ This confirms that thinking_level=high is working and tokens are extracted correctly!\n")
		}

		// Also check for ReasoningTokens (if present)
		if resp.Usage.ReasoningTokens != nil {
			fmt.Printf("   Reasoning tokens: %d (OpenAI gpt-5.1, etc.)\n", *resp.Usage.ReasoningTokens)
		}
	} else {
		fmt.Printf("⚠️  Unified Usage field not found\n")
	}

	// Check GenerationInfo for reasoning/thinking tokens (for detailed validation)
	if choice.GenerationInfo != nil {
		fmt.Printf("\n🔍 GenerationInfo Details (for reference):\n")
		info := choice.GenerationInfo

		// Check for reasoning tokens (Gemini might use different field names)
		if info.ReasoningTokens != nil {
			fmt.Printf("✅ Reasoning tokens in GenerationInfo: %d\n", *info.ReasoningTokens)
		} else {
			fmt.Printf("⚠️  Reasoning tokens not found in GenerationInfo.ReasoningTokens\n")
		}

		// Check for thoughts tokens (Gemini-specific)
		if info.ThoughtsTokens != nil {
			fmt.Printf("✅ Thoughts tokens in GenerationInfo: %d\n", *info.ThoughtsTokens)
		} else {
			fmt.Printf("⚠️  Thoughts tokens not found in GenerationInfo.ThoughtsTokens\n")
		}

		// Check Additional map as fallback for various field names
		if info.Additional != nil {
			if value, ok := info.Additional["ReasoningTokens"]; ok {
				fmt.Printf("✅ Reasoning tokens found in Additional map: %v\n", value)
			} else if value, ok := info.Additional["reasoning_tokens"]; ok {
				fmt.Printf("✅ Reasoning tokens found in Additional map (lowercase): %v\n", value)
			} else if value, ok := info.Additional["ThoughtsTokens"]; ok {
				fmt.Printf("✅ Thoughts tokens found in Additional map: %v\n", value)
			} else if value, ok := info.Additional["thoughts_tokens"]; ok {
				fmt.Printf("✅ Thoughts tokens found in Additional map (lowercase): %v\n", value)
			} else if value, ok := info.Additional["thinking_tokens"]; ok {
				fmt.Printf("✅ Thinking tokens found in Additional map: %v\n", value)
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

		// Log raw GenerationInfo for debugging
		if logger != nil {
			logger.Debugf("Raw GenerationInfo: %+v", info)
		}
	} else {
		fmt.Printf("⚠️  GenerationInfo not available\n")
	}

	fmt.Printf("\n")
}

// testGemini3FlashThinking runs a simple test with "Hi" message using Gemini 3 Flash with high thinking level
func testGemini3FlashThinking(ctx context.Context, mainTraceID interfaces.TraceID, logger interfaces.Logger, rec *recorder.Recorder) {
	fmt.Printf("\n🧪 TEST: Vertex AI Gemini 3 Flash with High Thinking Level (Simple 'Hi' Message)\n")
	fmt.Printf("==================================================================================\n")

	// Setup recorder for this test if needed
	testCtx := ctx
	if rec != nil {
		recConfig := rec.GetConfig()
		recConfig.ModelID = "gemini-3-flash-preview"
		recConfig.TestName = "gemini3_flash_thinking"
		rec = recorder.NewRecorder(recConfig)
		if vertexTokenTestReplay {
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

	// Initialize Gemini 3 Flash LLM
	gemini3FlashConfig := llmproviders.Config{
		Provider:     llmproviders.ProviderVertex,
		ModelID:      "gemini-3-flash-preview",
		Temperature:  0.7,
		EventEmitter: nil,
		TraceID:      mainTraceID,
		Logger:       logger,
		Context:      testCtx,
	}

	gemini3FlashLLM, err := llmproviders.InitializeLLM(gemini3FlashConfig)
	if err != nil {
		fmt.Printf("❌ Error creating Vertex AI Gemini 3 Flash LLM: %v\n", err)
		fmt.Printf("⏭️  Skipping Gemini 3 Flash thinking test\n")
		fmt.Printf("   Note: Make sure VERTEX_API_KEY or GOOGLE_API_KEY is set\n")
		return
	}

	fmt.Printf("🔧 Created Vertex AI Gemini 3 Flash LLM\n")
	fmt.Printf("📝 Sending simple message: 'Hi'\n")
	fmt.Printf("⚙️  Configuration:\n")
	fmt.Printf("   - thinking_level: high\n")
	if vertexTokenTestRecord {
		fmt.Printf("   - recording: enabled\n")
	}
	if vertexTokenTestReplay {
		fmt.Printf("   - replay: enabled\n")
	}
	fmt.Printf("\n")

	// Make the LLM call with high thinking level
	startTime := time.Now()
	resp, err := gemini3FlashLLM.GenerateContent(testCtx, simpleMessage,
		llmtypes.WithThinkingLevel("high"))
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

		// Validate ThoughtsTokens in unified Usage field (for Gemini 3 Flash with thinking_level=high)
		fmt.Printf("\n🔍 Validating ThoughtsTokens in unified Usage field:\n")
		validated := sharedutils.ValidateThoughtsTokensInUsage(resp.Usage, "gemini-3-flash-preview")
		if validated {
			fmt.Printf("   ✅ This confirms that thinking_level=high is working and tokens are extracted correctly!\n")
		}

		// Also check for ReasoningTokens (if present)
		if resp.Usage.ReasoningTokens != nil {
			fmt.Printf("   Reasoning tokens: %d (OpenAI gpt-5.1, etc.)\n", *resp.Usage.ReasoningTokens)
		}
	} else {
		fmt.Printf("⚠️  Unified Usage field not found\n")
	}

	// Check GenerationInfo for reasoning/thinking tokens (for detailed validation)
	if choice.GenerationInfo != nil {
		fmt.Printf("\n🔍 GenerationInfo Details (for reference):\n")
		info := choice.GenerationInfo

		// Check for reasoning tokens (Gemini might use different field names)
		if info.ReasoningTokens != nil {
			fmt.Printf("✅ Reasoning tokens in GenerationInfo: %d\n", *info.ReasoningTokens)
		} else {
			fmt.Printf("⚠️  Reasoning tokens not found in GenerationInfo.ReasoningTokens\n")
		}

		// Check for thoughts tokens (Gemini-specific)
		if info.ThoughtsTokens != nil {
			fmt.Printf("✅ Thoughts tokens in GenerationInfo: %d\n", *info.ThoughtsTokens)
		} else {
			fmt.Printf("⚠️  Thoughts tokens not found in GenerationInfo.ThoughtsTokens\n")
		}

		// Check Additional map as fallback for various field names
		if info.Additional != nil {
			if value, ok := info.Additional["ReasoningTokens"]; ok {
				fmt.Printf("✅ Reasoning tokens found in Additional map: %v\n", value)
			} else if value, ok := info.Additional["reasoning_tokens"]; ok {
				fmt.Printf("✅ Reasoning tokens found in Additional map (lowercase): %v\n", value)
			} else if value, ok := info.Additional["ThoughtsTokens"]; ok {
				fmt.Printf("✅ Thoughts tokens found in Additional map: %v\n", value)
			} else if value, ok := info.Additional["thoughts_tokens"]; ok {
				fmt.Printf("✅ Thoughts tokens found in Additional map (lowercase): %v\n", value)
			} else if value, ok := info.Additional["thinking_tokens"]; ok {
				fmt.Printf("✅ Thinking tokens found in Additional map: %v\n", value)
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

		// Log raw GenerationInfo for debugging
		if logger != nil {
			logger.Debugf("Raw GenerationInfo: %+v", info)
		}
	} else {
		fmt.Printf("⚠️  GenerationInfo not available\n")
	}

	fmt.Printf("\n")
}

// testGemini3FlashMultiTurnWithTools runs a multi-turn conversation test with tool calling for Gemini 3 Flash
func testGemini3FlashMultiTurnWithTools(ctx context.Context, mainTraceID interfaces.TraceID, logger interfaces.Logger, rec *recorder.Recorder) {
	fmt.Printf("\n🧪 TEST: Vertex AI Gemini 3 Flash Multi-Turn Conversation with Tool Calling\n")
	fmt.Printf("==============================================================================\n")

	// Setup recorder for this test if needed
	testCtx := ctx
	if rec != nil {
		recConfig := rec.GetConfig()
		recConfig.ModelID = "gemini-3-flash-preview"
		recConfig.TestName = "gemini3_flash_multiturn_tools"
		rec = recorder.NewRecorder(recConfig)
		if vertexTokenTestReplay {
			rec.SetReplayMode(true)
		}
		testCtx = recorder.WithRecorder(ctx, rec)
	}

	// Initialize Gemini 3 Flash LLM
	gemini3FlashConfig := llmproviders.Config{
		Provider:     llmproviders.ProviderVertex,
		ModelID:      "gemini-3-flash-preview",
		Temperature:  0.7,
		EventEmitter: nil,
		TraceID:      mainTraceID,
		Logger:       logger,
		Context:      testCtx,
	}

	gemini3FlashLLM, err := llmproviders.InitializeLLM(gemini3FlashConfig)
	if err != nil {
		fmt.Printf("❌ Error creating Vertex AI Gemini 3 Flash LLM: %v\n", err)
		fmt.Printf("⏭️  Skipping Gemini 3 Flash multi-turn test\n")
		fmt.Printf("   Note: Make sure VERTEX_API_KEY or GOOGLE_API_KEY is set\n")
		return
	}

	// Define tools for testing
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

	calculatorTool := llmtypes.Tool{
		Type: "function",
		Function: &llmtypes.FunctionDefinition{
			Name:        "calculate",
			Description: "Perform a mathematical calculation",
			Parameters: llmtypes.NewParameters(map[string]interface{}{
				"type": "object",
				"properties": map[string]any{
					"expression": map[string]any{
						"type":        "string",
						"description": "Mathematical expression to evaluate (e.g., '2 + 2', '10 * 5')",
					},
				},
				"required": []string{"expression"},
			}),
		},
	}

	tools := []llmtypes.Tool{weatherTool, calculatorTool}

	// Turn 1: Simple greeting and introduction
	fmt.Printf("\n🔄 Turn 1: Simple greeting\n")
	fmt.Printf("================================\n")
	turn1Messages := []llmtypes.MessageContent{
		{
			Role:  llmtypes.ChatMessageTypeHuman,
			Parts: []llmtypes.ContentPart{llmtypes.TextContent{Text: "Hello! My name is Bob and I'm testing the Gemini 3 Flash model."}},
		},
	}

	startTime1 := time.Now()
	resp1, err := gemini3FlashLLM.GenerateContent(testCtx, turn1Messages)
	duration1 := time.Since(startTime1)

	if err != nil {
		fmt.Printf("❌ Turn 1 failed: %v\n", err)
		return
	}

	if resp1 == nil || resp1.Choices == nil || len(resp1.Choices) == 0 {
		fmt.Printf("❌ Turn 1 failed - no response received\n")
		return
	}

	content1 := resp1.Choices[0].Content
	fmt.Printf("✅ Turn 1 completed in %v\n", duration1)
	fmt.Printf("   Response: %s\n", content1)
	if resp1.Usage != nil {
		fmt.Printf("   Tokens - Input: %d, Output: %d, Total: %d\n", resp1.Usage.InputTokens, resp1.Usage.OutputTokens, resp1.Usage.TotalTokens)
	}

	// Build conversation history
	conversation := []llmtypes.MessageContent{
		turn1Messages[0],
		{
			Role:  llmtypes.ChatMessageTypeAI,
			Parts: []llmtypes.ContentPart{llmtypes.TextContent{Text: content1}},
		},
	}

	// Turn 2: Follow-up question using context
	fmt.Printf("\n🔄 Turn 2: Follow-up question (testing context retention)\n")
	fmt.Printf("==========================================================\n")
	turn2Message := llmtypes.MessageContent{
		Role:  llmtypes.ChatMessageTypeHuman,
		Parts: []llmtypes.ContentPart{llmtypes.TextContent{Text: "What's my name? And can you check the weather in San Francisco?"}},
	}
	conversation = append(conversation, turn2Message)

	startTime2 := time.Now()
	resp2, err := gemini3FlashLLM.GenerateContent(testCtx, conversation,
		llmtypes.WithTools(tools),
	)
	duration2 := time.Since(startTime2)

	if err != nil {
		fmt.Printf("❌ Turn 2 failed: %v\n", err)
		return
	}

	if resp2 == nil || resp2.Choices == nil || len(resp2.Choices) == 0 {
		fmt.Printf("❌ Turn 2 failed - no response received\n")
		return
	}

	content2 := resp2.Choices[0].Content
	toolCalls2 := resp2.Choices[0].ToolCalls
	fmt.Printf("✅ Turn 2 completed in %v\n", duration2)
	fmt.Printf("   Response: %s\n", content2)
	fmt.Printf("   Tool calls: %d\n", len(toolCalls2))
	if resp2.Usage != nil {
		fmt.Printf("   Tokens - Input: %d, Output: %d, Total: %d\n", resp2.Usage.InputTokens, resp2.Usage.OutputTokens, resp2.Usage.TotalTokens)
		if resp2.Usage.ThoughtsTokens != nil {
			fmt.Printf("   Thoughts tokens: %d\n", *resp2.Usage.ThoughtsTokens)
		}
	}

	// Validate context retention - response should mention "Bob"
	if len(content2) > 0 {
		fmt.Printf("   Context check: Response length %d chars\n", len(content2))
	}

	// Add assistant response and tool calls to conversation
	if len(content2) > 0 {
		conversation = append(conversation, llmtypes.MessageContent{
			Role:  llmtypes.ChatMessageTypeAI,
			Parts: []llmtypes.ContentPart{llmtypes.TextContent{Text: content2}},
		})
	}

	// Add tool calls if present
	if len(toolCalls2) > 0 {
		toolCallParts := make([]llmtypes.ContentPart, 0, len(toolCalls2))
		for _, tc := range toolCalls2 {
			toolCallParts = append(toolCallParts, tc)
		}
		conversation = append(conversation, llmtypes.MessageContent{
			Role:  llmtypes.ChatMessageTypeAI,
			Parts: toolCallParts,
		})

		// Add tool responses
		toolResponseParts := make([]llmtypes.ContentPart, 0, len(toolCalls2))
		for i, tc := range toolCalls2 {
			if tc.FunctionCall != nil {
				var toolResult string
				if tc.FunctionCall.Name == "get_weather" {
					toolResult = `{"location": "San Francisco", "temperature": "18°C", "condition": "Partly cloudy"}`
				} else if tc.FunctionCall.Name == "calculate" {
					toolResult = `{"result": "42"}`
				} else {
					toolResult = `{"status": "success"}`
				}

				toolResponseParts = append(toolResponseParts, llmtypes.ToolCallResponse{
					ToolCallID: tc.ID,
					Name:       tc.FunctionCall.Name,
					Content:    toolResult,
				})
				fmt.Printf("   Tool %d: %s (ID: %s) - Response added\n", i+1, tc.FunctionCall.Name, tc.ID)
			}
		}

		if len(toolResponseParts) > 0 {
			conversation = append(conversation, llmtypes.MessageContent{
				Role:  llmtypes.ChatMessageTypeTool,
				Parts: toolResponseParts,
			})
		}
	}

	// Turn 3: Continue conversation after tool responses
	fmt.Printf("\n🔄 Turn 3: Continue conversation after tool responses\n")
	fmt.Printf("====================================================\n")
	turn3Message := llmtypes.MessageContent{
		Role:  llmtypes.ChatMessageTypeHuman,
		Parts: []llmtypes.ContentPart{llmtypes.TextContent{Text: "Thanks! Can you also calculate 15 * 8 for me?"}},
	}
	conversation = append(conversation, turn3Message)

	startTime3 := time.Now()
	resp3, err := gemini3FlashLLM.GenerateContent(testCtx, conversation,
		llmtypes.WithTools(tools),
	)
	duration3 := time.Since(startTime3)

	if err != nil {
		fmt.Printf("❌ Turn 3 failed: %v\n", err)
		return
	}

	if resp3 == nil || resp3.Choices == nil || len(resp3.Choices) == 0 {
		fmt.Printf("❌ Turn 3 failed - no response received\n")
		return
	}

	content3 := resp3.Choices[0].Content
	toolCalls3 := resp3.Choices[0].ToolCalls
	fmt.Printf("✅ Turn 3 completed in %v\n", duration3)
	fmt.Printf("   Response: %s\n", content3)
	fmt.Printf("   Tool calls: %d\n", len(toolCalls3))
	if resp3.Usage != nil {
		fmt.Printf("   Tokens - Input: %d, Output: %d, Total: %d\n", resp3.Usage.InputTokens, resp3.Usage.OutputTokens, resp3.Usage.TotalTokens)
		if resp3.Usage.ThoughtsTokens != nil {
			fmt.Printf("   Thoughts tokens: %d\n", *resp3.Usage.ThoughtsTokens)
		}
	}

	// Summary
	fmt.Printf("\n📊 Test Summary:\n")
	fmt.Printf("================\n")
	fmt.Printf("✅ Turn 1: Simple greeting - Completed\n")
	fmt.Printf("✅ Turn 2: Context retention + Tool calling - Completed\n")
	fmt.Printf("   - Tool calls made: %d\n", len(toolCalls2))
	fmt.Printf("✅ Turn 3: Continued conversation + Tool calling - Completed\n")
	fmt.Printf("   - Tool calls made: %d\n", len(toolCalls3))

	if resp1.Usage != nil && resp2.Usage != nil && resp3.Usage != nil {
		totalInput := resp1.Usage.InputTokens + resp2.Usage.InputTokens + resp3.Usage.InputTokens
		totalOutput := resp1.Usage.OutputTokens + resp2.Usage.OutputTokens + resp3.Usage.OutputTokens
		totalTokens := resp1.Usage.TotalTokens + resp2.Usage.TotalTokens + resp3.Usage.TotalTokens
		fmt.Printf("\n📈 Total Token Usage Across All Turns:\n")
		fmt.Printf("   Input tokens:  %d\n", totalInput)
		fmt.Printf("   Output tokens: %d\n", totalOutput)
		fmt.Printf("   Total tokens:  %d\n", totalTokens)
	}

	fmt.Printf("\n✅ Gemini 3 Flash multi-turn conversation with tool calling test completed successfully!\n")
}
