package step_based_workflow

import (
	"context"
	"fmt"
	"strings"
	"time"

	"mcp-agent-builder-go/agent_go/pkg/orchestrator/agents"
	mcpagent "github.com/manishiitg/mcpagent/agent"
	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"
	"github.com/manishiitg/mcpagent/observability"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

// WorkflowEvaluationScoringAgent is an agent that calculates scores for ALL evaluation steps
// in a single call, providing holistic analysis across all steps.
type WorkflowEvaluationScoringAgent struct {
	*agents.BaseOrchestratorAgent
}

// NewWorkflowEvaluationScoringAgent creates a new evaluation scoring agent
func NewWorkflowEvaluationScoringAgent(config *agents.OrchestratorAgentConfig, logger loggerv2.Logger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) *WorkflowEvaluationScoringAgent {
	baseAgent := agents.NewBaseOrchestratorAgentWithEventBridge(
		config,
		logger,
		tracer,
		agents.EvaluationScoringAgentType,
		eventBridge,
	)
	return &WorkflowEvaluationScoringAgent{
		BaseOrchestratorAgent: baseAgent,
	}
}

// Execute implements the OrchestratorAgent interface
func (a *WorkflowEvaluationScoringAgent) Execute(ctx context.Context, templateVars map[string]string, conversationHistory []llmtypes.MessageContent) (string, []llmtypes.MessageContent, error) {
	systemPrompt := a.GetSystemPrompt()

	// Append code execution instructions so the LLM knows to use get_api_spec + execute_shell_command
	// to call submit_score via HTTP API (same pattern as other agents via {{.CodeExecutionSection}})
	if a.GetConfig() != nil && a.GetConfig().UseCodeExecutionMode {
		codeExecSection := BuildCodeExecutionSection(true, false, "")
		if codeExecSection != "" {
			systemPrompt += "\n\n" + codeExecSection
		}
	}

	return a.BaseOrchestratorAgent.ExecuteWithTemplateValidation(ctx, templateVars, nil, conversationHistory, nil, systemPrompt, true)
}

// GetSystemPrompt returns the system prompt for the evaluation scoring agent
func (a *WorkflowEvaluationScoringAgent) GetSystemPrompt() string {
	return `You are an evaluation scoring agent. Your task is to analyze execution outputs from ALL evaluation steps and determine scores for each one.

## Your Task
You will receive all evaluation steps with their:
1. Step ID, title, description
2. Success criteria defining score levels (typically Score 10, Score 5, Score 0)
3. Execution output (what the evaluation agent found)

You must analyze ALL steps and submit a score for EACH one.

## Scoring Guidelines
- Read each step's success criteria carefully to understand what qualifies for each score level
- Analyze the execution output to determine which criteria level is met
- Be objective and evidence-based in your scoring
- If partial success is achieved, choose the score level that best matches the outcomes
- Consider cross-step patterns: if multiple steps fail for similar reasons, note this in your reasoning
- Look at the overall picture: do the combined results tell a coherent story?

## Available Tools
- **submit_score**: Submit score for one eval step. Call ONCE per step.
  - step_id (string): The ID of the evaluation step
  - score (integer): Score 0-10
  - reasoning (string): Why this score was assigned
  - evidence (string): Key evidence from execution output

## Instructions
1. Call submit_score for EACH evaluation step
2. After all scores submitted, write your overall evaluation summary as your final response
`
}

// EvaluationStepInput represents a single step's data for the scoring prompt
type EvaluationStepInput struct {
	ID              string
	Title           string
	Description     string
	SuccessCriteria string
	ExecutionOutput string
}

// GetUserPromptForAllSteps returns the user prompt for scoring all steps at once
func (a *WorkflowEvaluationScoringAgent) GetUserPromptForAllSteps(steps []EvaluationStepInput) string {
	now := time.Now()
	currentDate := now.Format("2006-01-02")
	currentTime := now.Format("15:04:05")

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("## 📅 Scoring Session: %s | %s\n\n", currentDate, currentTime))
	sb.WriteString(fmt.Sprintf("## Evaluation Steps (%d total)\n\n", len(steps)))

	for i, step := range steps {
		sb.WriteString(fmt.Sprintf("---\n### Step %d: %s\n", i+1, step.Title))
		sb.WriteString(fmt.Sprintf("**ID**: %s\n", step.ID))
		sb.WriteString(fmt.Sprintf("**Description**: %s\n\n", step.Description))
		sb.WriteString(fmt.Sprintf("**Success Criteria**:\n%s\n\n", step.SuccessCriteria))
		sb.WriteString(fmt.Sprintf("**Execution Output**:\n%s\n\n", step.ExecutionOutput))
	}

	sb.WriteString("---\n## Instructions\n")
	sb.WriteString("Analyze ALL steps above and call submit_score for EACH step.\n")
	sb.WriteString("After scoring all steps, write your overall evaluation summary as your final response.\n")

	return sb.String()
}
