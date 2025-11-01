package testing

import (
	"context"
	"fmt"
	"os"
	"time"

	"mcp-agent/agent_go/internal/llm"
	"mcp-agent/agent_go/internal/llmtypes"
	"mcp-agent/agent_go/internal/observability"

	"github.com/spf13/cobra"

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
  mcp-agent test token-usage                              # Test with default prompt
  mcp-agent test token-usage --prompt "Custom prompt"     # Test with custom prompt`,
	Run: runTokenUsageTest,
}

var (
	tokenTestPrompt string
)

func init() {
	TokenUsageTestCmd.Flags().StringVar(&tokenTestPrompt, "prompt", "Hello world", "Test prompt")
}

func runTokenUsageTest(cmd *cobra.Command, args []string) {
	// Load .env file for API keys
	_ = godotenv.Load(".env")

	fmt.Printf("🧪 Testing Token Usage Extraction from LangChain\n")
	fmt.Printf("================================================\n\n")

	// Initialize OpenAI LLM for testing
	fmt.Printf("🤖 Initializing OpenAI LLM for token usage testing...\n")

	// Test configuration
	fmt.Printf("🔧 Test Configuration:\n")
	fmt.Printf("   Provider: openai\n")
	fmt.Printf("   Prompt: %s\n\n", tokenTestPrompt)

	// Create simple message
	messages := []llmtypes.MessageContent{
		{
			Role:  llmtypes.ChatMessageTypeHuman,
			Parts: []llmtypes.ContentPart{llmtypes.TextContent{Text: tokenTestPrompt}},
		},
	}

	// Initialize logger and tracer for providers
	logger := GetTestLogger()

	// Set environment for Langfuse tracing
	os.Setenv("TRACING_PROVIDER", "langfuse")
	os.Setenv("LANGFUSE_DEBUG", "true")

	// Initialize tracer (simple noop tracer for token usage testing)
	tracer := observability.GetTracer("noop")

	// Start main trace for the entire token usage test
	mainTraceID := tracer.StartTrace("Token Usage Test: Multi-Provider Validation", map[string]interface{}{
		"test_type":   "token_usage_validation",
		"description": "Testing token usage extraction across OpenAI, Bedrock, Anthropic, OpenRouter, and Vertex AI (Google GenAI) providers",
		"timestamp":   time.Now().UTC(),
		"providers":   []string{"openai", "bedrock", "anthropic", "openrouter", "vertex"},
	})

	fmt.Printf("🔍 Started Langfuse trace: %s\n", mainTraceID)

	// Test 1: OpenAI gpt-4.1 for simple query
	fmt.Printf("\n🧪 TEST 1: OpenAI gpt-4.1 (Simple Query)\n")
	fmt.Printf("========================================\n")

	gpt41Config := llm.Config{
		Provider:    llm.ProviderOpenAI,
		ModelID:     "gpt-4.1-mini",
		Temperature: 0.7,
		Tracers:     nil,
		TraceID:     mainTraceID,
		Logger:      logger,
	}

	gpt41LLM, err := llm.InitializeLLM(gpt41Config)
	if err != nil {
		fmt.Printf("❌ Error creating OpenAI gpt-4.1-mini LLM: %v\n", err)
		fmt.Printf("⏭️  Skipping OpenAI gpt-4.1 test\n")
	} else {
		fmt.Printf("🔧 Created OpenAI gpt-4.1-mini LLM using providers.go\n")
		testLLMTokenUsage(gpt41LLM, messages)
	}

	// Test 2: OpenAI o3-mini for complex reasoning query
	fmt.Printf("\n🧪 TEST 2: OpenAI o3-mini (Complex Reasoning Query)\n")
	fmt.Printf("====================================================\n")

	o3Config := llm.Config{
		Provider:    llm.ProviderOpenAI,
		ModelID:     "gpt-4o-mini",
		Temperature: 0.7,
		Tracers:     nil,
		TraceID:     mainTraceID,
		Logger:      logger,
	}

	o3LLM, err := llm.InitializeLLM(o3Config)
	if err != nil {
		fmt.Printf("❌ Error creating OpenAI o3-mini LLM: %v\n", err)
		fmt.Printf("⏭️  Skipping OpenAI o3-mini test\n")
	} else {
		fmt.Printf("🔧 Created OpenAI o3-mini LLM using providers.go\n")

		complexPrompt := `Please analyze the following complex scenario step by step: A company has 3 warehouses in different cities. Warehouse A can ship 100 units per day, Warehouse B can ship 150 units per day, and Warehouse C can ship 200 units per day. They need to fulfill orders for 5 customers: Customer 1 needs 80 units, Customer 2 needs 120 units, Customer 3 needs 90 units, Customer 4 needs 110 units, and Customer 5 needs 140 units. The shipping costs from each warehouse to each customer vary. Please create an optimal shipping plan that minimizes total cost while meeting all customer demands. Show your mathematical reasoning, create a cost matrix, and solve this step by step.`

		complexMessages := []llmtypes.MessageContent{
			{
				Role:  llmtypes.ChatMessageTypeHuman,
				Parts: []llmtypes.ContentPart{llmtypes.TextContent{Text: complexPrompt}},
			},
		}

		testLLMTokenUsage(o3LLM, complexMessages)
	}

	// Test 3: Bedrock Claude for simple query
	fmt.Printf("\n🧪 TEST 3: Bedrock Claude (Simple Query)\n")
	fmt.Printf("========================================\n")

	bedrockConfig := llm.Config{
		Provider:    llm.ProviderBedrock,
		ModelID:     "global.anthropic.claude-sonnet-4-5-20250929-v1:0",
		Temperature: 0.7,
		Tracers:     nil,
		TraceID:     mainTraceID,
		Logger:      logger,
	}

	bedrockLLM, err := llm.InitializeLLM(bedrockConfig)
	if err != nil {
		fmt.Printf("❌ Error creating Bedrock Claude LLM: %v\n", err)
		fmt.Printf("⏭️  Skipping Bedrock test\n")
	} else {
		fmt.Printf("🔧 Created Bedrock Claude LLM using providers.go\n")
		testLLMTokenUsage(bedrockLLM, messages)
	}

	// Test 4: Anthropic direct API for simple query
	fmt.Printf("\n🧪 TEST 4: Anthropic Direct API (Simple Query)\n")
	fmt.Printf("==============================================\n")

	anthropicConfig := llm.Config{
		Provider:    llm.ProviderAnthropic,
		ModelID:     "claude-3-5-sonnet-20241022",
		Temperature: 0.7,
		Tracers:     nil,
		TraceID:     mainTraceID,
		Logger:      logger,
	}

	anthropicLLM, err := llm.InitializeLLM(anthropicConfig)
	if err != nil {
		fmt.Printf("❌ Error creating Anthropic Claude LLM: %v\n", err)
		fmt.Printf("⏭️  Skipping Anthropic test\n")
		fmt.Printf("   Note: Make sure ANTHROPIC_API_KEY is set\n")
	} else {
		fmt.Printf("🔧 Created Anthropic Claude LLM using providers.go (Anthropic SDK)\n")
		testLLMTokenUsage(anthropicLLM, messages)
	}

	// Test 4b: Anthropic direct API for tool calling with token usage
	fmt.Printf("\n🧪 TEST 4b: Anthropic Direct API (Tool Calling with Token Usage)\n")
	fmt.Printf("=================================================================\n")

	if anthropicLLM != nil {
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
		testLLMTokenUsageWithTools(anthropicLLM, toolMessages, []llmtypes.Tool{weatherTool})
	} else {
		fmt.Printf("⏭️  Skipping Anthropic tool calling test (LLM not initialized)\n")
	}

	// Test 5: OpenRouter for simple query
	fmt.Printf("\n🧪 TEST 5: OpenRouter (Simple Query)\n")
	fmt.Printf("====================================\n")

	openrouterConfig := llm.Config{
		Provider:    llm.ProviderOpenRouter,
		ModelID:     "moonshotai/kimi-k2",
		Temperature: 0.7,
		Tracers:     nil,
		TraceID:     mainTraceID,
		Logger:      logger,
	}

	openrouterLLM, err := llm.InitializeLLM(openrouterConfig)
	if err != nil {
		fmt.Printf("❌ Error creating OpenRouter LLM: %v\n", err)
		fmt.Printf("⏭️  Skipping OpenRouter test\n")
	} else {
		fmt.Printf("🔧 Created OpenRouter LLM using providers.go\n")
		testLLMTokenUsage(openrouterLLM, messages)
	}

	// Test 6: Vertex AI (Google GenAI) for simple query
	fmt.Printf("\n🧪 TEST 6: Vertex AI / Google GenAI (Simple Query)\n")
	fmt.Printf("==================================================\n")

	vertexConfig := llm.Config{
		Provider:    llm.ProviderVertex,
		ModelID:     "gemini-2.5-flash",
		Temperature: 0.7,
		Tracers:     nil,
		TraceID:     mainTraceID,
		Logger:      logger,
		Context:     context.Background(),
	}

	vertexLLM, err := llm.InitializeLLM(vertexConfig)
	if err != nil {
		fmt.Printf("❌ Error creating Vertex AI LLM: %v\n", err)
		fmt.Printf("⏭️  Skipping Vertex AI test\n")
		fmt.Printf("   Note: Make sure VERTEX_API_KEY or GOOGLE_API_KEY is set\n")
	} else {
		fmt.Printf("🔧 Created Vertex AI LLM using providers.go (Google GenAI SDK)\n")
		testLLMTokenUsage(vertexLLM, messages)
	}

	// Test 7: Vertex AI (Google GenAI) for tool calling with token usage
	fmt.Printf("\n🧪 TEST 7: Vertex AI / Google GenAI (Tool Calling with Token Usage)\n")
	fmt.Printf("=====================================================================\n")

	if vertexLLM != nil {
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
		testLLMTokenUsageWithTools(vertexLLM, toolMessages, []llmtypes.Tool{weatherTool})
	} else {
		fmt.Printf("⏭️  Skipping Vertex AI tool calling test (LLM not initialized)\n")
	}

	// End main trace with summary
	tracer.EndTrace(mainTraceID, map[string]interface{}{
		"final_status":     "completed",
		"success":          true,
		"test_type":        "token_usage_validation",
		"providers_tested": []string{"openai", "bedrock", "anthropic", "openrouter", "vertex"},
		"timestamp":        time.Now().UTC(),
	})

	fmt.Printf("\n🎉 Token Usage Test Complete!\n")
	fmt.Printf("🔍 Check Langfuse for trace: %s\n", mainTraceID)
}

func testLLMTokenUsage(llm llmtypes.Model, messages []llmtypes.MessageContent) {
	ctx := context.Background()
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

	// Check for token usage information
	fmt.Printf("🔍 Token Usage Analysis:\n")
	fmt.Printf("========================\n")

	if choice.GenerationInfo == nil {
		fmt.Printf("❌ No GenerationInfo found in response\n")
		fmt.Printf("   This means LangChain is not providing token usage data\n")
		fmt.Printf("   Token usage will need to be estimated\n")
		return
	}

	fmt.Printf("✅ GenerationInfo found! Checking for token data...\n\n")

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

	foundTokens := false
	info := choice.GenerationInfo
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

	if !foundTokens {
		fmt.Printf("❌ No standard token fields found in GenerationInfo\n")
		fmt.Printf("   GenerationInfo: %+v\n", info)
		fmt.Printf("\n   This suggests the LLM provider doesn't return token usage\n")
	} else {
		fmt.Printf("\n✅ Token usage data is available from LangChain!\n")
		fmt.Printf("   This means proper cost tracking and observability will work\n")
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
func testLLMTokenUsageWithTools(llm llmtypes.Model, messages []llmtypes.MessageContent, tools []llmtypes.Tool) {
	ctx := context.Background()
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

	// Check for token usage information
	fmt.Printf("🔍 Token Usage Analysis (with tools):\n")
	fmt.Printf("======================================\n")

	if choice.GenerationInfo == nil {
		fmt.Printf("❌ No GenerationInfo found in response\n")
		fmt.Printf("   Token usage extraction failed\n")
		return
	}

	fmt.Printf("✅ GenerationInfo found! Checking for token data...\n\n")

	// Check for specific token fields (Google GenAI uses these field names)
	tokenFields := map[string]string{
		"input_tokens":  "Input tokens",
		"output_tokens": "Output tokens",
		"total_tokens":  "Total tokens",
	}

	foundTokens := false
	var inputTokens, outputTokens, totalTokens interface{}
	info := choice.GenerationInfo

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

	if !foundTokens {
		fmt.Printf("❌ No standard token fields found in GenerationInfo\n")
		fmt.Printf("   GenerationInfo: %+v\n", info)
		fmt.Printf("\n   This suggests the adapter is not extracting token usage correctly\n")
	} else {
		fmt.Printf("\n✅ Token usage data extracted successfully!\n")

		// Validate token counts make sense
		if inputTokens != nil && outputTokens != nil && totalTokens != nil {
			inputVal := extractIntValue(inputTokens)
			outputVal := extractIntValue(outputTokens)
			totalVal := extractIntValue(totalTokens)

			fmt.Printf("\n🔍 Token Usage Validation:\n")
			fmt.Printf("   Input tokens: %d\n", inputVal)
			fmt.Printf("   Output tokens: %d\n", outputVal)
			fmt.Printf("   Total tokens: %d\n", totalVal)

			// Check if total matches sum (allowing for slight discrepancies)
			calculatedTotal := inputVal + outputVal
			if totalVal > 0 {
				diff := totalVal - calculatedTotal
				if diff < 0 {
					diff = -diff
				}
				if totalVal == calculatedTotal {
					fmt.Printf("   ✅ Total tokens matches input + output\n")
				} else if diff <= 2 {
					fmt.Printf("   ⚠️  Total tokens differs from input+output by %d (acceptable)\n", diff)
				} else {
					fmt.Printf("   ⚠️  Total tokens (%d) differs significantly from input+output (%d)\n", totalVal, calculatedTotal)
				}
			}

			// Check for reasonable token counts
			if inputVal > 0 && outputVal >= 0 {
				fmt.Printf("   ✅ Token counts are reasonable\n")
			} else {
				fmt.Printf("   ⚠️  Unusual token counts detected\n")
			}
		}
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

// extractIntValue safely extracts an integer value from interface{}
func extractIntValue(v interface{}) int {
	switch val := v.(type) {
	case int:
		return val
	case int32:
		return int(val)
	case int64:
		return int(val)
	case float32:
		return int(val)
	case float64:
		return int(val)
	default:
		return 0
	}
}
