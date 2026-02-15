package agentmcp

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	mcpagent "github.com/manishiitg/mcpagent/agent"
	testutils "github.com/manishiitg/mcpagent/cmd/testing/testutils"
	"github.com/manishiitg/mcpagent/llm"
	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"
	"github.com/manishiitg/mcpagent/observability"

	"github.com/manishiitg/multi-llm-provider-go/pkg/adapters/vertex"
)

var agentMCPTestCmd = &cobra.Command{
	Use:   "agent-mcp",
	Short: "Test agent with MCP servers (sequential-thinking, context7, aws-pricing)",
	Long: `Test agent functionality with multiple MCP servers.

This test:
1. Creates a temporary MCP config with sequential-thinking, context7, and awslabs.aws-pricing-mcp-server
2. Creates an agent with those MCP servers
3. Runs the agent with a question that requires tool usage
4. Verifies that MCP tools were called and used correctly

Note: This test doesn't use traditional asserts. Logs are analyzed (manually or by LLM) to verify success.
See criteria.md in the agent-mcp folder for detailed log analysis criteria.

Examples:
  mcpagent-test test agent-mcp --log-file logs/agent-mcp-test.log
  mcpagent-test test agent-mcp --provider openai --model gpt-4.1-mini --log-file logs/agent-mcp-test.log
  mcpagent-test test agent-mcp --verbose --log-file logs/agent-mcp-test.log`,
	RunE: func(cmd *cobra.Command, args []string) error {
		logger := testutils.NewTestLoggerFromViper()
		logger.Info("=== Agent MCP Test ===")

		// Test: Create agent with MCP servers and run a question
		if err := testAgentWithMCPServers(logger); err != nil {
			return fmt.Errorf("agent MCP test failed: %w", err)
		}

		logger.Info("✅ Agent MCP test passed!")
		return nil
	},
}

func init() {
	agentMCPTestCmd.Flags().String("model", "", "Model ID to use (e.g., gpt-4.1-mini, gpt-4o, claude-sonnet-4)")
	_ = viper.BindPFlag("model", agentMCPTestCmd.Flags().Lookup("model")) //nolint:gosec // BindPFlag errors are non-critical in test init
}

// GetAgentMCPTestCmd returns the agent MCP test command
func GetAgentMCPTestCmd() *cobra.Command {
	return agentMCPTestCmd
}

// testAgentWithMCPServers tests agent with MCP servers
func testAgentWithMCPServers(log loggerv2.Logger) error {
	log.Info("--- Test: Agent with MCP Servers ---")

	// Create temporary MCP config with the three servers
	mcpServers := map[string]interface{}{
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

	configPath, cleanup, err := testutils.CreateTempMCPConfig(mcpServers, log)
	if err != nil {
		return fmt.Errorf("failed to create temp MCP config: %w", err)
	}
	defer cleanup()

	log.Info("✅ Created temporary MCP config",
		loggerv2.String("path", configPath),
		loggerv2.Int("server_count", len(mcpServers)))

	// Get optional tracers (Langfuse and/or LangSmith if available)
	var tracerOptions []mcpagent.AgentOption

	// Try Langfuse
	langfuseTracer, _ := testutils.GetTracerWithLogger("langfuse", log)
	if langfuseTracer != nil && !testutils.IsNoopTracer(langfuseTracer) {
		tracerOptions = append(tracerOptions, mcpagent.WithTracer(langfuseTracer))
		log.Info("✅ Langfuse tracer enabled")
	}

	// Try LangSmith
	langsmithTracer, _ := testutils.GetTracerWithLogger("langsmith", log)
	if langsmithTracer != nil && !testutils.IsNoopTracer(langsmithTracer) {
		tracerOptions = append(tracerOptions, mcpagent.WithTracer(langsmithTracer))
		log.Info("✅ LangSmith tracer enabled")
	}

	// Fallback to first available tracer for legacy support
	var tracer observability.Tracer
	if langfuseTracer != nil && !testutils.IsNoopTracer(langfuseTracer) {
		tracer = langfuseTracer
	} else if langsmithTracer != nil && !testutils.IsNoopTracer(langsmithTracer) {
		tracer = langsmithTracer
	} else {
		tracer, _ = testutils.GetTracerWithLogger("noop", log)
	}

	// Initialize LLM - default to Vertex with gemini-3-flash-preview
	modelID := viper.GetString("model")
	provider := viper.GetString("test.provider")
	if provider == "" {
		provider = string(llm.ProviderVertex) // Default to Vertex
	}
	if modelID == "" {
		modelID = vertex.ModelGemini3FlashPreview // Default to gemini-3-flash-preview
	}
	model, llmProvider, err := testutils.CreateTestLLM(&testutils.TestLLMConfig{
		Provider: provider,
		ModelID:  modelID,
		Logger:   log,
	})
	if err != nil {
		return fmt.Errorf("failed to initialize LLM: %w", err)
	}

	log.Info("✅ LLM initialized",
		loggerv2.String("model_id", modelID),
		loggerv2.String("provider", string(llmProvider)))

	ctx := context.Background()
	traceID := testutils.GenerateTestTraceID()

	log.Info("Creating agent with MCP servers...",
		loggerv2.String("trace_id", string(traceID)),
		loggerv2.String("config_path", configPath))

	// Create agent with MCP servers (pass additional tracer options for multi-tracer support)
	ag, err := testutils.CreateAgentWithTracer(ctx, model, llmProvider, configPath, tracer, traceID, log, tracerOptions...)
	if err != nil {
		return fmt.Errorf("failed to create agent: %w", err)
	}

	log.Info("✅ Agent created with MCP servers",
		loggerv2.Int("server_count", len(mcpServers)))

	// Run agent with a question that will trigger MCP tool usage
	// This question should trigger sequential-thinking and potentially context7 or aws-pricing
	question := "Use sequential thinking to analyze: What would be the cost of running an EC2 t3.medium instance in us-east-1 for 30 days? Then search for any relevant AWS documentation about EC2 pricing."

	log.Info("Running agent with question that requires MCP tool usage...",
		loggerv2.String("question", question))

	// Run the agent - it should use MCP tools
	startTime := time.Now()
	response, err := ag.Ask(ctx, question)
	duration := time.Since(startTime)

	if err != nil {
		return fmt.Errorf("agent execution failed: %w", err)
	}

	log.Info("✅ Agent executed successfully",
		loggerv2.String("response", response),
		loggerv2.String("trace_id", string(traceID)),
		loggerv2.String("duration", duration.String()))

	// Verify that the response contains evidence of tool usage
	// The response should mention EC2 pricing or cost information
	if len(response) < 50 {
		log.Warn("Response seems too short - tools may not have been called",
			loggerv2.String("response", response))
	} else {
		log.Info("✅ Response length indicates tool usage",
			loggerv2.Int("response_length", len(response)))
	}

	// Flush all tracers (both Langfuse and LangSmith if enabled)
	if langfuseTracer != nil {
		if flusher, ok := langfuseTracer.(interface{ Flush() }); ok {
			log.Info("Flushing Langfuse tracer...")
			flusher.Flush()
			log.Info("✅ Langfuse tracer flushed")
		}
	}
	if langsmithTracer != nil {
		if flusher, ok := langsmithTracer.(interface{ Flush() }); ok {
			log.Info("Flushing LangSmith tracer...")
			flusher.Flush()
			log.Info("✅ LangSmith tracer flushed")
		}
	}

	logFile := viper.GetString("log-file")

	log.Info("✅ Agent MCP test completed",
		loggerv2.String("trace_id", string(traceID)),
		loggerv2.String("response_preview", truncateString(response, 200)),
		loggerv2.Int("response_length", len(response)),
		loggerv2.String("duration", duration.String()))

	log.Info("")
	if logFile != "" {
		log.Info("📋 Log file saved", loggerv2.String("path", logFile))
		log.Info("   See criteria.md in agent-mcp folder for log analysis criteria")
	} else {
		log.Info("📋 See criteria.md in agent-mcp folder for log analysis criteria")
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
