package step_based_workflow

import (
	"context"
	"fmt"
	"strings"

	"mcp-agent-builder-go/agent_go/pkg/orchestrator/agents"
	mcpagent "github.com/manishiitg/mcpagent/agent"
	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"
	"github.com/manishiitg/mcpagent/observability"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

// Pre-parsed templates for conditional agent - panics at startup if invalid
var conditionalSystemTemplate = MustRegisterTemplate("conditionalSystem", `## 🤖 ROLE: Conditional Agent
**Task**: Evaluate a workflow condition (TRUE/FALSE).
**Constraint**: Context is historical. Use tools to verify CURRENT state.

{{if .CodeExecution}}
## ⚡ CODE EXECUTION
- Use 'execute_shell_command' with Python to verify state if needed.
- Follow safety rules (no destructive ops).
{{end}}

## 🔍 PROCESS
1. **Analyze**: Understand the question: {{.Description}}
2. **Verify**: Use tools to gather factual evidence.
3. **Decide**:
   - **TRUE**: Meets requirements.
   - **FALSE**: Does not meet requirements.

{{if .VariableValues}}
## 🔑 VARIABLES
{{.VariableNames}}
**Current Values**: {{.VariableValues}}
{{end}}

{{if .LearningHistory}}
## 📚 LEARNINGS (Historical)
{{.LearningHistory}}
{{end}}

## 📤 OUTPUT
Return ONLY JSON: {"result": true|false, "reason": "evidence-based explanation"}`)

var conditionalUserTemplate = MustRegisterTemplate("conditionalUser", `## 📝 TASK
**Condition**: {{.Question}}
**Context**: {{.ConditionContext}}

**MANDATORY**: Use tools to verify reality. Do NOT rely on historical context alone.
**Output**: JSON {"result": bool, "reason": "string"}`)

var decisionSystemTemplate = MustRegisterTemplate("decisionSystem", `## 🤖 ROLE: Decision Evaluator
**Task**: Analyze execution output vs. evaluation question.

## 🔍 PROCESS
1. **Analyze**: Review output for specific evidence.
2. **Compare**: Apply criteria from the question.
3. **Decide**: TRUE if criteria are clearly met.

{{if .VariableValues}}
## 🔑 VARIABLES
{{.VariableNames}}
**Current Values**: {{.VariableValues}}
{{end}}

{{if .LearningHistory}}
## 📚 LEARNINGS
{{.LearningHistory}}
{{end}}

## 📤 OUTPUT
Call 'submit_decision_result' with structured reasoning.`)

var decisionUserTemplate = MustRegisterTemplate("decisionUser", `## 📝 EVALUATION
**Question**: {{.Question}}
**Execution Output**: {{.ExecutionOutput}}

**Analyze and submit results via 'submit_decision_result'.**`)

var routingSystemTemplate = MustRegisterTemplate("routingSystem", `## 🤖 ROLE: Routing Evaluator
**Task**: Analyze context/output and select the best route from available options.

## 🔍 PROCESS
1. **Analyze**: Review the routing question and available routes.
2. **Evaluate**: Compare evidence against each route's condition.
3. **Select**: Pick the route whose condition best matches the evidence.

{{if .VariableValues}}
## 🔑 VARIABLES
{{.VariableNames}}
**Current Values**: {{.VariableValues}}
{{end}}

## 📤 OUTPUT
Call 'submit_routing_result' with the selected route and reasoning.`)

var routingUserTemplate = MustRegisterTemplate("routingUser", `## 📝 ROUTING EVALUATION
**Question**: {{.Question}}
{{if .ExecutionOutput}}**Execution Output**: {{.ExecutionOutput}}{{end}}
{{if .ConditionContext}}**Context**: {{.ConditionContext}}{{end}}

## 🔀 AVAILABLE ROUTES
{{.RoutesDescription}}

**Select the best matching route and submit via 'submit_routing_result'.**`)

// ConditionalResponse represents a true/false response with reasoning
type ConditionalResponse struct {
	Result bool   `json:"result"`
	Reason string `json:"reason"`
}

// GetResult returns the boolean result
func (cr *ConditionalResponse) GetResult() bool {
	return cr.Result
}

// DecisionResponse represents the structured response from decision evaluation
type DecisionResponse struct {
	Result    bool   `json:"result"`    // The decision result (true or false)
	Reasoning string `json:"reasoning"` // Detailed reasoning for the decision
}

// WorkflowConditionalAgent evaluates conditional decisions for step branching
type WorkflowConditionalAgent struct {
	*agents.BaseOrchestratorAgent
}

// NewWorkflowConditionalAgent creates a new conditional agent
func NewWorkflowConditionalAgent(config *agents.OrchestratorAgentConfig, logger loggerv2.Logger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) *WorkflowConditionalAgent {
	baseAgent := agents.NewBaseOrchestratorAgentWithEventBridge(
		config,
		logger,
		tracer,
		agents.ConditionalAgentType,
		eventBridge,
	)

	return &WorkflowConditionalAgent{
		BaseOrchestratorAgent: baseAgent,
	}
}

// Decide makes a true/false decision based on context and question
// Returns ConditionalResponse for backward compatibility with conditional steps
// variableNames: Variable names with descriptions ({{VAR_NAME}} - description)
// variableValues: Variable names with actual values ({{VAR_NAME}} = value - description)
func (hctpca *WorkflowConditionalAgent) conditionalSystemPromptProcessor(templateVars map[string]string, isCodeExecutionMode bool) string {
	templateData := map[string]interface{}{
		"Description":     templateVars["Description"],
		"LearningHistory": templateVars["LearningHistory"],
		"VariableNames":   templateVars["VariableNames"],
		"VariableValues":  templateVars["VariableValues"],
		"CodeExecution":   isCodeExecutionMode,
	}

	var result strings.Builder
	if err := conditionalSystemTemplate.Execute(&result, templateData); err != nil {
		return "Error executing conditional system prompt template: " + err.Error()
	}
	return result.String()
}

func (hctpca *WorkflowConditionalAgent) conditionalUserMessageProcessor(templateVars map[string]string) string {
	var result strings.Builder
	if err := conditionalUserTemplate.Execute(&result, templateVars); err != nil {
		return "Error executing conditional user message template: " + err.Error()
	}
	return result.String()
}

func (hctpca *WorkflowConditionalAgent) decisionSystemPromptProcessor(templateVars map[string]string) string {
	var result strings.Builder
	if err := decisionSystemTemplate.Execute(&result, templateVars); err != nil {
		return "Error executing decision system prompt template: " + err.Error()
	}
	return result.String()
}

func (hctpca *WorkflowConditionalAgent) decisionUserMessageProcessor(templateVars map[string]string) string {
	var result strings.Builder
	if err := decisionUserTemplate.Execute(&result, templateVars); err != nil {
		return "Error executing decision user message template: " + err.Error()
	}
	return result.String()
}

// Decide makes a true/false decision based on context and question
func (hctpca *WorkflowConditionalAgent) Decide(ctx context.Context, conditionContext, question, description string, stepIndex, iteration int, isCodeExecutionMode bool, learningHistory string, variableNames, variableValues string) (*ConditionalResponse, error) {
	templateVars := map[string]string{
		"ConditionContext": conditionContext,
		"Question":         question,
		"Description":      description,
		"LearningHistory":  learningHistory,
		"VariableNames":    variableNames,
		"VariableValues":   variableValues,
	}

	systemPrompt := hctpca.conditionalSystemPromptProcessor(templateVars, isCodeExecutionMode)
	inputProcessor := func(vars map[string]string) string {
		return hctpca.conditionalUserMessageProcessor(vars)
	}

	schema := `{
		"type": "object",
		"properties": {
			"result": {"type": "boolean"},
			"reason": {"type": "string"}
		},
		"required": ["result", "reason"]
	}`

	result, _, err := agents.ExecuteStructuredWithInputProcessor[ConditionalResponse](
		hctpca.BaseOrchestratorAgent,
		ctx,
		templateVars,
		inputProcessor,
		[]llmtypes.MessageContent{},
		schema,
		systemPrompt,
		isCodeExecutionMode,
	)

	if err != nil {
		return nil, fmt.Errorf("conditional decision failed: %w", err)
	}
	return &result, nil
}

// EvaluateDecision makes a structured decision evaluation for decision steps
func (hctpca *WorkflowConditionalAgent) EvaluateDecision(ctx context.Context, executionOutput, question string, stepIndex, iteration int, isCodeExecutionMode bool, learningHistory string, variableNames, variableValues string) (*DecisionResponse, error) {
	templateVars := map[string]string{
		"ExecutionOutput": executionOutput,
		"Question":        question,
		"LearningHistory": learningHistory,
		"VariableNames":   variableNames,
		"VariableValues":  variableValues,
	}

	systemPrompt := hctpca.decisionSystemPromptProcessor(templateVars)
	inputProcessor := func(vars map[string]string) string {
		return hctpca.decisionUserMessageProcessor(vars)
	}

	schema := `{
		"type": "object",
		"properties": {
			"result": {"type": "boolean"},
			"reasoning": {"type": "string"}
		},
		"required": ["result", "reasoning"]
	}`

	result, _, err := agents.ExecuteStructuredWithInputProcessorViaTool[DecisionResponse](
		hctpca.BaseOrchestratorAgent,
		ctx,
		templateVars,
		inputProcessor,
		[]llmtypes.MessageContent{},
		schema,
		systemPrompt,
		isCodeExecutionMode,
		"submit_decision_result",
		"Submit the decision evaluation result.",
	)

	if err != nil {
		return nil, fmt.Errorf("decision evaluation failed: %w", err)
	}
	return &result, nil
}

// EvaluateRouting evaluates a routing question and selects one of N routes
// executionOutput is non-empty for execute-then-route mode, conditionContext is for pure routing mode
func (hctpca *WorkflowConditionalAgent) EvaluateRouting(ctx context.Context, executionOutput, conditionContext, question string, routes []RoutingRoute, stepIndex, iteration int, isCodeExecutionMode bool, variableNames, variableValues string) (*RoutingResponse, error) {
	// Build routes description
	var routesDesc strings.Builder
	routeIDList := make([]string, 0, len(routes))
	for i, route := range routes {
		routesDesc.WriteString(fmt.Sprintf("%d. **%s** (route_id: `%s`)\n   Condition: %s\n   Routes to: %s\n\n", i+1, route.RouteName, route.RouteID, route.Condition, route.NextStepID))
		routeIDList = append(routeIDList, route.RouteID)
	}

	templateVars := map[string]string{
		"ExecutionOutput":   executionOutput,
		"ConditionContext":  conditionContext,
		"Question":          question,
		"RoutesDescription": routesDesc.String(),
		"VariableNames":     variableNames,
		"VariableValues":    variableValues,
	}

	systemPrompt := hctpca.routingSystemPromptProcessor(templateVars)
	inputProcessor := func(vars map[string]string) string {
		return hctpca.routingUserMessageProcessor(vars)
	}

	// Build enum constraint for selected_route_id
	enumJSON := "["
	for i, id := range routeIDList {
		if i > 0 {
			enumJSON += ","
		}
		enumJSON += fmt.Sprintf("%q", id)
	}
	enumJSON += "]"

	schema := fmt.Sprintf(`{
		"type": "object",
		"properties": {
			"selected_route_id": {"type": "string", "enum": %s},
			"reasoning": {"type": "string"}
		},
		"required": ["selected_route_id", "reasoning"]
	}`, enumJSON)

	result, _, err := agents.ExecuteStructuredWithInputProcessorViaTool[RoutingResponse](
		hctpca.BaseOrchestratorAgent,
		ctx,
		templateVars,
		inputProcessor,
		[]llmtypes.MessageContent{},
		schema,
		systemPrompt,
		isCodeExecutionMode,
		"submit_routing_result",
		"Submit the routing evaluation result.",
	)

	if err != nil {
		return nil, fmt.Errorf("routing evaluation failed: %w", err)
	}
	return &result, nil
}

func (hctpca *WorkflowConditionalAgent) routingSystemPromptProcessor(templateVars map[string]string) string {
	var result strings.Builder
	if err := routingSystemTemplate.Execute(&result, templateVars); err != nil {
		return "Error executing routing system prompt template: " + err.Error()
	}
	return result.String()
}

func (hctpca *WorkflowConditionalAgent) routingUserMessageProcessor(templateVars map[string]string) string {
	var result strings.Builder
	if err := routingUserTemplate.Execute(&result, templateVars); err != nil {
		return "Error executing routing user message template: " + err.Error()
	}
	return result.String()
}

// Execute implements the OrchestratorAgent interface
// NOTE: This method is NOT USED - use Decide() instead
func (hctpca *WorkflowConditionalAgent) Execute(ctx context.Context, templateVars map[string]string, conversationHistory []llmtypes.MessageContent) (string, []llmtypes.MessageContent, error) {
	return "", nil, fmt.Errorf("Execute() is not used for conditional agent - use Decide() instead")
}
