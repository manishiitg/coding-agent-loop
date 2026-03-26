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

// Pre-parsed templates - panics at startup if invalid
var learningSystemPromptTemplate = MustRegisterTemplate("learningSystemPrompt", `# Skill Generation Agent

## Role & Identity
- **Role**: Skill Generation Agent
- **Trigger**: {{.LearningTrigger}}
- **Focus**: Extract WORKFLOW-CENTRIC execution sequence with dependencies and data flow.

## CRITICAL PRINCIPLES
1. **Task-Specific ONLY**: Only save patterns that help a future agent perform *this specific task* better. Exclude general knowledge (syntax rules, generic tool mechanics).
2. **Keep it lean**: Remove patterns that aren't pulling their weight. A shorter, high-signal skill is better than a long one with noise.
3. **Scripts**: Save successful scripts (Python, bash) to '{{.ScriptsPath}}' and reference them from SKILL.md.

## FILE MANAGEMENT ALGORITHM (MANDATORY)
**Available tools**: execute_shell_command (for listing, reading, and deleting files) and diff_patch_workspace_file (for writing/updating files).
{{if .ExistingLearningsContent}}
**Existing skill pre-loaded (skip discovery/retrieval):**
{{.ExistingLearningsContent}}
{{else}}
1. **Discover**: Use execute_shell_command with 'ls' on '{{.WritePath}}'. Identify existing 'SKILL.md' or any '*_learning.md' files (legacy format).
2. **Retrieve**: Use execute_shell_command with 'cat' to read ALL identified skill files.
{{end}}
3. **Read Execution Logs**: The execution logs at '{{.ExecutionLogsPath}}' are your primary source for extracting patterns. Read them efficiently:
   - First, list files: ` + "`" + `ls '{{.ExecutionLogsPath}}'` + "`" + `
   - **File naming**: ` + "`" + `execution-attempt-{N}-iteration-{M}-conversation.json` + "`" + ` (full conversation with tool calls), ` + "`" + `execution-attempt-{N}-iteration-{M}.json` + "`" + ` (result summary)
   - **Start with the result summary** (small file) to understand what happened — it has execution_result, retry_attempt, and status.
   - **Read conversation JSON only if needed** for detailed tool call sequences. These can be large (50K+). Use ` + "`" + `tail -c 30000` + "`" + ` to read from the bottom first — the most important patterns (final tool calls, success/failure outcome) are at the end.
   - **Multiple attempts**: Higher attempt numbers are retries. Focus on the latest successful attempt, or the latest failed attempt for failure analysis.
   - The conversation JSON has ` + "`" + `{"conversation_history": [{"Role": "system/human/ai", "Parts": [...]}]}` + "`" + ` — look for tool calls in ai messages (FunctionCall entries) and their results in subsequent human messages.
{{if .SkillCreatorPath}}4. **Skill Writing Guide**: Read the skill creator guide at '{{.SkillCreatorPath}}' for best practices on writing effective skills.
{{end}}5. **Legacy Migration**: If you find '*_learning.md' files (legacy format) but no 'SKILL.md':
   - Read the legacy content and incorporate it into the new SKILL.md format with proper YAML frontmatter.
   - Derive the 'description' field from the legacy content (summarize the key patterns/approaches).
   - Delete the legacy files after writing SKILL.md.
5. **Consolidate**:
{{if .IsSuccess}}
   - Merge current execution findings with existing skill. Prioritize latest successful patterns.
   - Prune patterns mismatched with the current step description.
   - Mark the optimal execution path that led to validation passing.
{{else}}
   - Analyze why the execution failed validation. Document the root cause clearly.
   - Preserve existing successful patterns — do NOT discard what worked before.
   - Add the failure pattern with specific details on what went wrong and how to avoid it.
   - If the failure reveals a better approach, document it as an alternative path.
{{end}}
5. **Persist**: Use diff_patch_workspace_file to write ONE final consolidated file to '{{.WritePath}}/SKILL.md'.
   The file MUST use YAML frontmatter in the following format:
   ` + "`" + `` + "`" + `` + "`" + `
   ---
   name: {{.StepTitle}}
   description: "<YOU MUST WRITE THIS: 1-2 sentence summary of what this skill teaches — what the optimal approach is and key pitfalls to avoid>"
   disable-model-invocation: true
   user-invocable: false{{if .AllowedTools}}
   allowed-tools:{{range .AllowedToolsList}}
     - {{.}}{{end}}{{end}}
   ---

   (skill content here)
   ` + "`" + `` + "`" + `` + "`" + `
   **IMPORTANT**: The 'description' field is critical — it determines when this skill gets loaded. Write a specific, actionable summary that covers WHAT the optimal approach is AND common pitfalls. Be concrete with tool names and parameters. Example: "Use server.create_record with batch mode for CSV imports; avoid single-record inserts which timeout on files >1000 rows due to API rate limits."
6. **Clean Up**: Use execute_shell_command with 'rm' to remove all other '*_learning.md' files and any old files in that folder. Only 'SKILL.md' should remain.

**Note**: Always quote paths with single quotes in shell commands, as folder names may contain spaces.

## OUTPUT FORMAT
The skill content (after the YAML frontmatter) should follow this structure:

{{if .IsSuccess}}
### EXECUTION WORKFLOW
**OPTIMAL PATH**
1. **server.tool**:
   - arguments: {COMPLETE JSON - replace hardcode paths with {{ "{{" }}WORKSPACE_PATH{{ "}}" }} }
   - prerequisites: [Condition]
   - outputs: [Description]
   - on_error: [Specific recovery]

### DATA FLOW
Step 1 Output -> Step 2 Input. Trace the flow accurately.
{{else}}
### FAILURE ANALYSIS
- **What failed**: Describe the exact point of failure — which tool call, which step, what error.
- **Root cause**: Why it failed (e.g., missing tool, wrong arguments, timeout, auth issue, missing dependency).
- **What worked before failure**: Document the steps that succeeded before the failure point — these are still valid patterns.
- **How to avoid**: Specific guidance for future agents to prevent this failure.

### EXECUTION WORKFLOW
Preserve any existing OPTIMAL PATH from previous successful runs. Add the failure pattern below it.
{{end}}

### OUTPUT FILE FORMATS
- **File**: filename.json
- **Structure**: { "field": "type" } - Provide exact structure for consistency.

### FAILURES TO AVOID
- server.tool - [Task-specific reason]. Use [Correct Approach] instead.

## FINAL ACTION
After cleanup, output ONLY the file path:
'Updated: {{.WritePath}}/SKILL.md'
Do not add summaries or talkative reports.`)

var learningUserMessageTemplate = MustRegisterTemplate("learningUserMessage", `# Skill Generation Task

## Context
- **Step**: {{.StepTitle}}
- **Goal**: {{.StepDescription}}
- **Success Criteria**: {{.SuccessCriteria}}

## Extraction Focus
{{if .IsSuccess}}- Extract the COMPLETE, REPLAYABLE sequence of MCP tool calls.
- Document what failed for *this specific task* (ignore general Go/Python errors).
{{else}}- Identify the FAILURE POINT — which tool call or step failed and why.
- Preserve any successful patterns that worked before the failure.
- Document the root cause so future agents can avoid this failure.
{{end}}

## Variable Handling
- Replace hardcoded IDs/paths with {{ "{{" }}VARIABLE_NAME{{ "}}" }} placeholders: {{.Variables}}
- **Workspace Paths**: Always replace with {{ "{{" }}WORKSPACE_PATH{{ "}}" }} or relative paths.

---
## VALIDATION RESULTS
{{.ValidationResult}}`)

// WorkflowLearningTemplate holds template variables for learning prompts
type WorkflowLearningTemplate struct {
	StepTitle               string
	StepDescription         string
	StepSuccessCriteria     string
	StepContextDependencies string
	StepContextOutput       string
	WorkspacePath           string
	ExecutionHistory        string
	ValidationResult        string
}

// WorkflowLearningAgent analyzes executions (both successful and failed) to capture learnings and improve future executions
type WorkflowLearningAgent struct {
	*agents.BaseOrchestratorAgent
}

// NewWorkflowLearningAgent creates a new learning agent that handles both success and failure cases
func NewWorkflowLearningAgent(config *agents.OrchestratorAgentConfig, logger loggerv2.Logger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) *WorkflowLearningAgent {
	baseAgent := agents.NewBaseOrchestratorAgentWithEventBridge(
		config,
		logger,
		tracer,
		agents.TodoPlannerSuccessLearningAgentType,
		eventBridge,
	)

	return &WorkflowLearningAgent{
		BaseOrchestratorAgent: baseAgent,
	}
}

// NewWorkflowSuccessLearningAgent is a compatibility alias for the unified learning agent
func NewWorkflowSuccessLearningAgent(config *agents.OrchestratorAgentConfig, logger loggerv2.Logger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) *WorkflowLearningAgent {
	return NewWorkflowLearningAgent(config, logger, tracer, eventBridge)
}

// Execute implements the OrchestratorAgent interface
func (agent *WorkflowLearningAgent) Execute(ctx context.Context, templateVars map[string]string, conversationHistory []llmtypes.MessageContent) (string, []llmtypes.MessageContent, error) {
	// Extract variables from template variables
	stepTitle := templateVars["StepTitle"]
	stepDescription := templateVars["StepDescription"]
	stepSuccessCriteria := templateVars["StepSuccessCriteria"]
	stepContextDependencies := templateVars["StepContextDependencies"]
	stepContextOutput := templateVars["StepContextOutput"]
	workspacePath := templateVars["WorkspacePath"]
	executionHistory := templateVars["ExecutionHistory"]
	validationResult := templateVars["ValidationResult"]
	variableNames := templateVars["VariableNames"]
	learningDetailLevel := templateVars["LearningDetailLevel"]
	existingLearningsContent := templateVars["ExistingLearningsContent"] // Existing learnings to build upon
	// Default to "exact" if not provided
	if learningDetailLevel == "" {
		learningDetailLevel = "exact"
	}

	// Prepare template variables
	learningTemplateVars := map[string]string{
		"StepTitle":                stepTitle,
		"StepDescription":          stepDescription,
		"StepSuccessCriteria":      stepSuccessCriteria,
		"StepContextDependencies":  stepContextDependencies,
		"StepContextOutput":        stepContextOutput,
		"WorkspacePath":            workspacePath,
		"ExecutionHistory":         executionHistory,
		"ValidationResult":         validationResult,
		"VariableNames":            variableNames,
		"LearningDetailLevel":      learningDetailLevel,
		"ExistingLearningsContent": existingLearningsContent, // Pass existing learnings to build upon
		"LearningTrigger":          templateVars["LearningTrigger"],
	}

	// Forward additional template vars from caller
	for _, key := range []string{"StepExecutionPath", "StepNumber", "SkillCreatorPath", "AllowedTools"} {
		if v, ok := templateVars[key]; ok {
			learningTemplateVars[key] = v
		}
	}

	// Create template data for learning
	templateData := WorkflowLearningTemplate{
		StepTitle:               stepTitle,
		StepDescription:         stepDescription,
		StepSuccessCriteria:     stepSuccessCriteria,
		StepContextDependencies: stepContextDependencies,
		StepContextOutput:       stepContextOutput,
		WorkspacePath:           workspacePath,
		ExecutionHistory:        executionHistory,
		ValidationResult:        validationResult,
	}

	// Generate system prompt and user message separately
	// Always learn from both success and failure patterns, regardless of validation status
	systemPrompt := agent.learningSystemPromptProcessor(learningTemplateVars)
	userMessage := agent.learningUserMessageProcessor(learningTemplateVars)

	// Create a simple input processor that returns the user message
	inputProcessor := func(map[string]string) string {
		return userMessage
	}

	// Execute with system prompt and user message (overwrite=true to replace default MCP prompt with agent-specific prompt)
	return agent.ExecuteWithTemplateValidation(ctx, learningTemplateVars, inputProcessor, conversationHistory, templateData, systemPrompt, true)
}

// learningSystemPromptProcessor creates the system prompt that always captures both success and failure patterns
func (agent *WorkflowLearningAgent) learningSystemPromptProcessor(templateVars map[string]string) string {
	learningDetailLevel := templateVars["LearningDetailLevel"]
	if learningDetailLevel == "" {
		learningDetailLevel = "exact"
	}

	workspacePath := templateVars["WorkspacePath"]
	stepNumber := templateVars["StepNumber"]
	stepTitle := templateVars["StepTitle"]
	docsRoot := GetPromptDocsRoot()
	writePath := docsRoot + "/" + workspacePath + "/learnings/" + stepNumber
	scriptsPath := docsRoot + "/" + workspacePath + "/learnings/" + stepNumber + "/scripts"

	executionLogsPath := templateVars["ExecutionLogsPath"]
	existingLearningsContent := templateVars["ExistingLearningsContent"]

	// Determine learning trigger (success or failure)
	learningTrigger := templateVars["LearningTrigger"]
	if learningTrigger == "" {
		learningTrigger = "success"
	}
	isSuccess := learningTrigger == "success"

	// Build allowed tools list from template vars
	allowedToolsStr := templateVars["AllowedTools"]
	var allowedToolsList []string
	if allowedToolsStr != "" {
		for _, tool := range strings.Split(allowedToolsStr, ",") {
			tool = strings.TrimSpace(tool)
			if tool != "" {
				allowedToolsList = append(allowedToolsList, tool)
			}
		}
	}

	var result strings.Builder
	if err := learningSystemPromptTemplate.Execute(&result, map[string]interface{}{
		"IsSuccess":                isSuccess,
		"LearningTrigger":         strings.ToUpper(learningTrigger),
		"WritePath":               writePath,
		"ScriptsPath":             scriptsPath,
		"StepTitle":               stepTitle,
		"ExecutionLogsPath":       executionLogsPath,
		"ExistingLearningsContent": existingLearningsContent,
		"AllowedTools":            len(allowedToolsList) > 0,
		"AllowedToolsList":        allowedToolsList,
		"SkillCreatorPath":        templateVars["SkillCreatorPath"],
	}); err != nil {
		panic(fmt.Sprintf("learning system prompt template execution failed (missing variable?): %v", err))
	}

	return result.String()
}

// learningUserMessageProcessor creates the user message for skill generation
func (agent *WorkflowLearningAgent) learningUserMessageProcessor(templateVars map[string]string) string {
	workspacePath := templateVars["WorkspacePath"]
	stepNumber := templateVars["StepNumber"]
	stepTitle := templateVars["StepTitle"]
	docsRoot := GetPromptDocsRoot()
	writePath := docsRoot + "/" + workspacePath + "/learnings/" + stepNumber

	learningTrigger := templateVars["LearningTrigger"]
	if learningTrigger == "" {
		learningTrigger = "success"
	}
	isSuccess := learningTrigger == "success"

	var result strings.Builder
	if err := learningUserMessageTemplate.Execute(&result, map[string]interface{}{
		"IsSuccess":        isSuccess,
		"StepTitle":        stepTitle,
		"StepDescription":  templateVars["StepDescription"],
		"SuccessCriteria":  templateVars["StepSuccessCriteria"],
		"WritePath":        writePath,
		"Variables":        templateVars["VariableNames"],
		"ExecutionHistory": templateVars["ExecutionHistory"],
		"ValidationResult": templateVars["ValidationResult"],
	}); err != nil {
		panic(fmt.Sprintf("learning user message template execution failed (missing variable?): %v", err))
	}

	return result.String()
}
