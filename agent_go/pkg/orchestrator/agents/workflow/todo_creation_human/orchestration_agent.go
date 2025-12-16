package todo_creation_human

import (
	"context"
	"fmt"

	"mcp-agent-builder-go/agent_go/pkg/orchestrator/agents"
	mcpagent "mcpagent/agent"
	loggerv2 "mcpagent/logger/v2"
	"mcpagent/observability"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

// HumanControlledTodoPlannerOrchestrationAgent evaluates orchestration decisions for orchestration steps
type HumanControlledTodoPlannerOrchestrationAgent struct {
	*agents.BaseOrchestratorAgent
}

// NewHumanControlledTodoPlannerOrchestrationAgent creates a new orchestration agent
func NewHumanControlledTodoPlannerOrchestrationAgent(config *agents.OrchestratorAgentConfig, logger loggerv2.Logger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) *HumanControlledTodoPlannerOrchestrationAgent {
	baseAgent := agents.NewBaseOrchestratorAgentWithEventBridge(
		config,
		logger,
		tracer,
		agents.OrchestrationAgentType,
		eventBridge,
	)

	return &HumanControlledTodoPlannerOrchestrationAgent{
		BaseOrchestratorAgent: baseAgent,
	}
}

// EvaluateOrchestration evaluates orchestration step output to select a route and check success criteria
// Returns OrchestrationResponse with selected route and success evaluation
func (hctpoa *HumanControlledTodoPlannerOrchestrationAgent) EvaluateOrchestration(
	ctx context.Context,
	executionOutput string,
	evaluationQuestion string,
	routes []OrchestrationRoute,
	successCriteria string,
	stepIndex int,
	iteration int,
	learningHistory string,
	conversationHistory []llmtypes.MessageContent,
) (*OrchestrationResponse, error) {
	// Build routes description for prompt
	routesDescription := ""
	for i, route := range routes {
		routesDescription += fmt.Sprintf("\n**Route %d: %s** (ID: %s)\n", i+1, route.RouteName, route.RouteID)
		routesDescription += fmt.Sprintf("- Condition: %s\n", route.Condition)
		if route.ContextToPass != "" {
			routesDescription += fmt.Sprintf("- Context to pass: %s\n", route.ContextToPass)
		}
	}

	// Build template variables
	templateVars := map[string]string{
		"ExecutionOutput":    executionOutput,
		"EvaluationQuestion": evaluationQuestion,
		"RoutesDescription":  routesDescription,
		"SuccessCriteria":    successCriteria,
		"StepIndex":          fmt.Sprintf("%d", stepIndex),
		"Iteration":          fmt.Sprintf("%d", iteration),
		"LearningHistory":    learningHistory,
	}

	// Build system prompt for orchestration evaluation
	systemPrompt := fmt.Sprintf(`# Orchestration Evaluation Agent

You are an expert orchestration evaluation agent specialized in analyzing execution outputs and making structured orchestration decisions. Your role is to evaluate orchestration step execution results and provide comprehensive structured analysis.

## 🔍 ORCHESTRATION EVALUATION FRAMEWORK

### Step 1: Analyze Execution Output
**Review the execution output:** What was the result of the orchestration step's execution?

**IMPORTANT**: You have access to workspace tools, human tools, and MCP tools. If you need additional information to properly evaluate the execution output, use these tools to:
- Read files from the workspace to understand context
- Query systems via MCP tools to verify state
- Request human feedback if needed for clarification
- Gather any additional information required for accurate evaluation

### Step 2: Understand the Evaluation Question
**Analyze the question:** What specific condition or criteria needs to be evaluated for orchestration?

### Step 3: Evaluate Success Criteria
**Success Criteria**: %s

**Evaluation Strategy:**
- Analyze the execution output against the success criteria
- **Use tools if needed**: If the execution output is unclear or you need to verify the current state, use workspace tools, MCP tools, or request human feedback
- Determine if the success criteria is met
- If met, validation will be called as a sub-agent to verify the success
- If not met, proceed to route selection

### Step 3a: Handle Validation Response (if success criteria was met)
**When validation response is received:**
- Review the validation response in the conversation history
- Check if validation confirmed that success criteria is met (is_success_criteria_met: true)
- Set **success_criteria_verified_by_validation**: true only if validation confirms success
- If validation does not confirm success, set success_criteria_verified_by_validation: false and proceed to route selection

### Step 4: Route Selection (if success criteria not met)
**Available Routes:**
%s

**Route Selection Strategy:**
- Compare execution output against each route's condition
- **Use tools if needed**: If you need more information to match the output to a route, use available tools to gather context
- Select the route whose condition best matches the execution output
- If multiple routes match, select the most specific/relevant one
- If no route matches exactly, select the closest match

### Step 5: Structured Response
**Response Requirements:**
- **success_criteria_met**: true if success criteria is met, false otherwise
- **selected_route_id**: ID of the route to execute (required if success_criteria_met is false, empty string if success_criteria_met is true)
- **reasoning**: Detailed explanation of route selection and success evaluation
- **success_reasoning**: Detailed explanation of success criteria evaluation (if success_criteria_met is true)
- **success_criteria_verified_by_validation**: true only if validation agent confirmed success criteria is met (required if success_criteria_met is true and validation response has been received, false otherwise)

## 📋 OUTPUT REQUIREMENTS

**USE THE 'submit_orchestration_result' TOOL TO SUBMIT YOUR ORCHESTRATION ANALYSIS**

You MUST call the 'submit_orchestration_result' tool with your structured orchestration response. Do NOT return JSON directly in your response - use the tool instead.

The tool accepts a structured object with:
- selected_route_id: string - ID of the route to execute (required if success_criteria_met is false, empty string if success_criteria_met is true)
- reasoning: string - Detailed reasoning explaining route selection and evaluation
- success_criteria_met: boolean - Whether the success criteria is met
- success_reasoning: string - Detailed reasoning for success criteria evaluation (required if success_criteria_met is true)
- success_criteria_verified_by_validation: boolean - Whether validation confirmed success (required if success_criteria_met is true and validation response received, false otherwise)

**Example JSON structure (before validation):**
`+"```json"+`
{
  "selected_route_id": "",
  "reasoning": "The execution output meets all success criteria.",
  "success_criteria_met": true,
  "success_reasoning": "All requirements have been fulfilled.",
  "success_criteria_verified_by_validation": false
}
`+"```"+`

**Example JSON structure (after validation confirms):**
`+"```json"+`
{
  "selected_route_id": "",
  "reasoning": "Validation confirmed that success criteria is met.",
  "success_criteria_met": true,
  "success_reasoning": "All requirements have been fulfilled and validated.",
  "success_criteria_verified_by_validation": true
}
`+"```"+`

**Example JSON structure (route selection):**
`+"```json"+`
{
  "selected_route_id": "auth-error",
  "reasoning": "The execution output shows an authentication error. Route 'auth-error' matches this condition.",
  "success_criteria_met": false,
  "success_reasoning": "",
  "success_criteria_verified_by_validation": false
}
`+"```"+`

**CRITICAL**: You MUST call the 'submit_orchestration_result' tool with your orchestration analysis. The tool will be available to you - use it to submit your structured orchestration response.

## 📚 LEARNING CONTEXT (Reference Only)
%s

**Learning Usage Guidelines:**
- Use learnings to understand typical orchestration patterns
- Reference learnings for decision-making strategies, not as current state
- Verify that learning patterns apply to current execution output
`, successCriteria, routesDescription, learningHistory)

	// Build user message input processor
	inputProcessor := func(vars map[string]string) string {
		return fmt.Sprintf(`## 📝 ORCHESTRATION EVALUATION TASK

**Execution Output** (from orchestration step execution):
%s`, vars["ExecutionOutput"])
	}

	// Build schema for structured output
	schema := `{
		"type": "object",
		"properties": {
			"selected_route_id": {
				"type": "string",
				"description": "ID of the route to execute. Required if success_criteria_met is false, empty string if success_criteria_met is true."
			},
			"reasoning": {
				"type": "string",
				"description": "Detailed reasoning explaining route selection and evaluation process"
			},
			"success_criteria_met": {
				"type": "boolean",
				"description": "Whether the main orchestration step's success criteria is met"
			},
			"success_reasoning": {
				"type": "string",
				"description": "Detailed reasoning for success criteria evaluation. Required if success_criteria_met is true."
			},
			"success_criteria_verified_by_validation": {
				"type": "boolean",
				"description": "Whether validation confirmed that success criteria is met. This is set to true only after validation agent confirms success. Required if success_criteria_met is true and validation response has been received."
			}
		},
		"required": ["selected_route_id", "reasoning", "success_criteria_met", "success_reasoning", "success_criteria_verified_by_validation"]
	}`

	// Define tool name and description for structured output via tool calls
	toolName := "submit_orchestration_result"
	toolDescription := "Submit the orchestration evaluation result. This tool should be called with the structured orchestration response containing the selected route, reasoning, and success criteria evaluation."

	// Use ExecuteStructuredWithInputProcessorViaTool similar to validation agent
	result, _, err := agents.ExecuteStructuredWithInputProcessorViaTool[OrchestrationResponse](
		hctpoa.BaseOrchestratorAgent,
		ctx,
		templateVars,
		inputProcessor,
		conversationHistory,
		schema,
		systemPrompt,
		false, // Append to system prompt (don't overwrite)
		toolName,
		toolDescription,
	)

	if err != nil {
		return nil, fmt.Errorf("orchestration evaluation failed: %w", err)
	}

	return &result, nil
}

// Execute implements the OrchestratorAgent interface
// NOTE: This method is NOT USED - use EvaluateOrchestration() instead
func (hctpoa *HumanControlledTodoPlannerOrchestrationAgent) Execute(ctx context.Context, templateVars map[string]string, conversationHistory []llmtypes.MessageContent) (string, []llmtypes.MessageContent, error) {
	return "", nil, fmt.Errorf("Execute() is not used for orchestration agent - use EvaluateOrchestration() instead")
}
