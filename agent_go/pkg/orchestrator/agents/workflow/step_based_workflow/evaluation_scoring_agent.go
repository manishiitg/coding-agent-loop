package step_based_workflow

import (
	"context"
	"time"

	"mcp-agent-builder-go/agent_go/pkg/orchestrator/agents"
	mcpagent "github.com/manishiitg/mcpagent/agent"
	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"
	"github.com/manishiitg/mcpagent/observability"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

// WorkflowEvaluationScoringAgent is an agent that calculates scores for evaluation steps
// based on execution outputs and success criteria.
// This agent is used after evaluation execution completes to analyze outputs and generate scores.
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
	return a.BaseOrchestratorAgent.ExecuteWithTemplateValidation(ctx, templateVars, nil, conversationHistory, nil, systemPrompt, true)
}

// GetSystemPrompt returns the system prompt for the evaluation scoring agent
func (a *WorkflowEvaluationScoringAgent) GetSystemPrompt() string {
	return `You are an evaluation scoring agent. Your task is to analyze execution outputs from evaluation steps and determine scores based on success criteria.

## Your Task
You will receive:
1. The execution output from an evaluation step (what actually happened)
2. The success criteria defining score levels (typically Score 10, Score 5, Score 0)

You must analyze the output against the success criteria and determine the appropriate score.

## Scoring Guidelines
- Read the success criteria carefully to understand what qualifies for each score level
- Analyze the execution output to determine which criteria level is met
- Be objective and evidence-based in your scoring
- If partial success is achieved, choose the score level that best matches the outcomes

## Output Format
You MUST call the submit_score tool with your evaluation result. The tool requires:
- step_id: The ID of the evaluation step being scored
- score: Integer score (typically 0, 5, or 10)
- reasoning: Brief explanation of why this score was assigned
- evidence: Key evidence from the execution output supporting this score
`
}

// GetUserPrompt returns the user prompt for scoring a specific step
func (a *WorkflowEvaluationScoringAgent) GetUserPrompt(stepID, stepTitle, stepDescription, successCriteria, executionOutput string) string {
	now := time.Now()
	currentDate := now.Format("2006-01-02")
	currentTime := now.Format("15:04:05")

	return `## 📅 Scoring Session: ` + currentDate + ` | ` + currentTime + `

## Evaluation Step
ID: ` + stepID + `
Title: ` + stepTitle + `
Description: ` + stepDescription + `

## Success Criteria
` + successCriteria + `

## Execution Output
` + executionOutput + `

## Instructions
Analyze the execution output against the success criteria and determine the appropriate score.
Call the submit_score tool with:
- step_id: "` + stepID + `"
- score: The score value (0, 5, or 10 based on success criteria)
- reasoning: Why this score was assigned
- evidence: Key evidence from the output`
}
