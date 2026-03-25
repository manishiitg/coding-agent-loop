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

// Pre-parsed templates for code execution learning - panics at startup if invalid
var learningCodeSystemTemplate = MustRegisterTemplate("learningCodeSystem", `# Skill Creation Agent

## Role
You are a Skill Creation Agent. You extract the best code patterns from a completed step execution and persist them as a reusable skill file (SKILL.md + code files).

## Step Context
- **Step**: {{.StepTitle}}
- **Goal**: {{.StepDescription}}
- **Success Criteria**: {{.SuccessCriteria}}
{{if .Variables}}
## Variables
Replace these hardcoded values with {{ "{{" }}VARIABLE_NAME{{ "}}" }} placeholders in the skill file:
{{.Variables}}
{{end}}
## Validation Result
{{.ValidationResult}}

## Understanding the execution logs
The execution logs contain the full agent conversation for this step. Key elements you'll find:
- **DESCRIPTION**: The step's task description — what the agent was asked to do.
- **Orchestrator Instructions**: Additional context/instructions passed by the parent orchestrator (paths, specific requirements). These are run-specific and should NOT be hardcoded into the skill.
- **Tool calls**: The actual code executed via execute_shell_command, API calls, and their results.
- **Variables**: Values like ` + "`" + `{{.StepTitle}}` + "`" + ` that were resolved at runtime. In the skill file, replace these with {{ "{{" }}VARIABLE_NAME{{ "}}" }} placeholders so the skill is reusable across runs.

## Paths
| Path | Location |
|------|----------|
| Skill folder | ` + "`" + `{{.WritePath}}/` + "`" + ` |
| Code folder | ` + "`" + `{{.CodePath}}/` + "`" + ` |
| Execution logs | ` + "`" + `{{.ExecutionLogsPath}}/` + "`" + ` |
{{if .SkillCreatorPath}}| Skill creator guide | ` + "`" + `{{.SkillCreatorPath}}` + "`" + ` |
{{end}}
## Tools
- **execute_shell_command**: List, read, and delete files
- **diff_patch_workspace_file**: Write/update files

**Note**: Always quote paths with single quotes in shell commands (folder names may contain spaces).`)

var learningCodeUserTemplate = MustRegisterTemplate("learningCodeUser", `Create/update the skill file for step **{{.StepTitle}}**.

## Steps

### 1. Read existing skill files
List and read files from the skill folder (` + "`" + `{{.WritePath}}/` + "`" + `) and code folder (` + "`" + `{{.CodePath}}/` + "`" + `) to see what already exists.

### 2. Read execution logs
List and read the execution log files from ` + "`" + `{{.ExecutionLogsPath}}/` + "`" + `. These contain the full agent conversation — tool calls, code executed via execute_shell_command, and results. Extract the best code patterns from these logs.

### 3. Consolidate
{{if .IsSuccess}}- Merge new code patterns with existing skill. Prune old/inefficient code — keep ONLY the best.
- Mark the optimal code pattern that led to validation passing.
{{else}}- Analyze why execution failed. Document the root cause.
- Preserve existing successful patterns — do NOT discard what worked.
- Add the failure pattern with details on what went wrong.
{{end}}
### 4. Write skill file
{{if .SkillCreatorPath}}Read the skill creator guide at ` + "`" + `{{.SkillCreatorPath}}` + "`" + ` for best practices on writing effective skills.
{{end}}Use diff_patch_workspace_file to write ` + "`" + `{{.WritePath}}/SKILL.md` + "`" + ` with this format:
` + "`" + `` + "`" + `` + "`" + `
---
name: {{.StepTitle}}
description: "<1-2 sentence summary of the optimal code approach and key pitfalls>"
disable-model-invocation: true
user-invocable: false{{if .AllowedTools}}
allowed-tools:{{range .AllowedToolsList}}
  - {{.}}{{end}}{{end}}
---

(skill content here)
` + "`" + `` + "`" + `` + "`" + `

**Skill content should include**:
- **Best code pattern**: Explain WHY it works. Save the complete runnable code to ` + "`" + `{{.CodePath}}/` + "`" + ` with appropriate extension (.py, .sh). Reference it from SKILL.md using relative paths (e.g., 'code/main.py').
- **API calls**: Use ` + "`" + `os.environ["MCP_API_URL"]` + "`" + ` and ` + "`" + `os.environ["MCP_API_TOKEN"]` + "`" + ` — never hardcode URLs or tokens.
- **Output schema**: Document the exact JSON structure of created files.
- **Failures to avoid**: Task-specific pitfalls with root causes.

**IMPORTANT**: The 'description' field determines when this skill gets loaded. Be specific about the approach AND pitfalls.

### 5. Clean up
Delete all '*_learning.md' files (legacy format). Only SKILL.md and code files should remain.

## Final output
After cleanup, output ONLY: ` + "`" + `Updated: {{.WritePath}}/SKILL.md` + "`" + ``)

// WorkflowCodeExecutionLearningAgent analyzes code execution mode executions
// to capture code patterns (Python, bash, curl, etc.) and improve future code generation
type WorkflowCodeExecutionLearningAgent struct {
	*agents.BaseOrchestratorAgent
}

// NewWorkflowCodeExecutionLearningAgent creates a new code execution learning agent
func NewWorkflowCodeExecutionLearningAgent(config *agents.OrchestratorAgentConfig, logger loggerv2.Logger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) *WorkflowCodeExecutionLearningAgent {
	baseAgent := agents.NewBaseOrchestratorAgentWithEventBridge(
		config,
		logger,
		tracer,
		agents.TodoPlannerSuccessLearningAgentType,
		eventBridge,
	)

	return &WorkflowCodeExecutionLearningAgent{
		BaseOrchestratorAgent: baseAgent,
	}
}

// Execute implements the OrchestratorAgent interface for code execution mode learning
func (agent *WorkflowCodeExecutionLearningAgent) Execute(ctx context.Context, templateVars map[string]string, conversationHistory []llmtypes.MessageContent) (string, []llmtypes.MessageContent, error) {
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
	// Prepare template variables
	learningTemplateVars := map[string]string{
		"StepTitle":               stepTitle,
		"StepDescription":         stepDescription,
		"StepSuccessCriteria":     stepSuccessCriteria,
		"StepContextDependencies": stepContextDependencies,
		"StepContextOutput":       stepContextOutput,
		"WorkspacePath":           workspacePath,
		"ExecutionHistory":        executionHistory,
		"ValidationResult":        validationResult,
		"VariableNames":           variableNames,
	}

	// Add step-specific paths if provided (when flag is enabled)
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

	// Generate system prompt and user message for code execution mode
	systemPrompt := agent.learningSystemPromptProcessorCodeExecution(learningTemplateVars)
	userMessage := agent.learningUserMessageProcessorCodeExecution(learningTemplateVars)

	// Create a simple input processor that returns the user message
	inputProcessor := func(map[string]string) string {
		return userMessage
	}

	// Execute with system prompt and user message (overwrite=true to replace default MCP prompt with agent-specific prompt)
	// Note: SetSystemPrompt now always overwrites. If code execution instructions are needed, use prompt.GetCodeExecutionInstructions()
	return agent.ExecuteWithTemplateValidation(ctx, learningTemplateVars, inputProcessor, conversationHistory, templateData, systemPrompt, true)
}

// learningSystemPromptProcessorCodeExecution creates the system prompt for code execution mode learning
func (agent *WorkflowCodeExecutionLearningAgent) learningSystemPromptProcessorCodeExecution(templateVars map[string]string) string {
	workspacePath := templateVars["WorkspacePath"]
	stepNumber := templateVars["StepNumber"]
	stepTitle := templateVars["StepTitle"]
	docsRoot := GetPromptDocsRoot()
	writePath := docsRoot + "/" + workspacePath + "/learnings/" + stepNumber
	codePath := docsRoot + "/" + workspacePath + "/learnings/" + stepNumber + "/code"
	executionLogsPath := templateVars["ExecutionLogsPath"]

	var result strings.Builder
	if err := learningCodeSystemTemplate.Execute(&result, map[string]interface{}{
		"WritePath":         writePath,
		"CodePath":          codePath,
		"StepTitle":         stepTitle,
		"StepDescription":   templateVars["StepDescription"],
		"SuccessCriteria":   templateVars["StepSuccessCriteria"],
		"Variables":         templateVars["VariableNames"],
		"ValidationResult":      templateVars["ValidationResult"],
		"ExecutionLogsPath":     executionLogsPath,
		"SkillCreatorPath": templateVars["SkillCreatorPath"],
	}); err != nil {
		return "Error executing learning code system prompt template: " + err.Error()
	}

	return result.String()
}

// learningUserMessageProcessorCodeExecution creates the user message for code execution mode learning
func (agent *WorkflowCodeExecutionLearningAgent) learningUserMessageProcessorCodeExecution(templateVars map[string]string) string {
	workspacePath := templateVars["WorkspacePath"]
	stepNumber := templateVars["StepNumber"]
	stepTitle := templateVars["StepTitle"]
	docsRoot := GetPromptDocsRoot()
	writePath := docsRoot + "/" + workspacePath + "/learnings/" + stepNumber
	codePath := docsRoot + "/" + workspacePath + "/learnings/" + stepNumber + "/code"

	executionLogsPath := templateVars["ExecutionLogsPath"]

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
	if err := learningCodeUserTemplate.Execute(&result, map[string]interface{}{
		"StepTitle":              stepTitle,
		"WritePath":              writePath,
		"CodePath":               codePath,
		"ExecutionLogsPath":      executionLogsPath,
		"IsSuccess":              isSuccess,
		"AllowedTools":           len(allowedToolsList) > 0,
		"AllowedToolsList":       allowedToolsList,
		"SkillCreatorPath":  templateVars["SkillCreatorPath"],
	}); err != nil {
		return "Error executing learning code user message template: " + err.Error()
	}

	return result.String()
}
