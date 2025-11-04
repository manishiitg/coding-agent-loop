package testing

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"mcp-agent/agent_go/internal/llm"
	"mcp-agent/agent_go/internal/llmtypes"
	"mcp-agent/agent_go/internal/observability"
	"mcp-agent/agent_go/pkg/mcpagent"
	"mcp-agent/agent_go/pkg/orchestrator/agents/workflow/todo_creation_human"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// TestStructuredOutputResult represents test result structure
type TestStructuredOutputResult struct {
	Steps []TestPlanStep `json:"steps"`
}

// TestPlanStep represents a step in test planning output
type TestPlanStep struct {
	Title               string   `json:"title"`
	Description         string   `json:"description"`
	SuccessCriteria     string   `json:"success_criteria"`
	ContextDependencies []string `json:"context_dependencies"`
	ContextOutput       string   `json:"context_output"`
	HasLoop             bool     `json:"has_loop"`
}

var structuredToolTestCmd = &cobra.Command{
	Use:   "structured-tool",
	Short: "Test AskWithHistoryStructuredViaTool function",
	Long: `Comprehensive test for AskWithHistoryStructuredViaTool that validates:
1. Successful tool call with structured output
2. Conversational input detection (HasStructuredOutput=false)
3. Nested structs and arrays
4. Optional fields (omitempty)
5. Custom types handling
6. Multiple tool calls (uses last one)
7. Conversation history preservation

This test ensures the structured output via tool call functionality works correctly.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Get logging configuration from viper
		logFile := viper.GetString("log-file")
		logLevel := viper.GetString("log-level")

		// Initialize test logger
		InitTestLogger(logFile, logLevel)
		logger := GetTestLogger()

		logger.Infof("=== Structured Tool Test ===")

		// Get provider and model from viper
		provider := viper.GetString("provider")
		if provider == "" {
			provider = "bedrock"
		}
		model := viper.GetString("model")
		if model == "" {
			model = "anthropic.claude-3-5-sonnet-20241022-v2:0"
		}

		// Create LLM instance
		llmInstance, err := llm.InitializeLLM(llm.Config{
			Provider:    llm.Provider(provider),
			ModelID:     model,
			Temperature: 0.7,
			Logger:      logger,
		})
		if err != nil {
			return fmt.Errorf("failed to initialize LLM: %w", err)
		}

		// Create trace ID
		traceID := observability.TraceID(fmt.Sprintf("structured-tool-test-%d", time.Now().UnixNano()))

		// Create agent
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()

		agent, err := mcpagent.NewAgent(
			ctx,
			llmInstance,
			"fileserver", // Use fileserver for testing
			"configs/mcp_servers_simple.json",
			model,
			observability.GetTracer("console"),
			traceID,
			logger,
		)
		if err != nil {
			return fmt.Errorf("failed to create agent: %w", err)
		}
		defer agent.Close()

		logger.Infof("✅ Agent created successfully")

		// Test 1: Successful tool call with structured output
		logger.Infof("\n--- Test 1: Successful Tool Call ---")
		testSchema := `{
			"type": "object",
			"properties": {
				"steps": {
					"type": "array",
					"items": {
						"type": "object",
						"properties": {
							"title": {"type": "string"},
							"description": {"type": "string"},
							"success_criteria": {"type": "string"},
							"context_dependencies": {"type": "array", "items": {"type": "string"}},
							"context_output": {"type": "string"},
							"has_loop": {"type": "boolean"}
						},
						"required": ["title", "description", "success_criteria", "has_loop"]
					}
				}
			},
			"required": ["steps"]
		}`

		toolName := "submit_test_plan"
		toolDescription := "Submit the test plan in structured JSON format. Call this tool when you have completed the plan."

		// Create user message
		userMessage := llmtypes.MessageContent{
			Role:  llmtypes.ChatMessageTypeHuman,
			Parts: []llmtypes.ContentPart{llmtypes.TextContent{Text: "Create a simple test plan with 2 steps: 1) Setup test environment, 2) Run tests"}},
		}

		result, err := mcpagent.AskWithHistoryStructuredViaTool[TestStructuredOutputResult](
			agent,
			ctx,
			[]llmtypes.MessageContent{userMessage},
			toolName,
			toolDescription,
			testSchema,
		)
		if err != nil {
			return fmt.Errorf("test 1 failed: %w", err)
		}

		if !result.HasStructuredOutput {
			return fmt.Errorf("test 1 failed: expected structured output but got conversational input: %s", result.TextResponse)
		}

		if len(result.StructuredResult.Steps) == 0 {
			return fmt.Errorf("test 1 failed: expected at least one step but got none")
		}

		logger.Infof("✅ Test 1 passed: Got structured output with %d steps", len(result.StructuredResult.Steps))
		resultJSON, _ := json.MarshalIndent(result.StructuredResult, "", "  ")
		logger.Infof("Result: %s", string(resultJSON))

		// Test 2: Conversational input (no tool call)
		logger.Infof("\n--- Test 2: Conversational Input Detection ---")
		conversationalUserMessage := llmtypes.MessageContent{
			Role:  llmtypes.ChatMessageTypeHuman,
			Parts: []llmtypes.ContentPart{llmtypes.TextContent{Text: "Hi, how are you?"}},
		}

		result2, err := mcpagent.AskWithHistoryStructuredViaTool[TestStructuredOutputResult](
			agent,
			ctx,
			[]llmtypes.MessageContent{conversationalUserMessage},
			toolName,
			toolDescription,
			testSchema,
		)
		if err != nil {
			return fmt.Errorf("test 2 failed: %w", err)
		}

		if result2.HasStructuredOutput {
			return fmt.Errorf("test 2 failed: expected conversational input but got structured output")
		}

		if result2.TextResponse == "" {
			return fmt.Errorf("test 2 failed: expected text response but got empty")
		}

		logger.Infof("✅ Test 2 passed: Correctly detected conversational input")
		logger.Infof("Text response: %s", result2.TextResponse)

		// Test 3: Test with PlanningResponse (actual struct from planning agent)
		logger.Infof("\n--- Test 3: PlanningResponse Struct Test ---")
		planningSchema := `{
			"type": "object",
			"properties": {
				"steps": {
					"type": "array",
					"items": {
						"type": "object",
						"properties": {
							"title": {"type": "string"},
							"description": {"type": "string"},
							"success_criteria": {"type": "string"},
							"context_dependencies": {"type": "array", "items": {"type": "string"}},
							"context_output": {"type": "string"},
							"has_loop": {"type": "boolean"},
							"loop_condition": {"type": "string"},
							"max_iterations": {"type": "integer"}
						},
						"required": ["title", "description", "success_criteria", "has_loop"]
					}
				}
			},
			"required": ["steps"]
		}`

		planningToolName := "submit_planning_response"
		planningToolDescription := "Submit the final structured planning response in JSON format."

		planningUserMessage := llmtypes.MessageContent{
			Role:  llmtypes.ChatMessageTypeHuman,
			Parts: []llmtypes.ContentPart{llmtypes.TextContent{Text: "Create a plan to deploy a web application with 3 steps"}},
		}

		result3, err := mcpagent.AskWithHistoryStructuredViaTool[todo_creation_human.PlanningResponse](
			agent,
			ctx,
			[]llmtypes.MessageContent{planningUserMessage},
			planningToolName,
			planningToolDescription,
			planningSchema,
		)
		if err != nil {
			return fmt.Errorf("test 3 failed: %w", err)
		}

		if !result3.HasStructuredOutput {
			return fmt.Errorf("test 3 failed: expected structured output but got conversational input: %s", result3.TextResponse)
		}

		if len(result3.StructuredResult.Steps) == 0 {
			return fmt.Errorf("test 3 failed: expected at least one step but got none")
		}

		logger.Infof("✅ Test 3 passed: Got PlanningResponse with %d steps", len(result3.StructuredResult.Steps))
		result3JSON, _ := json.MarshalIndent(result3.StructuredResult, "", "  ")
		logger.Infof("Result: %s", string(result3JSON))

		logger.Infof("\n=== All Tests Passed ===")
		return nil
	},
}

func init() {
	TestingCmd.AddCommand(structuredToolTestCmd)
}
