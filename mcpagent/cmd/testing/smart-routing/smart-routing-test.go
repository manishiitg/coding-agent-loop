package smartrouting

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
	"github.com/manishiitg/multi-llm-provider-go/pkg/adapters/openai"

	mcpagent "github.com/manishiitg/mcpagent/agent"
	testutils "github.com/manishiitg/mcpagent/cmd/testing/testutils"
	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"
)

var smartRoutingTestCmd = &cobra.Command{
	Use:   "smart-routing",
	Short: "Test smart routing feature (tool filtering based on conversation context)",
	Long: `Test the agent's smart routing feature that filters tools based on conversation context.

This test:
1. Creates an agent with multiple MCP servers (enough to exceed thresholds)
2. Enables smart routing with configurable thresholds
3. Runs conversations that should trigger smart routing
4. Verifies that tools are filtered correctly based on conversation context
5. Checks that system prompt is rebuilt with filtered servers

Note: This test doesn't use traditional asserts. Logs are analyzed (manually or by LLM) to verify success.
See criteria.md in the smart-routing folder for detailed log analysis criteria.

Examples:
  mcpagent-test test smart-routing --log-file logs/smart-routing-test.log
  mcpagent-test test smart-routing --max-tools-threshold 5 --max-servers-threshold 2 --log-file logs/smart-routing-test.log
  mcpagent-test test smart-routing --temperature 0.1 --max-tokens 1000 --log-file logs/smart-routing-test.log`,
	RunE: func(cmd *cobra.Command, args []string) error {
		logger := testutils.NewTestLoggerFromViper()
		logger.Info("=== Smart Routing Test ===")

		if err := testSmartRouting(logger); err != nil {
			return fmt.Errorf("smart routing test failed: %w", err)
		}

		logger.Info("✅ Smart Routing test passed!")
		return nil
	},
}

func init() {
	smartRoutingTestCmd.Flags().String("model", "", "Model ID to use (e.g., gpt-4.1-mini, gpt-4o)")
	smartRoutingTestCmd.Flags().Int("max-tools-threshold", 5, "Maximum tools threshold for smart routing (default: 5)")
	smartRoutingTestCmd.Flags().Int("max-servers-threshold", 2, "Maximum servers threshold for smart routing (default: 2)")
	smartRoutingTestCmd.Flags().Float64("temperature", 0.1, "Temperature for smart routing LLM call (default: 0.1)")
	smartRoutingTestCmd.Flags().Int("max-tokens", 1000, "Max tokens for smart routing LLM call (default: 1000)")
	_ = viper.BindPFlag("model", smartRoutingTestCmd.Flags().Lookup("model"))                                 //nolint:gosec // BindPFlag errors are non-critical in test init
	_ = viper.BindPFlag("max-tools-threshold", smartRoutingTestCmd.Flags().Lookup("max-tools-threshold"))     //nolint:gosec // BindPFlag errors are non-critical in test init
	_ = viper.BindPFlag("max-servers-threshold", smartRoutingTestCmd.Flags().Lookup("max-servers-threshold")) //nolint:gosec // BindPFlag errors are non-critical in test init
	_ = viper.BindPFlag("temperature", smartRoutingTestCmd.Flags().Lookup("temperature"))                     //nolint:gosec // BindPFlag errors are non-critical in test init
	_ = viper.BindPFlag("max-tokens", smartRoutingTestCmd.Flags().Lookup("max-tokens"))                       //nolint:gosec // BindPFlag errors are non-critical in test init
}

// GetSmartRoutingTestCmd returns the smart routing test command
func GetSmartRoutingTestCmd() *cobra.Command {
	return smartRoutingTestCmd
}

// testSmartRouting implements the main test logic
func testSmartRouting(log loggerv2.Logger) error {
	log.Info("--- Test: Smart Routing Feature ---")

	ctx := context.Background()
	traceID := testutils.GenerateTestTraceID()

	// Initialize LLM
	modelID := viper.GetString("model")
	if modelID == "" {
		modelID = openai.ModelGPT41Mini // Default to gpt-4.1-mini
	}
	model, llmProvider, err := testutils.CreateTestLLM(&testutils.TestLLMConfig{
		Provider: "",
		ModelID:  modelID,
		Logger:   log,
	})
	if err != nil {
		return fmt.Errorf("failed to initialize LLM: %w", err)
	}
	log.Info("✅ LLM initialized", loggerv2.String("model_id", modelID), loggerv2.String("provider", string(llmProvider)))

	// Create temporary MCP config with multiple servers to exceed thresholds
	// We need at least max-servers-threshold + 1 servers
	maxServersThreshold := viper.GetInt("max-servers-threshold")
	if maxServersThreshold == 0 {
		maxServersThreshold = 2
	}
	requiredServers := maxServersThreshold + 2 // Ensure we exceed threshold

	// Use common MCP servers for testing (same format as agent-mcp-test.go)
	servers := map[string]interface{}{
		"sequential-thinking": map[string]interface{}{
			"command": "npx",
			"args":    []interface{}{"--yes", "@modelcontextprotocol/server-sequential-thinking"},
		},
		"context7": map[string]interface{}{
			"url":      "https://mcp.context7.com/mcp",
			"protocol": "http",
		},
		"awslabs.aws-pricing-mcp-server": map[string]interface{}{
			"command": "uvx",
			"args":    []interface{}{"awslabs.aws-pricing-mcp-server"},
			"env": map[string]interface{}{
				"FASTMCP_LOG_LEVEL": "ERROR",
				"AWS_PROFILE":       "default",
				"AWS_REGION":        "us-east-1",
			},
			"disabled":    false,
			"autoApprove": []interface{}{},
		},
	}

	// Add more servers if needed to exceed threshold
	if len(servers) < requiredServers {
		// Add additional servers if available
		// For now, we'll use what we have and adjust thresholds if needed
		log.Info("Using available servers", loggerv2.Int("server_count", len(servers)))
	}

	configPath, cleanup, err := testutils.CreateTempMCPConfig(servers, log)
	if err != nil {
		return fmt.Errorf("failed to create temp MCP config: %w", err)
	}
	defer cleanup()
	log.Info("✅ Created temporary MCP config", loggerv2.String("path", configPath), loggerv2.Int("server_count", len(servers)))

	// Create agent with smart routing enabled
	tracer, _ := testutils.GetTracerWithLogger("noop", log)
	ag, err := testutils.CreateAgentWithTracer(ctx, model, llmProvider, configPath, tracer, traceID, log,
		mcpagent.WithSmartRouting(true), // Enable smart routing
	)
	if err != nil {
		return fmt.Errorf("failed to create agent: %w", err)
	}

	// Set smart routing thresholds (low values for testing)
	maxToolsThreshold := viper.GetInt("max-tools-threshold")
	if maxToolsThreshold == 0 {
		maxToolsThreshold = 5
	}
	ag.SetSmartRoutingThresholds(maxToolsThreshold, maxServersThreshold)
	log.Info("✅ Smart routing thresholds set",
		loggerv2.Int("max_tools", maxToolsThreshold),
		loggerv2.Int("max_servers", maxServersThreshold))

	// Set smart routing config
	temperature := viper.GetFloat64("temperature")
	if temperature == 0 {
		temperature = 0.1
	}
	maxTokens := viper.GetInt("max-tokens")
	if maxTokens == 0 {
		maxTokens = 1000
	}
	ag.SetSmartRoutingConfig(temperature, maxTokens, 8, 200, 300)
	log.Info("✅ Smart routing config set",
		loggerv2.Any("temperature", temperature),
		loggerv2.Int("max_tokens", maxTokens))

	// Check if smart routing should be used
	shouldUse := ag.ShouldUseSmartRouting()
	log.Info("Smart routing eligibility check",
		loggerv2.Any("should_use", shouldUse),
		loggerv2.Int("total_tools", len(ag.Tools)),
		loggerv2.Int("total_servers", len(ag.Clients)),
		loggerv2.Int("max_tools_threshold", maxToolsThreshold),
		loggerv2.Int("max_servers_threshold", maxServersThreshold))

	if !shouldUse {
		log.Warn("⚠️ Smart routing will not be used - thresholds not exceeded",
			loggerv2.Int("tools", len(ag.Tools)),
			loggerv2.Int("servers", len(ag.Clients)),
			loggerv2.Int("required_tools", maxToolsThreshold+1),
			loggerv2.Int("required_servers", maxServersThreshold+1))
		log.Info("💡 Tip: Adjust --max-tools-threshold and --max-servers-threshold to lower values if needed")
	}

	// --- Test Scenario 1: Conversation that should trigger smart routing ---
	log.Info("--- Scenario 1: Testing smart routing with AWS-focused question ---")
	question1 := "What would be the cost of running an EC2 t3.medium instance in us-east-1 for 30 days?"

	log.Info("Running agent with AWS-focused question...", loggerv2.String("question", question1))
	startTime := time.Now()
	response1, err := ag.Ask(ctx, question1)
	duration1 := time.Since(startTime)

	if err != nil {
		return fmt.Errorf("agent execution failed: %w", err)
	}
	log.Info("✅ Agent executed successfully",
		loggerv2.String("response_preview", truncateString(response1, 200)),
		loggerv2.String("duration", duration1.String()),
		loggerv2.Int("response_length", len(response1)))

	// Check if smart routing was applied by checking filtered tools
	filteredToolsCount := len(ag.Tools) // This will be updated after conversation
	log.Info("Tool filtering results",
		loggerv2.Int("total_tools", len(ag.Tools)),
		loggerv2.Int("filtered_tools", filteredToolsCount))

	// --- Test Scenario 2: Multi-turn conversation to test context building ---
	log.Info("--- Scenario 2: Testing smart routing with multi-turn conversation ---")

	// First turn: AWS question
	userMessage1 := fmt.Sprintf("I need to analyze AWS costs. %s", question1)
	log.Info("Turn 1: AWS question", loggerv2.String("question", userMessage1))
	response2a, _, err := ag.AskWithHistory(ctx, []llmtypes.MessageContent{
		{Role: llmtypes.ChatMessageTypeHuman, Parts: []llmtypes.ContentPart{llmtypes.TextContent{Text: userMessage1}}},
	})
	if err != nil {
		log.Warn("Turn 1 failed", loggerv2.Error(err))
	} else {
		log.Info("✅ Turn 1 completed", loggerv2.String("response_preview", truncateString(response2a, 150)))
	}

	// Second turn: Follow-up question that might need different servers
	userMessage2 := "Now use sequential thinking to analyze the cost implications"
	log.Info("Turn 2: Sequential thinking question", loggerv2.String("question", userMessage2))
	response2b, _, err := ag.AskWithHistory(ctx, []llmtypes.MessageContent{
		{Role: llmtypes.ChatMessageTypeHuman, Parts: []llmtypes.ContentPart{llmtypes.TextContent{Text: userMessage1}}},
		{Role: llmtypes.ChatMessageTypeAI, Parts: []llmtypes.ContentPart{llmtypes.TextContent{Text: response2a}}},
		{Role: llmtypes.ChatMessageTypeHuman, Parts: []llmtypes.ContentPart{llmtypes.TextContent{Text: userMessage2}}},
	})
	if err != nil {
		log.Warn("Turn 2 failed", loggerv2.Error(err))
	} else {
		log.Info("✅ Turn 2 completed", loggerv2.String("response_preview", truncateString(response2b, 150)))
	}

	// --- Test Scenario 3: Verify smart routing events in logs ---
	log.Info("--- Scenario 3: Verifying smart routing behavior ---")
	log.Info("✅ Smart routing test scenarios completed",
		loggerv2.String("trace_id", string(traceID)),
		loggerv2.String("duration_scenario1", duration1.String()),
		loggerv2.Any("smart_routing_enabled", ag.IsSmartRoutingEnabled()),
		loggerv2.Any("should_use_smart_routing", shouldUse))

	logFile := viper.GetString("log-file")
	log.Info("")
	if logFile != "" {
		log.Info("📋 Log file saved", loggerv2.String("path", logFile))
		log.Info("   See criteria.md in smart-routing folder for log analysis criteria")
	} else {
		log.Info("📋 See criteria.md in smart-routing folder for log analysis criteria")
		log.Info("   Tip: Use --log-file to save logs for analysis")
	}
	log.Info("   These tests don't use traditional asserts - logs are analyzed by LLM to verify success")

	return nil
}

// truncateString truncates a string to the specified length
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
