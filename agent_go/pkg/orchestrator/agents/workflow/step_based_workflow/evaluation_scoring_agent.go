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

	// extraSystemPromptSection is appended to the system prompt after the base
	// prompt and the optional code-exec section. Used to inject learn_code
	// instructions when declared_execution_mode is learn_code.
	extraSystemPromptSection string

	// learnCodeMode, when true, sets the IsLearnCodeMode template var so the
	// AgentStarted event carries UseLearnCodeMode=true and the UI renders
	// "Learn Code" instead of "Code Exec".
	learnCodeMode bool
}

// SetExtraSystemPromptSection lets the factory attach mode-specific instructions
// (currently the learn_code section) without subclassing the agent.
func (a *WorkflowEvaluationScoringAgent) SetExtraSystemPromptSection(section string) {
	a.extraSystemPromptSection = section
}

// SetLearnCodeMode flips the learn_code marker for event labeling. The factory
// calls this when declared_execution_mode == learn_code so the UI and event log
// attribute the run to the correct mode.
func (a *WorkflowEvaluationScoringAgent) SetLearnCodeMode(on bool) {
	a.learnCodeMode = on
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
	// to call submit_report via HTTP API (same pattern as other agents via {{.CodeExecutionSection}})
	if a.GetConfig() != nil && a.GetConfig().UseCodeExecutionMode {
		codeExecSection := BuildCodeExecutionSection(true, "")
		if codeExecSection != "" {
			systemPrompt += "\n\n" + codeExecSection
		}
	}

	// Append mode-specific extras (e.g. learn_code main.py authoring instructions)
	if a.extraSystemPromptSection != "" {
		systemPrompt += "\n\n" + a.extraSystemPromptSection
	}

	// Ensure the AgentStarted event carries the right mode marker. base_orchestrator_agent.go
	// reads templateVars["IsLearnCodeMode"] to set UseLearnCodeMode on the event, which is
	// what makes the UI render "Learn Code" instead of "Code Exec".
	if templateVars == nil {
		templateVars = map[string]string{}
	}
	templateVars["IsEvaluationMode"] = "true"
	if a.learnCodeMode {
		if _, set := templateVars["IsLearnCodeMode"]; !set {
			templateVars["IsLearnCodeMode"] = "true"
		}
	}

	return a.BaseOrchestratorAgent.ExecuteWithTemplateValidation(ctx, templateVars, nil, conversationHistory, nil, systemPrompt, true)
}

// GetSystemPrompt returns the system prompt for the evaluation scoring agent
func (a *WorkflowEvaluationScoringAgent) GetSystemPrompt() string {
	return `You are an evaluation scoring agent. Your task is to analyze execution outputs from ALL evaluation steps and write a single JSON report scoring every step.

## Your Task
You will receive all evaluation steps with their:
1. Step ID, title, description (the description tells you what the eval step was checking and how scores should be assigned)
2. Execution output (what the evaluation agent found)

You must analyze ALL steps and write one ` + "`evaluation_report.json`" + ` file covering every step.

## Scoring Guidelines
- Read each step's description carefully — it encodes both what the eval did and what passing/failing looks like (often as embedded "score N pts: ..." lines)
- Many eval steps emit a structured ` + "`context_output.json`" + ` containing fields like ` + "`score`, `max_score`, `checks[]`" + `. When present, use those numeric fields directly — that's the eval step's own deterministic verdict
- Be objective and evidence-based in your scoring
- If partial success is achieved, choose the score level that best matches the outcomes
- Consider cross-step patterns: if multiple steps fail for similar reasons, surface that in reasoning

## Output Contract
Write the report file using one of your available tools:
- ` + "`execute_shell_command`" + ` — e.g. ` + "`python3 -c 'import json,sys; open(sys.argv[1],\"w\").write(json.dumps({...}, indent=2))' /abs/path`" + ` or a heredoc ` + "`cat > /abs/path << 'EOF' ... EOF`" + `
- ` + "`diff_patch_workspace_file`" + ` — for incremental edits if a draft already exists

The user prompt below gives you the exact ABSOLUTE path. Always use absolute paths in shell commands — relative paths are unreliable here because the shell session's cwd doesn't necessarily match the workspace root.

The JSON must match this schema:

` + "```json" + `
{
  "step_scores": [
    {
      "step_id":   "string — must match a step_id from the input",
      "score":     0-10,
      "reasoning": "string >= 20 chars — why this score",
      "evidence":  "string >= 10 chars — key excerpts from execution output"
    }
  ]
}
` + "```" + `

Constraints:
- Every step_id from the input MUST appear exactly once in step_scores (no duplicates, no missing)
- Field types and minimum lengths are enforced after you finish — produce a clean report on the first try
- Do NOT include extra fields the runtime owns (max_score, step_title, totals, percentages — those are filled in afterwards). There is intentionally NO ` + "`summary`" + ` field — per-step reasoning + evidence is the entire output

## Instructions
1. Read each step's description (the rubric is in there) and execution output
2. Write the complete report file using a workspace write tool
3. End your turn — the runtime validates the file against the schema and surfaces any errors
`
}

// EvaluationStepInput represents a single step's data for the scoring prompt.
// success_criteria has been removed from EvaluationStep — the description encodes
// what passing/failing looks like.
//
// JSON tags are required: this struct is also marshaled into scoring_inputs.json
// for the learn_code main.py to read, and the agreed-on schema (documented in
// scoringLearnCodePromptSection) uses lowercase keys. Without the tags Go would
// emit `ID`/`Title`/etc., breaking any main.py that follows the documented schema.
type EvaluationStepInput struct {
	ID              string `json:"id"`
	Title           string `json:"title"`
	Description     string `json:"description"`
	ExecutionOutput string `json:"execution_output"`
}

// GetUserPromptForAllSteps returns the user prompt for scoring all steps at once.
// reportPath is the absolute path where the agent must write evaluation_report.json.
// targetRunPath is the absolute path to the original execution folder being evaluated
// (the value of {{TARGET_RUN_PATH}}); empty string skips the section.
func (a *WorkflowEvaluationScoringAgent) GetUserPromptForAllSteps(steps []EvaluationStepInput, reportPath string, targetRunPath string) string {
	now := time.Now()
	currentDate := now.Format("2006-01-02")
	currentTime := now.Format("15:04:05")

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("## 📅 Scoring Session: %s | %s\n\n", currentDate, currentTime))
	sb.WriteString(fmt.Sprintf("## Output File\nWrite the report to: `%s`\n\n", reportPath))
	if targetRunPath != "" {
		sb.WriteString(fmt.Sprintf("## Target Run Path\nThe workflow run you're scoring placed its execution artifacts at:\n`%s`\n\nThis is the resolved absolute value of `{{TARGET_RUN_PATH}}` — eval step descriptions and their outputs may reference it. Use this absolute path if you need to read the original artifacts directly.\n\n", targetRunPath))
	}
	sb.WriteString(fmt.Sprintf("## Evaluation Steps (%d total)\n\n", len(steps)))

	for i, step := range steps {
		sb.WriteString(fmt.Sprintf("---\n### Step %d: %s\n", i+1, step.Title))
		sb.WriteString(fmt.Sprintf("**ID**: %s\n", step.ID))
		sb.WriteString(fmt.Sprintf("**Description**: %s\n\n", step.Description))
		sb.WriteString(fmt.Sprintf("**Execution Output**:\n%s\n\n", step.ExecutionOutput))
	}

	sb.WriteString("---\n## Instructions\n")
	sb.WriteString(fmt.Sprintf("Analyze ALL steps above and write `%s` containing one step_scores entry per step plus an overall summary, matching the schema in your system prompt.\n", reportPath))

	return sb.String()
}
