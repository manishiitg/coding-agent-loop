package step_based_workflow

import (
	"context"
	"fmt"
	"strings"

	mcpagent "github.com/manishiitg/mcpagent/agent"
	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"
	"github.com/manishiitg/mcpagent/observability"
	"mcp-agent-builder-go/agent_go/pkg/orchestrator/agents"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

// Pre-parsed templates - panics at startup if invalid
var learningSystemPromptTemplate = MustRegisterTemplate("learningSystemPrompt", `# Skill Generation Agent

## Role & Identity
- **Role**: Skill Generation Agent
- **Trigger**: {{.LearningTrigger}}
- **Focus**: Extract WORKFLOW-CENTRIC execution sequence with dependencies and data flow for step "{{.StepTitle}}".

## CRITICAL PRINCIPLES
1. **Task-Specific ONLY**: Only save patterns that help a future agent perform *this specific task* better. Exclude general knowledge (syntax rules, generic tool mechanics).
2. **Keep it lean**: Remove patterns that aren't pulling their weight. A shorter, high-signal skill is better than a long one with noise.
3. **Scripts**: Save successful scripts (Python, bash) to '{{.ScriptsPath}}' and reference them from SKILL.md.
4. **For scripted code steps**: ` + "`main.py`" + ` and helper scripts are the executable source of truth. Use SKILL.md only for secondary notes: edge cases, selector drift, recovery strategies, and debugging guidance for future repair runs.
{{if .SkillCreatorPath}}
## SKILL WRITING GUIDE (CRITICAL — READ FIRST)
Before writing or updating anything, read the skill creator guide at '{{.SkillCreatorPath}}'.
It defines the correct anatomy of a skill (SKILL.md + references/ + scripts/ + assets/), progressive disclosure, file organization, and writing patterns. **Follow that guide** for how to structure the skill folder.
{{end}}

## FILE MANAGEMENT ALGORITHM (MANDATORY)
**Available tools**: execute_shell_command (for listing, reading, deleting, and **creating new files** via shell — e.g. using cat heredoc or tee) and diff_patch_workspace_file (for patching/updating **existing** files only — will fail if the file does not exist yet).
{{if .ExistingLearningsContent}}
**Existing skill pre-loaded (skip discovery/retrieval):**
{{.ExistingLearningsContent}}
{{else}}
1. **Discover**: Use execute_shell_command with 'ls -R' on '{{.WritePath}}'. Identify existing skill files.
2. **Retrieve**: Use execute_shell_command with 'cat' to read existing skill files.
{{end}}
3. **Read Execution Logs**: The execution logs at '{{.ExecutionLogsPath}}' are your primary source for extracting patterns. Read them efficiently:
   - First, list files: ` + "`" + `ls '{{.ExecutionLogsPath}}'` + "`" + `
   - **File naming**: ` + "`" + `execution-attempt-{N}-iteration-{M}-conversation.json` + "`" + ` (full conversation with tool calls), ` + "`" + `execution-attempt-{N}-iteration-{M}.json` + "`" + ` (result summary)
   - **Start with the result summary** (small file) to understand what happened — it has execution_result, retry_attempt, and status.
   - **Read conversation JSON only if needed** for detailed tool call sequences. These can be large (50K+). Use ` + "`" + `tail -c 30000` + "`" + ` to read from the bottom first — the most important patterns (final tool calls, success/failure outcome) are at the end.
   - **Multiple attempts**: Higher attempt numbers are retries. Focus on the latest successful attempt, or the latest failed attempt for failure analysis.
   - The conversation JSON has ` + "`" + `{"conversation_history": [{"Role": "system/human/ai", "Parts": [...]}]}` + "`" + ` — look for tool calls in ai messages (FunctionCall entries) and their results in subsequent human messages.
4. **Legacy Migration**: If you find '*_learning.md' files (legacy format) but no 'SKILL.md':
   - Read the legacy content and incorporate it into the new skill format with proper YAML frontmatter.
   - Derive the 'description' field from the legacy content (summarize the key patterns/approaches).
   - Delete the legacy files after writing the new skill.
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
6. **Persist**: Write the skill to '{{.WritePath}}/'.
   - SKILL.md MUST exist with YAML frontmatter:
   `+"`"+``+"`"+``+"`"+`
   ---
   name: {{.StepTitle}}
   description: "<1-2 sentence summary of what this skill teaches — optimal approach and key pitfalls>"
   disable-model-invocation: true
   user-invocable: false{{if .AllowedTools}}
   allowed-tools:{{range .AllowedToolsList}}
     - {{.}}{{end}}{{end}}
   ---
   `+"`"+``+"`"+``+"`"+`
   **IMPORTANT**: The 'description' field is critical — write a specific, actionable summary. Be concrete with tool names and parameters.
   - Organize supporting files following the skill creator guide (references/, scripts/, etc.).
   - Use diff_patch_workspace_file for existing files, execute_shell_command with cat heredoc for new files.
7. **Clean Up**:
{{if .IsScriptedCodeMode}}
   - Remove only legacy '*_learning.md' files and obsolete temporary markdown files.
   - **Do NOT delete** ` + "`main.py`" + `, helper ` + "`*.py`" + ` / ` + "`*.sh`" + ` files, or ` + "`script_metadata.json`" + `.
{{else}}
   - Remove legacy '*_learning.md' files. Do NOT remove reference files, scripts, or supporting skill files.
{{end}}

**Note**: Always quote paths with single quotes in shell commands, as folder names may contain spaces.

## FINAL ACTION
After writing, list what was updated:
'Updated: {{.WritePath}}/ (files: <list of files changed>)'`)

var learningUserMessageTemplate = MustRegisterTemplate("learningUserMessage", `# Skill Generation Task

## Context
- **Step**: {{.StepTitle}}
- **Goal**: {{.StepDescription}}

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

// Global learning system prompt template — focuses on domain-level accumulated knowledge
var globalLearningSystemPromptTemplate = MustRegisterTemplate("globalLearningSystemPrompt", `# Global Workflow Skill Generation Agent

## Role & Identity
- **Role**: Global Workflow Skill Generation Agent
- **Trigger**: {{.LearningTrigger}} (from step: {{.ContributingStepTitle}})
- **Focus**: Accumulate domain-level knowledge across ALL workflow steps into a shared skill at '{{.WritePath}}/'.
{{if .GlobalSkillObjective}}
## SKILL OBJECTIVE (from user)
{{.GlobalSkillObjective}}

Every piece of knowledge you capture should contribute toward this objective. Ask yourself: "Does this help achieve the skill objective?" If not, skip it.
{{end}}
## CRITICAL PRINCIPLES
1. **Domain Knowledge Only**: Capture knowledge about the TARGET SYSTEM (website structure, API patterns, auth flows, data schemas, selectors, common patterns) — NOT step-specific tool sequences.
2. **Accumulate & Merge**: Each step contributes new knowledge. Merge with existing content. Never discard previous knowledge unless proven wrong.
3. **Cross-Step Patterns**: Document patterns that help ANY step in the workflow, not just the one that discovered them.
{{if .SkillCreatorPath}}
## SKILL WRITING GUIDE (CRITICAL — READ FIRST)
Before writing or updating anything, read the skill creator guide at '{{.SkillCreatorPath}}'.
It defines the correct anatomy of a skill (SKILL.md + references/ + scripts/ + assets/), progressive disclosure, file organization, and writing patterns. **Follow that guide** for how to structure the skill folder — do not invent your own format.
{{end}}

## FILE MANAGEMENT ALGORITHM (MANDATORY)
**Available tools**: execute_shell_command (for listing, reading, deleting, and **creating new files** via shell — e.g. using cat heredoc or tee) and diff_patch_workspace_file (for patching/updating **existing** files only).
{{if .ExistingLearningsContent}}
**Existing global skill pre-loaded (skip discovery/retrieval):**
{{.ExistingLearningsContent}}
{{else}}
1. **Discover**: Use execute_shell_command with 'ls -R' on '{{.WritePath}}' to see the full folder structure.
2. **Retrieve**: Read existing files to understand current knowledge state.
{{end}}
3. **Read Execution Logs**: The execution logs at '{{.ExecutionLogsPath}}' show what step "{{.ContributingStepTitle}}" just did.
   - List files: ` + "`" + `ls '{{.ExecutionLogsPath}}'` + "`" + `
   - Start with result summary (small file) to understand what happened.
   - Read conversation JSON if needed for domain details (use ` + "`" + `tail -c 30000` + "`" + ` for large files).
4. **Merge & Consolidate**:
{{if .IsSuccess}}
   - Extract domain knowledge discovered by step "{{.ContributingStepTitle}}".
   - Decide which file(s) to update or create based on the skill structure.
   - Update SKILL.md if you added new reference files.
   - Update any patterns that were refined or corrected by this execution.
{{else}}
   - Document what went wrong and any domain knowledge revealed by the failure.
   - Preserve all existing successful patterns.
   - Add failure avoidance guidance to the relevant file.
{{end}}
5. **Persist**:
   - SKILL.md MUST exist with YAML frontmatter:
   ` + "```" + `
   ---
   name: {{.WorkflowName}}
   description: "<Summary of accumulated domain knowledge for this workflow>"
   disable-model-invocation: true
   user-invocable: false
   ---
   ` + "```" + `
   - Organize supporting files following the skill creator guide (references/, scripts/, etc.).
   - Use diff_patch_workspace_file for existing files, execute_shell_command with cat heredoc for new files.
6. **Clean Up**: Remove only legacy '*_learning.md' files. Do NOT remove reference files, scripts, or topic files.

## FINAL ACTION
After writing, list what was updated:
'Updated: {{.WritePath}}/ (files: <list of files changed>)'`)

// Global learning user message template
var globalLearningUserMessageTemplate = MustRegisterTemplate("globalLearningUserMessage", `# Global Workflow Skill Update

## Contributing Step
- **Step**: {{.ContributingStepTitle}} (ID: {{.ContributingStepID}})
- **Goal**: {{.StepDescription}}

## What Happened
{{if .IsSuccess}}- Step executed successfully. Extract domain knowledge discovered during execution.
{{else}}- Step failed. Document domain knowledge revealed by the failure and how to prevent it.
{{end}}

## Instructions
- Merge findings into the global skill, organized by category
- Do NOT include step-specific tool sequences — only domain knowledge
- Keep the skill focused on the TARGET SYSTEM, not on workflow mechanics
- Replace hardcoded values with {{"{{"}}VARIABLE_NAME{{"}}"}} placeholders: {{.Variables}}

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
	for _, key := range []string{"StepExecutionPath", "StepNumber", "SkillCreatorPath", "AllowedTools", "IsScriptedCodeMode", "UseGlobalLearning", "ContributingStepID", "ContributingStepTitle", "GlobalSkillObjective"} {
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
	workspacePath := templateVars["WorkspacePath"]
	stepNumber := templateVars["StepNumber"]
	docsRoot := GetPromptDocsRoot()

	// Determine learning trigger (success or failure)
	learningTrigger := templateVars["LearningTrigger"]
	if learningTrigger == "" {
		learningTrigger = "success"
	}
	isSuccess := learningTrigger == "success"

	executionLogsPath := templateVars["ExecutionLogsPath"]
	existingLearningsContent := templateVars["ExistingLearningsContent"]

	// Global learning mode: use the global template
	if templateVars["UseGlobalLearning"] == "true" {
		writePath := docsRoot + "/" + workspacePath + "/learnings/" + GlobalLearningID
		var result strings.Builder
		if err := globalLearningSystemPromptTemplate.Execute(&result, map[string]interface{}{
			"IsSuccess":                isSuccess,
			"LearningTrigger":          strings.ToUpper(learningTrigger),
			"WritePath":                writePath,
			"ContributingStepTitle":    templateVars["ContributingStepTitle"],
			"WorkflowName":             "Workflow Knowledge",
			"ExecutionLogsPath":        executionLogsPath,
			"ExistingLearningsContent": existingLearningsContent,
			"SkillCreatorPath":         templateVars["SkillCreatorPath"],
			"GlobalSkillObjective":     templateVars["GlobalSkillObjective"],
		}); err != nil {
			panic(fmt.Sprintf("global learning system prompt template execution failed: %v", err))
		}
		return result.String()
	}

	// Per-step learning mode: use the existing template
	stepTitle := templateVars["StepTitle"]
	writePath := docsRoot + "/" + workspacePath + "/learnings/" + stepNumber
	scriptsPath := docsRoot + "/" + workspacePath + "/learnings/" + stepNumber + "/scripts"

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
		"IsScriptedCodeMode":       templateVars["IsScriptedCodeMode"] == "true",
		"LearningTrigger":          strings.ToUpper(learningTrigger),
		"WritePath":                writePath,
		"ScriptsPath":              scriptsPath,
		"StepTitle":                stepTitle,
		"ExecutionLogsPath":        executionLogsPath,
		"ExistingLearningsContent": existingLearningsContent,
		"AllowedTools":             len(allowedToolsList) > 0,
		"AllowedToolsList":         allowedToolsList,
		"SkillCreatorPath":         templateVars["SkillCreatorPath"],
	}); err != nil {
		panic(fmt.Sprintf("learning system prompt template execution failed (missing variable?): %v", err))
	}

	return result.String()
}

// learningUserMessageProcessor creates the user message for skill generation
func (agent *WorkflowLearningAgent) learningUserMessageProcessor(templateVars map[string]string) string {
	workspacePath := templateVars["WorkspacePath"]
	docsRoot := GetPromptDocsRoot()

	learningTrigger := templateVars["LearningTrigger"]
	if learningTrigger == "" {
		learningTrigger = "success"
	}
	isSuccess := learningTrigger == "success"

	// Global learning mode: use the global user message template
	if templateVars["UseGlobalLearning"] == "true" {
		var result strings.Builder
		if err := globalLearningUserMessageTemplate.Execute(&result, map[string]interface{}{
			"IsSuccess":              isSuccess,
			"ContributingStepTitle":  templateVars["ContributingStepTitle"],
			"ContributingStepID":     templateVars["ContributingStepID"],
			"StepDescription":        templateVars["StepDescription"],
			"Variables":              templateVars["VariableNames"],
			"ValidationResult":       templateVars["ValidationResult"],
		}); err != nil {
			panic(fmt.Sprintf("global learning user message template execution failed: %v", err))
		}
		return result.String()
	}

	// Per-step learning mode
	stepNumber := templateVars["StepNumber"]
	stepTitle := templateVars["StepTitle"]
	writePath := docsRoot + "/" + workspacePath + "/learnings/" + stepNumber

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
