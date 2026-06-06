package step_based_workflow

import (
	"context"
	"errors"
	"fmt"
	"strings"

	mcpagent "github.com/manishiitg/mcpagent/agent"
	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"
	"github.com/manishiitg/mcpagent/observability"
	"mcp-agent-builder-go/agent_go/pkg/orchestrator/agents"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

// Pre-parsed templates for routing evaluation - panics at startup if invalid
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

// WorkflowConditionalAgent evaluates routing decisions for step branching
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

// EvaluateRouting is the legacy LLM routing helper. Workflow routing is now
// deterministic and does not call this path.
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
		if isWorkflowCancellationErr(ctx, err) {
			return nil, context.Canceled
		}
		if recovered := recoverRoutingResponseFromText(err, routes); recovered != nil {
			return recovered, nil
		}
		return nil, fmt.Errorf("routing evaluation failed: %w", err)
	}
	return &result, nil
}

func recoverRoutingResponseFromText(err error, routes []RoutingRoute) *RoutingResponse {
	var nonStructured *agents.NonStructuredResponseError
	if !errors.As(err, &nonStructured) {
		return nil
	}

	answer := strings.TrimSpace(nonStructured.TextResponse)
	answer = strings.Trim(answer, "`\"'")
	if answer == "" {
		return nil
	}

	for _, route := range routes {
		if strings.EqualFold(answer, route.RouteID) || strings.EqualFold(answer, route.RouteName) {
			return &RoutingResponse{
				SelectedRouteID: route.RouteID,
				Reasoning:       answer,
			}
		}
	}

	lowered := strings.ToLower(answer)
	for _, route := range routes {
		if strings.Contains(lowered, strings.ToLower(route.RouteID)) || strings.Contains(lowered, strings.ToLower(route.RouteName)) {
			return &RoutingResponse{
				SelectedRouteID: route.RouteID,
				Reasoning:       answer,
			}
		}
	}

	return nil
}

func (hctpca *WorkflowConditionalAgent) routingSystemPromptProcessor(templateVars map[string]string) string {
	var result strings.Builder
	if err := routingSystemTemplate.Execute(&result, templateVars); err != nil {
		panic(fmt.Sprintf("routing system prompt template execution failed (missing variable?): %v", err))
	}
	return result.String()
}

func (hctpca *WorkflowConditionalAgent) routingUserMessageProcessor(templateVars map[string]string) string {
	var result strings.Builder
	if err := routingUserTemplate.Execute(&result, templateVars); err != nil {
		panic(fmt.Sprintf("routing user message template execution failed (missing variable?): %v", err))
	}
	return result.String()
}

// Execute implements the OrchestratorAgent interface
// NOTE: This method is NOT USED - use EvaluateRouting() instead
func (hctpca *WorkflowConditionalAgent) Execute(ctx context.Context, templateVars map[string]string, conversationHistory []llmtypes.MessageContent) (string, []llmtypes.MessageContent, error) {
	return "", nil, fmt.Errorf("Execute() is not used for routing agent - use EvaluateRouting() instead")
}
