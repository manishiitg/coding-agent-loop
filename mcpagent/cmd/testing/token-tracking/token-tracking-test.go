package tokentracking

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	testutils "github.com/manishiitg/mcpagent/cmd/testing/testutils"
	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"

	"github.com/manishiitg/multi-llm-provider-go/pkg/adapters/openai"
)

var tokenTrackingTestCmd = &cobra.Command{
	Use:   "token-tracking",
	Short: "Test token usage tracking (cumulative token accumulation)",
	Long: `Test the agent's token usage tracking feature.

This test:
1. Makes multiple agent calls to verify cumulative token accumulation
2. Verifies GetTokenUsage() returns correct metrics after each call
3. Tests cache tokens tracking (if supported by provider)
4. Tests reasoning tokens tracking (if supported by model)
5. Verifies LLM call count and cache-enabled call count
6. Tests multi-turn conversation token accumulation

Note: This test doesn't use traditional asserts. Logs are analyzed (manually or by LLM) to verify success.
See criteria.md in the token-tracking folder for detailed log analysis criteria.

Examples:
  mcpagent-test test token-tracking --log-file logs/token-tracking-test.log
  mcpagent-test test token-tracking --num-calls 5 --log-file logs/token-tracking-test.log
  mcpagent-test test token-tracking --model gpt-4.1 --log-file logs/token-tracking-test.log`,
	RunE: func(cmd *cobra.Command, args []string) error {
		logger := testutils.NewTestLoggerFromViper()
		logger.Info("=== Token Tracking Test ===")

		// Get test parameters
		numCalls := viper.GetInt("num-calls")
		if numCalls == 0 {
			numCalls = 3 // Default to 3 calls
		}

		logger.Info("Test configuration",
			loggerv2.Int("num_calls", numCalls))

		// Run the test
		if err := testTokenTracking(logger, numCalls); err != nil {
			return fmt.Errorf("token tracking test failed: %w", err)
		}

		logger.Info("✅ Token tracking test passed!")
		return nil
	},
}

func init() {
	tokenTrackingTestCmd.Flags().String("model", "", "Model ID to use (e.g., gpt-4.1)")
	tokenTrackingTestCmd.Flags().Int("num-calls", 3, "Number of agent calls to make (default: 3)")

	_ = viper.BindPFlag("model", tokenTrackingTestCmd.Flags().Lookup("model"))         //nolint:gosec // BindPFlag errors are non-critical in test init
	_ = viper.BindPFlag("num-calls", tokenTrackingTestCmd.Flags().Lookup("num-calls")) //nolint:gosec // BindPFlag errors are non-critical in test init
}

// GetTokenTrackingTestCmd returns the token tracking test command
func GetTokenTrackingTestCmd() *cobra.Command {
	return tokenTrackingTestCmd
}

// testTokenTracking tests the token usage tracking feature
func testTokenTracking(log loggerv2.Logger, numCalls int) error {
	log.Info("--- Test: Token Usage Tracking ---")

	ctx := context.Background()
	traceID := testutils.GenerateTestTraceID()

	// Create minimal MCP config (no servers needed for basic token tracking)
	mcpServers := map[string]interface{}{}
	configPath, cleanup, err := testutils.CreateTempMCPConfig(mcpServers, log)
	if err != nil {
		return fmt.Errorf("failed to create temp MCP config: %w", err)
	}
	defer cleanup()

	// Get optional tracer
	tracer, _ := testutils.GetTracerWithLogger("langfuse", log)
	if tracer == nil {
		tracer, _ = testutils.GetTracerWithLogger("noop", log)
	}

	// Initialize LLM
	modelID := viper.GetString("model")
	if modelID == "" {
		modelID = openai.ModelGPT41 // Default
	}
	model, llmProvider, err := testutils.CreateTestLLM(&testutils.TestLLMConfig{
		Provider: "",      // Empty to use viper/flags
		ModelID:  modelID, // Use model from flag if provided
		Logger:   log,
	})
	if err != nil {
		return fmt.Errorf("failed to initialize LLM: %w", err)
	}

	log.Info("✅ LLM initialized", loggerv2.String("model_id", modelID), loggerv2.String("provider", string(llmProvider)))

	// Create agent
	ag, err := testutils.CreateAgentWithTracer(ctx, model, llmProvider, configPath, tracer, traceID, log)
	if err != nil {
		return fmt.Errorf("failed to create agent: %w", err)
	}

	log.Info("✅ Agent created")

	// Test 1: Initial token usage (should be zero)
	log.Info("--- Test 1: Initial Token Usage (Should Be Zero) ---")
	promptTokens, completionTokens, totalTokens, cacheTokens, reasoningTokens, llmCallCount, cacheEnabledCallCount := ag.GetTokenUsage()
	logTokenUsage(log, "Initial", promptTokens, completionTokens, totalTokens, cacheTokens, reasoningTokens, llmCallCount, cacheEnabledCallCount)

	if totalTokens != 0 || llmCallCount != 0 {
		log.Warn("⚠️  Initial token usage is not zero - agent may have been used before",
			loggerv2.Int("total_tokens", totalTokens),
			loggerv2.Int("llm_call_count", llmCallCount))
	} else {
		log.Info("✅ Initial token usage is zero as expected")
	}

	// Test 2: Single call token accumulation
	log.Info("--- Test 2: Single Call Token Accumulation ---")
	question1 := "What is 2+2? Please give a brief answer."
	log.Info("Making first agent call...", loggerv2.String("question", question1))

	startTime := time.Now()
	response1, err := ag.Ask(ctx, question1)
	if err != nil {
		return fmt.Errorf("failed to make first agent call: %w", err)
	}
	duration1 := time.Since(startTime)

	log.Info("✅ First call completed",
		loggerv2.String("response_preview", truncateString(response1, 100)),
		loggerv2.Any("duration", duration1))

	// Check token usage after first call
	promptTokens, completionTokens, totalTokens, cacheTokens, reasoningTokens, llmCallCount, cacheEnabledCallCount = ag.GetTokenUsage()
	logTokenUsage(log, "After First Call", promptTokens, completionTokens, totalTokens, cacheTokens, reasoningTokens, llmCallCount, cacheEnabledCallCount)

	if totalTokens == 0 {
		log.Warn("⚠️  Total tokens is still zero after first call - token tracking may not be working")
	} else {
		log.Info("✅ Token accumulation working - tokens tracked after first call")
	}

	if llmCallCount != 1 {
		log.Warn("⚠️  LLM call count mismatch",
			loggerv2.Int("expected", 1),
			loggerv2.Int("actual", llmCallCount))
	} else {
		log.Info("✅ LLM call count is correct")
	}

	// Test 3: Multiple calls cumulative accumulation
	log.Info("--- Test 3: Multiple Calls Cumulative Accumulation ---")
	previousTotal := totalTokens
	previousCallCount := llmCallCount

	questions := []string{
		"What is the capital of France?",
		"Name three primary colors.",
		"What is 10 * 5?",
	}

	// Make additional calls (up to numCalls total, including the first one)
	callsToMake := numCalls - 1
	if callsToMake > len(questions) {
		callsToMake = len(questions)
	}

	for i := 0; i < callsToMake; i++ {
		log.Info("Making additional call...",
			loggerv2.Int("call_number", i+2),
			loggerv2.String("question", questions[i]))

		startTime = time.Now()
		response, err := ag.Ask(ctx, questions[i])
		if err != nil {
			return fmt.Errorf("failed to make agent call %d: %w", i+2, err)
		}
		duration := time.Since(startTime)

		log.Info("✅ Call completed",
			loggerv2.Int("call_number", i+2),
			loggerv2.String("response_preview", truncateString(response, 100)),
			loggerv2.Any("duration", duration))

		// Check token usage after each call
		promptTokens, completionTokens, totalTokens, cacheTokens, reasoningTokens, llmCallCount, cacheEnabledCallCount = ag.GetTokenUsage()
		logTokenUsage(log, fmt.Sprintf("After Call %d", i+2), promptTokens, completionTokens, totalTokens, cacheTokens, reasoningTokens, llmCallCount, cacheEnabledCallCount)

		// Verify cumulative accumulation
		if totalTokens <= previousTotal {
			log.Warn("⚠️  Total tokens did not increase",
				loggerv2.Int("previous", previousTotal),
				loggerv2.Int("current", totalTokens))
		} else {
			log.Info("✅ Token accumulation verified - tokens increased",
				loggerv2.Int("previous", previousTotal),
				loggerv2.Int("current", totalTokens),
				loggerv2.Int("increase", totalTokens-previousTotal))
		}

		if llmCallCount <= previousCallCount {
			log.Warn("⚠️  LLM call count did not increase",
				loggerv2.Int("previous", previousCallCount),
				loggerv2.Int("current", llmCallCount))
		} else {
			log.Info("✅ LLM call count increased",
				loggerv2.Int("previous", previousCallCount),
				loggerv2.Int("current", llmCallCount))
		}

		previousTotal = totalTokens
		previousCallCount = llmCallCount
	}

	// Test 4: Multi-turn conversation
	log.Info("--- Test 4: Multi-Turn Conversation Token Accumulation ---")
	conversationQuestions := []string{
		"My name is Alice. Remember that.",
		"What is my name?",
		"What did I tell you my name was?",
	}

	conversationTotalBefore := totalTokens
	conversationCallCountBefore := llmCallCount

	for i, question := range conversationQuestions {
		log.Info("Multi-turn conversation call...",
			loggerv2.Int("turn", i+1),
			loggerv2.String("question", question))

		startTime = time.Now()
		response, err := ag.Ask(ctx, question)
		if err != nil {
			return fmt.Errorf("failed to make conversation call %d: %w", i+1, err)
		}
		duration := time.Since(startTime)

		log.Info("✅ Conversation call completed",
			loggerv2.Int("turn", i+1),
			loggerv2.String("response_preview", truncateString(response, 100)),
			loggerv2.Any("duration", duration))
	}

	// Check final token usage after multi-turn conversation
	promptTokens, completionTokens, totalTokens, cacheTokens, reasoningTokens, llmCallCount, cacheEnabledCallCount = ag.GetTokenUsage()
	logTokenUsage(log, "After Multi-Turn Conversation", promptTokens, completionTokens, totalTokens, cacheTokens, reasoningTokens, llmCallCount, cacheEnabledCallCount)

	conversationTokens := totalTokens - conversationTotalBefore
	conversationCalls := llmCallCount - conversationCallCountBefore

	log.Info("Multi-turn conversation summary",
		loggerv2.Int("tokens_used", conversationTokens),
		loggerv2.Int("calls_made", conversationCalls),
		loggerv2.Int("tokens_per_call", conversationTokens/conversationCalls))

	// Test 5: Final summary
	log.Info("--- Test 5: Final Token Usage Summary ---")
	finalPromptTokens, finalCompletionTokens, finalTotalTokens, finalCacheTokens, finalReasoningTokens, finalLLMCallCount, finalCacheEnabledCallCount := ag.GetTokenUsage()
	logTokenUsage(log, "Final Summary", finalPromptTokens, finalCompletionTokens, finalTotalTokens, finalCacheTokens, finalReasoningTokens, finalLLMCallCount, finalCacheEnabledCallCount)

	// Calculate averages
	if finalLLMCallCount > 0 {
		avgPromptTokens := float64(finalPromptTokens) / float64(finalLLMCallCount)
		avgCompletionTokens := float64(finalCompletionTokens) / float64(finalLLMCallCount)
		avgTotalTokens := float64(finalTotalTokens) / float64(finalLLMCallCount)

		log.Info("Token usage averages per call",
			loggerv2.Any("avg_prompt_tokens", avgPromptTokens),
			loggerv2.Any("avg_completion_tokens", avgCompletionTokens),
			loggerv2.Any("avg_total_tokens", avgTotalTokens))
	}

	// Cache tokens analysis
	if finalCacheTokens > 0 {
		log.Info("✅ Cache tokens detected",
			loggerv2.Int("total_cache_tokens", finalCacheTokens),
			loggerv2.Int("cache_enabled_calls", finalCacheEnabledCallCount))
		if finalCacheEnabledCallCount > 0 {
			avgCacheTokens := float64(finalCacheTokens) / float64(finalCacheEnabledCallCount)
			log.Info("Average cache tokens per cache-enabled call",
				loggerv2.Any("avg_cache_tokens", avgCacheTokens))
		}
	} else {
		log.Info("ℹ️  No cache tokens detected (this is normal if provider/model doesn't support cache)")
	}

	// Reasoning tokens analysis
	if finalReasoningTokens > 0 {
		log.Info("✅ Reasoning tokens detected",
			loggerv2.Int("total_reasoning_tokens", finalReasoningTokens))
	} else {
		log.Info("ℹ️  No reasoning tokens detected (this is normal if model doesn't support reasoning tokens)")
	}

	// Verify token consistency
	if finalTotalTokens != (finalPromptTokens + finalCompletionTokens) {
		// Note: This might be expected if reasoning tokens are included in total
		log.Info("ℹ️  Total tokens does not equal prompt + completion (may include reasoning tokens)",
			loggerv2.Int("prompt", finalPromptTokens),
			loggerv2.Int("completion", finalCompletionTokens),
			loggerv2.Int("total", finalTotalTokens),
			loggerv2.Int("difference", finalTotalTokens-(finalPromptTokens+finalCompletionTokens)))
	} else {
		log.Info("✅ Token consistency verified: total = prompt + completion")
	}

	log.Info("✅ All token tracking tests completed successfully")
	return nil
}

// logTokenUsage logs token usage in a structured format
func logTokenUsage(log loggerv2.Logger, label string, promptTokens, completionTokens, totalTokens, cacheTokens, reasoningTokens, llmCallCount, cacheEnabledCallCount int) {
	log.Info(fmt.Sprintf("📊 Token Usage: %s", label),
		loggerv2.Int("prompt_tokens", promptTokens),
		loggerv2.Int("completion_tokens", completionTokens),
		loggerv2.Int("total_tokens", totalTokens),
		loggerv2.Int("cache_tokens", cacheTokens),
		loggerv2.Int("reasoning_tokens", reasoningTokens),
		loggerv2.Int("llm_call_count", llmCallCount),
		loggerv2.Int("cache_enabled_call_count", cacheEnabledCallCount))
}

// truncateString truncates a string to the specified length
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
