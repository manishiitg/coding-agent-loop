package step_based_workflow

import (
	"context"
	"strings"

	"mcp-agent-builder-go/agent_go/pkg/orchestrator/agents"
	mcpagent "github.com/manishiitg/mcpagent/agent"
	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"
	"github.com/manishiitg/mcpagent/observability"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

// Pre-parsed templates - panics at startup if invalid
var learningSystemPromptTemplate = MustRegisterTemplate("learningSystemPrompt", `# Learning Analysis Agent

## Role & Identity
- **Role**: Learning Agent (Efficiency Optimizer)
- **Mode**: {{.Mode}}
- **Trigger**: {{.LearningTrigger}}
- **Focus**: {{if .IsExact}}Extract WORKFLOW-CENTRIC execution sequence with dependencies and data flow.{{else}}Extract tool names and high-level patterns + Python scripts.{{end}}

## CRITICAL LEARNING PRINCIPLES
1. **Task-Specific ONLY**: Only save learnings that help a future agent perform *this specific task* better.
2. **Explain the WHY, not just the WHAT**: For every pattern you capture, explain *why* it works or *why* the alternative fails. Future agents are smart — they need reasoning, not rigid rules. Instead of "ALWAYS use batch mode", write "Use batch mode because single-record inserts timeout on files >1000 rows due to API rate limits."
3. **Keep it lean**: Remove patterns that aren't pulling their weight. If a previously documented pattern was not used or not helpful in this execution, prune it. A shorter, high-signal skill is better than a long one with noise.
4. **Exclude General Knowledge**:
   - Syntax/Compilation errors (LLMs already know language rules).
   - Internal workspace tool mechanics (execute_shell_command, read_workspace_file, diff_patch_workspace_file, etc.).
   - Generic naming or formatting feedback.
5. **Include**:
   - **Patterns**: MCP tool calling sequences ('server.tool') with arguments, and *why* this sequence works.
   - **Shell Commands**: Successful execute_shell_command patterns (full paths used), and data processing pipelines.
   - **Success Criteria**: Exact JSON structures, field names, and data types found in outputs.
   - **Failures to Avoid**: Task-specific dead-ends with root cause (e.g., "Tool X doesn't work for PDF extraction because it only supports text/plain content type").
   - **Scripts**: Full content of successful scripts — Python, bash, or other (save to '{{.ScriptsPath}}'). If the same script pattern appears across multiple runs, bundle it as a reusable file.

## FILE MANAGEMENT ALGORITHM (MANDATORY)
**Available tools**: execute_shell_command (for listing, reading, and deleting files) and diff_patch_workspace_file (for writing/updating files).
{{if .ExistingLearningsContent}}
**Existing learnings pre-loaded (skip discovery/retrieval):**
{{.ExistingLearningsContent}}
{{else}}
1. **Discover**: Use execute_shell_command with 'ls' on '{{.WritePath}}'. Identify existing 'SKILL.md' or any '*_learning.md' files (legacy format).
2. **Retrieve**: Use execute_shell_command with 'cat' to read ALL identified learning files.
{{end}}
3. **Optional - Check Execution Logs**: If you need more context about actual tool usage, read execution logs from '{{.ExecutionLogsPath}}' (if available).
4. **Legacy Migration**: If you find '*_learning.md' files (legacy format) but no 'SKILL.md':
   - Read the legacy content and incorporate it into the new SKILL.md format with proper YAML frontmatter.
   - Derive the 'description' field from the legacy content (summarize the key patterns/approaches).
   - Delete the legacy files after writing SKILL.md.
5. **Consolidate**:
{{if .IsSuccess}}
   - Merge current execution findings with all history. Prioritize latest successful patterns.
   - Prune patterns mismatched with the current step description.
   - Mark the optimal execution path that led to validation passing.
{{else}}
   - Analyze why the execution failed validation. Document the root cause clearly.
   - Preserve existing successful patterns from history — do NOT discard what worked before.
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

   (learning content here)
   ` + "`" + `` + "`" + `` + "`" + `
   **IMPORTANT**: The 'description' field is critical — it determines when this skill gets loaded. Write a specific, actionable summary that covers WHAT the optimal approach is AND common pitfalls. Be concrete with tool names and parameters. Example: "Use server.create_record with batch mode for CSV imports; avoid single-record inserts which timeout on files >1000 rows due to API rate limits."
6. **Clean Up**: Use execute_shell_command with 'rm' to remove all other '*_learning.md' files and any old learning files in that folder. Only 'SKILL.md' should remain.

**Note**: Always quote paths with single quotes in shell commands, as folder names may contain spaces.

## OUTPUT FORMAT
The learning content (after the YAML frontmatter) should follow this structure:

{{if .IsExact}}
### EXECUTION WORKFLOW (EXACT MODE)
**OPTIMAL PATH**
1. **server.tool**:
   - arguments: {COMPLETE JSON - replace hardcode paths with {{ "{{" }}WORKSPACE_PATH{{ "}}" }} }
   - prerequisites: [Condition]
   - outputs: [Description]
   - on_error: [Specific recovery]

### DATA FLOW
Step 1 Output -> Step 2 Input. Trace the flow accurately.
{{else}}
### SUCCESS PATTERN
- **Tools**: server.tool
- **Approach**: Brief description of the strategy.
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

var learningUserMessageTemplate = MustRegisterTemplate("learningUserMessage", `# Learning Task: {{if .IsExact}}Workflow Extraction{{else}}Tool Extraction{{end}}

## Context
- **Step**: {{.StepTitle}}
- **Goal**: {{.StepDescription}}
- **Success Criteria**: {{.SuccessCriteria}}

## Extraction Focus
- {{if .IsExact}}Extract the COMPLETE, REPLAYABLE sequence of MCP tool calls.{{else}}Extract successful tool names and Python recipes.{{end}}
- Document what failed for *this specific task* (ignore general Go/Python errors).

## Variable Handling
- Replace hardcoded IDs/paths with {{ "{{" }}VARIABLE_NAME{{ "}}" }} placeholders: {{.Variables}}
- **Workspace Paths**: Always replace with {{ "{{" }}WORKSPACE_PATH{{ "}}" }} or relative paths.

---
## EXECUTION HISTORY
{{.ExecutionHistory}}

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
	}

	// Add step-specific paths (always enabled)
	if stepExecutionPath, ok := templateVars["StepExecutionPath"]; ok {
		learningTemplateVars["StepExecutionPath"] = stepExecutionPath
	}
	if stepNumber, ok := templateVars["StepNumber"]; ok {
		learningTemplateVars["StepNumber"] = stepNumber
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
	writePath := workspacePath + "/learnings/" + stepNumber
	scriptsPath := workspacePath + "/learnings/" + stepNumber + "/scripts"

	isExact := learningDetailLevel == "exact"

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
		"Mode":                     strings.ToUpper(learningDetailLevel),
		"IsExact":                  isExact,
		"IsSuccess":                isSuccess,
		"LearningTrigger":          strings.ToUpper(learningTrigger),
		"WritePath":                writePath,
		"ScriptsPath":              scriptsPath,
		"StepTitle":                stepTitle,
		"ExecutionLogsPath":        executionLogsPath,
		"ExistingLearningsContent": existingLearningsContent,
		"AllowedTools":             len(allowedToolsList) > 0,
		"AllowedToolsList":         allowedToolsList,
	}); err != nil {
		return "Error executing learning system prompt template: " + err.Error()
	}

	return result.String()
}

// learningUserMessageProcessor creates the user message that always instructs to capture both success and failure patterns
func (agent *WorkflowLearningAgent) learningUserMessageProcessor(templateVars map[string]string) string {
	learningDetailLevel := templateVars["LearningDetailLevel"]
	if learningDetailLevel == "" {
		learningDetailLevel = "exact"
	}

	workspacePath := templateVars["WorkspacePath"]
	stepNumber := templateVars["StepNumber"]
	stepTitle := templateVars["StepTitle"]
	writePath := workspacePath + "/learnings/" + stepNumber

	isExact := learningDetailLevel == "exact"

	var result strings.Builder
	if err := learningUserMessageTemplate.Execute(&result, map[string]interface{}{
		"IsExact":          isExact,
		"StepTitle":        stepTitle,
		"StepDescription":  templateVars["StepDescription"],
		"SuccessCriteria":  templateVars["StepSuccessCriteria"],
		"WritePath":        writePath,
		"Variables":        templateVars["VariableNames"],
		"ExecutionHistory": templateVars["ExecutionHistory"],
		"ValidationResult": templateVars["ValidationResult"],
	}); err != nil {
		return "Error executing learning user message template: " + err.Error()
	}

	return result.String()
}
