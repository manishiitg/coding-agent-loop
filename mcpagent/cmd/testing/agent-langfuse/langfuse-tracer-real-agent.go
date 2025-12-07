package agentlangfuse

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	mcpagent "mcpagent/agent"
	testutils "mcpagent/cmd/testing/testutils"
	loggerv2 "mcpagent/logger/v2"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

var langfuseTracerTestCmd = &cobra.Command{
	Use:   "langfuse-tracer",
	Short: "Test Langfuse tracing with agent",
	Long: `Test Langfuse tracing functionality with the agent.

This test:
1. Creates a Langfuse tracer
2. Creates an agent with the Langfuse tracer
3. Runs the agent with a simple question
4. The agent will automatically emit events to Langfuse

Examples:
  mcpagent-test test langfuse-tracer
  mcpagent-test test langfuse-tracer --verbose
  mcpagent-test test langfuse-tracer --provider openai --model gpt-4.1-mini`,
	RunE: func(cmd *cobra.Command, args []string) error {
		logger := testutils.NewTestLoggerFromViper()
		logger.Info("=== Langfuse Tracer Test with Agent ===")

		// Test: Create agent with Langfuse tracer and run a question
		if err := testAgentWithLangfuseTracer(logger); err != nil {
			return fmt.Errorf("agent with langfuse tracer test failed: %w", err)
		}

		logger.Info("✅ Langfuse tracer test passed!")
		return nil
	},
}

func init() {
	langfuseTracerTestCmd.Flags().String("model", "", "Model ID to use (e.g., gpt-4.1-mini, gpt-4o, claude-sonnet-4)")
	viper.BindPFlag("model", langfuseTracerTestCmd.Flags().Lookup("model"))
}

// GetLangfuseTracerTestCmd returns the Langfuse tracer test command
func GetLangfuseTracerTestCmd() *cobra.Command {
	return langfuseTracerTestCmd
}

// testAgentWithLangfuseTracer tests agent with Langfuse tracer
func testAgentWithLangfuseTracer(log loggerv2.Logger) error {
	log.Info("--- Test: Agent with Langfuse Tracer ---")

	// Get Langfuse tracer (will use environment variables or .env file)
	langfuseTracer, isRealTracer := testutils.GetTracerWithLogger("langfuse", log)
	if langfuseTracer == nil {
		return fmt.Errorf("failed to get Langfuse tracer - check LANGFUSE_PUBLIC_KEY and LANGFUSE_SECRET_KEY")
	}

	// Check if it's actually a Langfuse tracer (not NoopTracer)
	if !isRealTracer {
		log.Warn("Langfuse tracer not available, using NoopTracer. Check credentials.")
		log.Info("Skipping agent test - Langfuse credentials required")
		return nil // Not an error, just skip if credentials missing
	}

	log.Info("✅ Langfuse tracer initialized",
		loggerv2.String("tracer_type", fmt.Sprintf("%T", langfuseTracer)))

	// Initialize LLM (required for agent)
	// Use provider and model from viper/flags (defaults to bedrock, but can be overridden with --provider and --model flags)
	modelID := viper.GetString("model")
	model, err := testutils.CreateTestLLM(&testutils.TestLLMConfig{
		Provider: "",      // Empty to use viper/flags
		ModelID:  modelID, // Use model from flag if provided
		Logger:   log,
	})
	if err != nil {
		log.Warn("Failed to initialize LLM", loggerv2.Error(err))
		log.Info("Skipping agent execution test - LLM initialization required")
		return nil // Not an error, just skip if LLM not available
	}

	ctx := context.Background()
	traceID := testutils.GenerateTestTraceID()

	log.Info("Creating agent with Langfuse tracer...",
		loggerv2.String("trace_id", string(traceID)))

	// Create minimal agent with Langfuse tracer
	ag, err := testutils.CreateMinimalAgent(ctx, model, langfuseTracer, traceID, log)
	if err != nil {
		return fmt.Errorf("failed to create agent: %w", err)
	}

	log.Info("✅ Agent created with Langfuse tracer")

	// Run agent with a simple question
	// The agent will automatically emit events to Langfuse
	question := "What is the weather today?"

	log.Info("Running agent with question...",
		loggerv2.String("question", question))

	// Create initial message
	messages := []llmtypes.MessageContent{
		{
			Role: llmtypes.ChatMessageTypeHuman,
			Parts: []llmtypes.ContentPart{
				llmtypes.TextContent{Text: question},
			},
		},
	}

	// Run the agent - it will automatically emit events to Langfuse
	response, updatedMessages, err := mcpagent.AskWithHistory(ag, ctx, messages)
	if err != nil {
		return fmt.Errorf("agent execution failed: %w", err)
	}

	log.Info("✅ Agent executed successfully",
		loggerv2.String("response", response),
		loggerv2.String("trace_id", string(traceID)))

	// Wait for events to be processed and sent to Langfuse
	log.Info("Waiting for events to be sent to Langfuse...")
	time.Sleep(3 * time.Second)

	// Flush tracer to ensure all events are sent
	if flusher, ok := langfuseTracer.(interface{ Flush() }); ok {
		flusher.Flush()
		log.Info("✅ Tracer flushed")
	}

	log.Info("✅ Agent with Langfuse tracer test completed",
		loggerv2.String("trace_id", string(traceID)),
		loggerv2.String("response", response),
		loggerv2.String("response_length", fmt.Sprintf("%d", len(response))),
		loggerv2.Int("messages_count", len(updatedMessages)))

	return nil
}
