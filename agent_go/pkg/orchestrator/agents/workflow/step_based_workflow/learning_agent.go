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

// Learning system prompt template — focuses on domain-level accumulated knowledge
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
{{if .IsScriptedCodeMode}}6. **Scripts (Code Execution Mode)**:
   - ` + "`main.py`" + ` and helper scripts are STEP-SPECIFIC — save them to '{{.StepScriptsPath}}/' (NOT to the global skill folder).
   - Domain knowledge (selectors, API patterns, common issues) goes to the global skill at '{{.WritePath}}/'.
   - Reference the step scripts path from SKILL.md if relevant patterns were discovered.
{{end}}7. **Clean Up**: Remove only legacy '*_learning.md' files. Do NOT remove reference files, scripts, or topic files.

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
	for _, key := range []string{"StepExecutionPath", "StepNumber", "SkillCreatorPath", "AllowedTools", "IsScriptedCodeMode", "UseGlobalLearning", "ContributingStepID", "ContributingStepTitle", "GlobalSkillObjective", "StepScriptsPath"} {
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
	docsRoot := GetPromptDocsRoot()

	// Determine learning trigger (success or failure)
	learningTrigger := templateVars["LearningTrigger"]
	if learningTrigger == "" {
		learningTrigger = "success"
	}
	isSuccess := learningTrigger == "success"

	executionLogsPath := templateVars["ExecutionLogsPath"]
	existingLearningsContent := templateVars["ExistingLearningsContent"]

	// Always use global learning template
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
		"IsScriptedCodeMode":       templateVars["IsScriptedCodeMode"] == "true",
		"StepScriptsPath":          templateVars["StepScriptsPath"],
	}); err != nil {
		panic(fmt.Sprintf("learning system prompt template execution failed: %v", err))
	}
	return result.String()
}

// learningUserMessageProcessor creates the user message for skill generation
func (agent *WorkflowLearningAgent) learningUserMessageProcessor(templateVars map[string]string) string {
	learningTrigger := templateVars["LearningTrigger"]
	if learningTrigger == "" {
		learningTrigger = "success"
	}
	isSuccess := learningTrigger == "success"

	// Always use global learning user message template
	var result strings.Builder
	if err := globalLearningUserMessageTemplate.Execute(&result, map[string]interface{}{
		"IsSuccess":              isSuccess,
		"ContributingStepTitle":  templateVars["ContributingStepTitle"],
		"ContributingStepID":     templateVars["ContributingStepID"],
		"StepDescription":        templateVars["StepDescription"],
		"Variables":              templateVars["VariableNames"],
		"ValidationResult":       templateVars["ValidationResult"],
	}); err != nil {
		panic(fmt.Sprintf("learning user message template execution failed: %v", err))
	}
	return result.String()
}
