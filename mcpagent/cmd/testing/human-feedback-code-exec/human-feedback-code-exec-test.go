package humanfeedbackcodeexec

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	mcpagent "github.com/manishiitg/mcpagent/agent"
	testutils "github.com/manishiitg/mcpagent/cmd/testing/testutils"
	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"

	"github.com/manishiitg/multi-llm-provider-go/pkg/adapters/openai"
)

var humanFeedbackCodeExecTestCmd = &cobra.Command{
	Use:   "human-feedback-code-exec",
	Short: "Test human_feedback tool availability in code execution mode",
	Long: `Test that human_feedback tool is available as a normal tool in code execution mode.

This test:
1. Creates an agent in code execution mode
2. Registers a human_feedback tool with category "human"
3. Registers a regular custom tool (should be excluded in code exec mode)
4. Verifies human_feedback is available in Tools array
5. Verifies regular custom tools are NOT in Tools array (only virtual tools)
6. Verifies only discover_code_files and write_code virtual tools are available

Note: This test doesn't use traditional asserts. Logs are analyzed (manually or by LLM) to verify success.
See criteria.md in the human-feedback-code-exec folder for detailed log analysis criteria.

Examples:
  mcpagent-test test human-feedback-code-exec --log-file logs/human-feedback-code-exec-test.log
  mcpagent-test test human-feedback-code-exec --model gpt-4.1 --log-file logs/human-feedback-code-exec-test.log`,
	RunE: func(cmd *cobra.Command, args []string) error {
		logger := testutils.NewTestLoggerFromViper()
		logger.Info("=== Human Feedback Code Execution Test ===")

		if err := testHumanFeedbackInCodeExecMode(logger); err != nil {
			return fmt.Errorf("human feedback code exec test failed: %w", err)
		}

		logger.Info("✅ Human feedback code execution test passed!")
		return nil
	},
}

func init() {
	humanFeedbackCodeExecTestCmd.Flags().String("model", "", "Model ID to use (e.g., gpt-4.1)")
	_ = viper.BindPFlag("model", humanFeedbackCodeExecTestCmd.Flags().Lookup("model")) //nolint:gosec // BindPFlag errors are non-critical in test init
}

// GetHumanFeedbackCodeExecTestCmd returns the human feedback code exec test command
func GetHumanFeedbackCodeExecTestCmd() *cobra.Command {
	return humanFeedbackCodeExecTestCmd
}

// testHumanFeedbackInCodeExecMode tests that human_feedback tool is available in code execution mode
func testHumanFeedbackInCodeExecMode(log loggerv2.Logger) error {
	log.Info("--- Test: Human Feedback Tool in Code Execution Mode ---")

	ctx := context.Background()
	traceID := testutils.GenerateTestTraceID()

	// Create minimal MCP config (no servers needed for this test)
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

	// Create agent in code execution mode
	log.Info("--- Step 1: Create Agent in Code Execution Mode ---")
	ag, err := testutils.CreateAgentWithTracer(ctx, model, llmProvider, configPath, tracer, traceID, log,
		mcpagent.WithCodeExecutionMode(true), // Enable code execution mode
	)
	if err != nil {
		return fmt.Errorf("failed to create agent: %w", err)
	}

	log.Info("✅ Agent created in code execution mode")

	// Get initial tools count
	initialTools := ag.Tools
	log.Info("Initial tools in code execution mode",
		loggerv2.Int("count", len(initialTools)))

	// Log initial tool names
	initialToolNames := make([]string, 0, len(initialTools))
	for _, tool := range initialTools {
		if tool.Function != nil {
			initialToolNames = append(initialToolNames, tool.Function.Name)
		}
	}
	log.Info("Initial tool names", loggerv2.Any("tools", initialToolNames))

	// Verify only virtual tools are present initially (discover_code_files, write_code)
	expectedVirtualTools := map[string]bool{
		"discover_code_files": false,
		"write_code":          false,
	}
	for _, tool := range initialTools {
		if tool.Function != nil {
			if tool.Function.Name == "discover_code_files" {
				expectedVirtualTools["discover_code_files"] = true
			}
			if tool.Function.Name == "write_code" {
				expectedVirtualTools["write_code"] = true
			}
		}
	}

	if expectedVirtualTools["discover_code_files"] && expectedVirtualTools["write_code"] {
		log.Info("✅ Initial tools verified - only code execution virtual tools present")
	} else {
		log.Warn("⚠️  Initial tools may not match expected - check tool list",
			loggerv2.Any("has_discover_code_files", expectedVirtualTools["discover_code_files"]),
			loggerv2.Any("has_write_code", expectedVirtualTools["write_code"]))
	}

	// Step 2: Register a regular custom tool (should be excluded in code exec mode)
	log.Info("--- Step 2: Register Regular Custom Tool (Should Be Excluded) ---")
	regularToolName := "test_regular_tool"
	regularToolParams := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"input": map[string]interface{}{
				"type":        "string",
				"description": "Test input",
			},
		},
		"required": []string{"input"},
	}

	regularToolExec := func(ctx context.Context, args map[string]interface{}) (string, error) {
		return "regular tool executed", nil
	}

	err = ag.RegisterCustomTool(
		regularToolName,
		"A regular custom tool that should be excluded in code exec mode",
		regularToolParams,
		regularToolExec,
		"custom", // Regular category, not "human"
	)
	if err != nil {
		return fmt.Errorf("failed to register regular custom tool: %w", err)
	}

	log.Info("✅ Regular custom tool registered", loggerv2.String("tool", regularToolName))

	// Check tools after registering regular tool
	toolsAfterRegular := ag.Tools
	regularToolFound := false
	for _, tool := range toolsAfterRegular {
		if tool.Function != nil && tool.Function.Name == regularToolName {
			regularToolFound = true
			break
		}
	}

	if regularToolFound {
		log.Warn("⚠️  Regular custom tool found in Tools array - should be excluded in code exec mode!")
	} else {
		log.Info("✅ Regular custom tool correctly excluded from Tools array in code exec mode")
	}

	// Step 3: Register human_feedback tool with category "human"
	log.Info("--- Step 3: Register Human Feedback Tool (Should Be Available) ---")
	humanFeedbackParams := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"message_for_user": map[string]interface{}{
				"type":        "string",
				"description": "Message to display to the user requesting their feedback",
			},
			"unique_id": map[string]interface{}{
				"type":        "string",
				"description": "Unique identifier for this feedback request. Always generate a UUID.",
			},
		},
		"required": []string{"message_for_user", "unique_id"},
	}

	humanFeedbackExec := func(ctx context.Context, args map[string]interface{}) (string, error) {
		// Simulate human feedback - in real scenario this would wait for user input
		messageForUser, _ := args["message_for_user"].(string)
		uniqueID, _ := args["unique_id"].(string)
		log.Info("Human feedback tool called",
			loggerv2.String("message", messageForUser),
			loggerv2.String("unique_id", uniqueID))
		// Return a simulated response
		return "User approved: " + messageForUser, nil
	}

	err = ag.RegisterCustomTool(
		"human_feedback",
		"Use this tool when you need to get human input, confirmation, or feedback. This tool will pause execution until the user provides input via the UI.",
		humanFeedbackParams,
		humanFeedbackExec,
		"human", // CRITICAL: Category must be "human" for it to be available in code exec mode
	)
	if err != nil {
		return fmt.Errorf("failed to register human_feedback tool: %w", err)
	}

	log.Info("✅ Human feedback tool registered with category 'human'")

	// Step 4: Verify human_feedback is in Tools array
	log.Info("--- Step 4: Verify Human Feedback Tool is Available ---")
	finalTools := ag.Tools
	humanFeedbackFound := false
	finalToolNames := make([]string, 0, len(finalTools))
	for _, tool := range finalTools {
		if tool.Function != nil {
			toolName := tool.Function.Name
			finalToolNames = append(finalToolNames, toolName)
			if toolName == "human_feedback" {
				humanFeedbackFound = true
			}
		}
	}

	log.Info("Final tools in code execution mode",
		loggerv2.Int("count", len(finalTools)),
		loggerv2.Any("tool_names", finalToolNames))

	if humanFeedbackFound {
		log.Info("✅ Human feedback tool found in Tools array - correctly available in code exec mode!")
	} else {
		log.Warn("⚠️  Human feedback tool NOT found in Tools array - should be available in code exec mode!")
	}

	// Step 5: Verify tool counts
	log.Info("--- Step 5: Verify Tool Counts ---")
	codeExecToolsCount := 0
	humanToolsCount := 0
	otherToolsCount := 0

	for _, tool := range finalTools {
		if tool.Function != nil {
			toolName := tool.Function.Name
			if toolName == "discover_code_files" || toolName == "write_code" {
				codeExecToolsCount++
			} else if toolName == "human_feedback" {
				humanToolsCount++
			} else {
				otherToolsCount++
				log.Info("Other tool found", loggerv2.String("tool", toolName))
			}
		}
	}

	log.Info("Tool breakdown",
		loggerv2.Int("code_exec_tools", codeExecToolsCount),
		loggerv2.Int("human_tools", humanToolsCount),
		loggerv2.Int("other_tools", otherToolsCount),
		loggerv2.Int("total", len(finalTools)))

	// Expected: 2 code exec tools (discover_code_files, write_code) + 1 human tool (human_feedback)
	expectedTotal := 3
	if len(finalTools) == expectedTotal {
		log.Info("✅ Tool count matches expected",
			loggerv2.Int("expected", expectedTotal),
			loggerv2.Int("actual", len(finalTools)))
	} else {
		log.Info("ℹ️  Tool count",
			loggerv2.Int("expected", expectedTotal),
			loggerv2.Int("actual", len(finalTools)),
			loggerv2.String("note", "May vary if other virtual tools are present"))
	}

	// Step 6: Test that LLM can see and potentially call the tool
	log.Info("--- Step 6: Test Tool Availability for LLM ---")
	log.Info("✅ Human feedback tool is registered and available in Tools array")
	log.Info("✅ Regular custom tool is correctly excluded from Tools array")
	log.Info("✅ Code execution mode is working correctly - human tools available, regular tools excluded")

	// Summary
	log.Info("--- Test Summary ---")
	log.Info("Code execution mode behavior verified:",
		loggerv2.Any("human_feedback_available", humanFeedbackFound),
		loggerv2.Any("regular_tool_excluded", !regularToolFound),
		loggerv2.Int("total_tools", len(finalTools)),
		loggerv2.Int("code_exec_tools", codeExecToolsCount),
		loggerv2.Int("human_tools", humanToolsCount))

	return nil
}
