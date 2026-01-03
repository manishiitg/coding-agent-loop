package step_based_workflow

import (
	"context"
	"fmt"
	"strings"
	"text/template"

	"mcp-agent-builder-go/agent_go/pkg/orchestrator/agents"
	mcpagent "mcpagent/agent"
	loggerv2 "mcpagent/logger/v2"
	"mcpagent/observability"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

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

	templateStr := `## 🤖 ROLE: Conditional Agent
**Task**: Evaluate a workflow condition (TRUE/FALSE).
**Constraint**: Context is historical. Use tools to verify CURRENT state.

{{if .CodeExecution}}
## ⚡ CODE EXECUTION
- Use 'write_code' to verify state if needed.
- Follow Go safety rules (no destructive ops).
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

## 📚 LEARNINGS (Historical)
{{.LearningHistory}}

## 📤 OUTPUT
Return ONLY JSON: {"result": true|false, "reason": "evidence-based explanation"}`

	tmpl, err := template.New("conditionalSystem").Parse(templateStr)
	if err != nil {
		return "Error parsing conditional system prompt template: " + err.Error()
	}
	var result strings.Builder
	if err := tmpl.Execute(&result, templateData); err != nil {
		return "Error executing conditional system prompt template: " + err.Error()
	}
	return result.String()
}

func (hctpca *WorkflowConditionalAgent) conditionalUserMessageProcessor(templateVars map[string]string) string {
	templateStr := `## 📝 TASK
**Condition**: {{.Question}}
**Context**: {{.ConditionContext}}

**MANDATORY**: Use tools to verify reality. Do NOT rely on historical context alone.
**Output**: JSON {"result": bool, "reason": "string"}`

	tmpl, err := template.New("conditionalUser").Parse(templateStr)
	if err != nil {
		return "Error parsing conditional user message template: " + err.Error()
	}
	var result strings.Builder
	if err := tmpl.Execute(&result, templateVars); err != nil {
		return "Error executing conditional user message template: " + err.Error()
	}
	return result.String()
}

func (hctpca *WorkflowConditionalAgent) decisionSystemPromptProcessor(templateVars map[string]string) string {
	templateStr := `## 🤖 ROLE: Decision Evaluator
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

## 📚 LEARNINGS
{{.LearningHistory}}

## 📤 OUTPUT
Call 'submit_decision_result' with structured reasoning.`

	tmpl, err := template.New("decisionSystem").Parse(templateStr)
	if err != nil {
		return "Error parsing decision system prompt template: " + err.Error()
	}
	var result strings.Builder
	if err := tmpl.Execute(&result, templateVars); err != nil {
		return "Error executing decision system prompt template: " + err.Error()
	}
	return result.String()
}

func (hctpca *WorkflowConditionalAgent) decisionUserMessageProcessor(templateVars map[string]string) string {
	templateStr := `## 📝 EVALUATION
**Question**: {{.Question}}
**Execution Output**: {{.ExecutionOutput}}

**Analyze and submit results via 'submit_decision_result'.**`

	tmpl, err := template.New("decisionUser").Parse(templateStr)
	if err != nil {
		return "Error parsing decision user message template: " + err.Error()
	}
	var result strings.Builder
	if err := tmpl.Execute(&result, templateVars); err != nil {
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

// Execute implements the OrchestratorAgent interface
// NOTE: This method is NOT USED - use Decide() instead
func (hctpca *WorkflowConditionalAgent) Execute(ctx context.Context, templateVars map[string]string, conversationHistory []llmtypes.MessageContent) (string, []llmtypes.MessageContent, error) {
	return "", nil, fmt.Errorf("Execute() is not used for conditional agent - use Decide() instead")
}
