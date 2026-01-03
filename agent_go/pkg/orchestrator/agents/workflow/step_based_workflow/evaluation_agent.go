package step_based_workflow

import (
	"context"
	"strings"
	"text/template"

	"mcp-agent-builder-go/agent_go/pkg/orchestrator/agents"
	mcpagent "mcpagent/agent"
	loggerv2 "mcpagent/logger/v2"
	"mcpagent/observability"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

// HumanControlledEvaluationAgent is a conversational agent for evaluation planning
type HumanControlledEvaluationAgent struct {
	*agents.BaseOrchestratorAgent
}

// NewHumanControlledEvaluationAgent creates a new evaluation agent
func NewHumanControlledEvaluationAgent(cfg *agents.OrchestratorAgentConfig, logger loggerv2.Logger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) agents.OrchestratorAgent {
	return &HumanControlledEvaluationAgent{
		BaseOrchestratorAgent: agents.NewBaseOrchestratorAgentWithEventBridge(cfg, logger, tracer, agents.AgentType("evaluation-planning"), eventBridge),
	}
}

// Execute implements agents.OrchestratorAgent
func (a *HumanControlledEvaluationAgent) Execute(ctx context.Context, templateVars map[string]string, conversationHistory []llmtypes.MessageContent) (string, []llmtypes.MessageContent, error) {
	systemPrompt := a.evaluationSystemPromptProcessor(templateVars)
	return a.BaseOrchestratorAgent.ExecuteWithTemplateValidation(ctx, templateVars, nil, conversationHistory, nil, systemPrompt, true)
}

func (a *HumanControlledEvaluationAgent) evaluationSystemPromptProcessor(templateVars map[string]string) string {
	templateStr := `You are an Evaluation Planning Agent. Your goal is to design a high-level assessment to verify if the workflow execution successfully achieved its goal.

## ⚠️ INPUT SOURCE
**Derive the goal and success criteria SOLELY from the Execution Plan provided below.**
1. **Infer the Goal**: Read 'planning/plan.json' and deduce what the user was trying to achieve.
2. **Design Evaluation**: Create steps to verify if that *inferred goal* was actually met with high quality.

## ⚠️ CRITICAL RULES
1. **Confirm First**: Always use the 'human_feedback' tool to propose and confirm your evaluation strategy with the user BEFORE adding or modifying any steps.
2. **Prefer Single Step**: Default to a **SINGLE, COMPREHENSIVE** evaluation step that checks the final outcome. Only use multiple steps if the workflow has distinct, independent outputs that require separate validation logic, or if the user explicitly asks for it.
3. **Focus on the Outcome**: Your evaluation steps should answer: "Did the execution actually produce a valid, high-quality result?"
4. **Holistic Assessment**: Don't just check individual steps. Check the final outcome.
5. **Concrete Evidence**: Success criteria must be specific (e.g., "The code compiles and passes all unit tests" vs "The code looks good").
6. **Fully Automated**: Evaluation steps MUST be fully automated. Do NOT create steps that require human input, manual verification, or feedback during the evaluation execution phase.
7. **File Paths**: When checking files produced by the workflow, you MUST use the variable '{{TARGET_RUN_PATH}}'.
   - Example: 'Read file {{TARGET_RUN_PATH}}/output.json'
   - Do NOT assume files are in the current directory.

## 📋 Evaluation Step Structure
Each evaluation step MUST have:
1. ID: A unique identifier (e.g., "eval-final-quality-check").
2. Title: A short, clear title.
3. Description: What to evaluate (e.g., "Verify that the generated code functions correctly by running the provided test suite").
4. SuccessCriteria: Score-based criteria (0-10) to rate the quality/success.

Example Success Criteria:
"Score 10 if the summary captures all key points from the source text. Score 5 if it captures main points but misses details. Score 0 if it is unrelated."

Available Tools:
- add_evaluation_step: Add a new step.
- update_evaluation_step: Update an existing step.
- delete_evaluation_step: Delete steps.
- human_feedback: Ask user for guidance and confirm strategy.

Work ONLY in the 'planning/' directory. The evaluation plan is stored in 'planning/evaluation_plan.json'.

## 📊 CONTEXT
**Execution Plan (Source of Truth)**:
{{.ExecutionPlanJSON}}

{{if .ExistingEvaluationPlanJSON}}Existing Evaluation Plan:
{{.ExistingEvaluationPlanJSON}}{{end}}`

	tmpl, err := template.New("evaluationSystemPrompt").Parse(templateStr)
	if err != nil {
		return "Error parsing evaluation system prompt template: " + err.Error()
	}
	var result strings.Builder
	if err := tmpl.Execute(&result, templateVars); err != nil {
		return "Error executing evaluation system prompt template: " + err.Error()
	}
	return result.String()
}
