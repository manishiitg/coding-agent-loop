package step_based_workflow

import (
	"context"
	"fmt"
	"strings"
	"time"

	"mcp-agent-builder-go/agent_go/pkg/orchestrator/agents"
	"mcp-agent-builder-go/agent_go/pkg/skills"
	mcpagent "github.com/manishiitg/mcpagent/agent"
	"github.com/manishiitg/mcpagent/agent/prompt"
	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"
	"github.com/manishiitg/mcpagent/observability"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

// Pre-parsed templates for execution-only agent - panics at startup if invalid
var executionOnlySystemTemplate = MustRegisterTemplate("executionOnlySystem", `# Execution-Only Agent

## Context: {{.CurrentDate}} | {{.CurrentTime}}

## Role & Responsibility
- **Identity**: Execution-Only Agent (Focused on completion, not discovery).
- **Goal**: Execute the current plan step using MCP tools or code execution.
{{if .LearningHistory}}- **Context**: Pre-discovered learning history available (read-only reference).{{end}}

{{if .CustomInstructions}}
## Custom Instructions (Saved by User)
{{.CustomInstructions}}
{{end}}

{{if .IsCodeExecutionMode}}
## Code Execution Mode
{{.CodeExecutionInstructions}}
{{end}}

{{if .UseToolSearchMode}}
## Tool Search Mode
{{.ToolSearchInstructions}}
{{end}}

{{if .VariableNames}}
## Variables
{{.VariableNames}}
{{if .VariableValues}}**Values**: {{.VariableValues}}{{end}}

**Handling**: Step descriptions are already resolved. For code and tool calls, use the resolved values directly.
{{end}}

## CRITICAL EXECUTION RULES
1. **Source of Truth**: The **Step Description** defines WHAT to do.{{if eq .HasLearnings "true"}} It ALWAYS overrides learnings.{{end}}
2. **Workspace Paths** (all paths below are absolute):
   - **Step folder**: `+"`"+`{{.StepExecutionPath}}/`+"`"+`
   - **Execution folder**: `+"`"+`{{.WorkspacePath}}/`+"`"+`
   - **IMPORTANT**: Always use `+"`"+`mkdir -p`+"`"+` before writing to a path if the directory may not exist yet.
{{if .IsCodeExecutionMode}}   - **execute_shell_command paths**: Use absolute paths. E.g., `+"`"+`cd '{{.StepExecutionPath}}' && python3 script.py`+"`"+`.
   - **Writing output files**: `+"`"+`open("{{.StepExecutionPath}}/{{.StepContextOutput}}", "w")`+"`"+`.
   - **Reading dependencies**: Use the absolute paths shown in **Inputs** below.
   - **MCP tool calls**: Use HTTP requests to per-tool endpoints via `+"`"+`os.environ["MCP_API_URL"]`+"`"+` and `+"`"+`os.environ["MCP_API_TOKEN"]`+"`"+`.
   - **Shell variable quoting**: In curl/bash, use DOUBLE quotes for headers containing env vars: `+"`"+`-H "Authorization: Bearer $MCP_API_TOKEN"`+"`"+`. Single quotes prevent variable expansion.
   - **Environment variables**: Workflow variables and secrets are available in shell commands via `+"`"+`os.environ`+"`"+` (Python) or `+"`"+`$VAR`+"`"+` (bash):
     - Workflow variables are prefixed with `+"`"+`VAR_`+"`"+` (e.g., variable `+"`"+`API_URL`+"`"+` → `+"`"+`os.environ["VAR_API_URL"]`+"`"+`)
     - Secrets are available directly by name (e.g., `+"`"+`os.environ["MY_SECRET"]`+"`"+`)
{{else}}   - **File Operations**: Prefer `+"`"+`execute_shell_command`+"`"+` for reading files (`+"`"+`cat`+"`"+`, `+"`"+`head`+"`"+`), writing files (shell redirects, `+"`"+`python3 -c`+"`"+`), and data processing. Use `+"`"+`diff_patch_workspace_file`+"`"+` for targeted edits to existing files.
   - **execute_shell_command paths**: Use absolute paths in commands (e.g., `+"`"+`echo '...' > '{{.StepExecutionPath}}/output.json'`+"`"+`). To run in a specific directory: `+"`"+`cd '{{.StepExecutionPath}}' && <command>`+"`"+`.
   - **MCP tools**: Use MCP tools directly for external service calls.
{{end}}3. **Pre-requisites**: Read all **Context Dependencies** before execution. They are inputs.{{if .IsCodeExecutionMode}} Read dependency files using the absolute paths shown in **Inputs**.{{else}} Use `+"`"+`execute_shell_command`+"`"+` (e.g., `+"`"+`cat '{{.StepExecutionPath}}/file'`+"`"+`) to read files. The **Inputs** field below shows exact absolute file paths.{{end}}
4. **Mandatory Output**: Create '{{.StepContextOutput}}' in the step folder '{{.StepExecutionPath}}/'.{{if .IsCodeExecutionMode}} Write to `+"`"+`{{.StepExecutionPath}}/{{.StepContextOutput}}`+"`"+` in your code.{{else}} Use `+"`"+`execute_shell_command`+"`"+` with full path (e.g., `+"`"+`echo '...' > '{{.StepExecutionPath}}/{{.StepContextOutput}}'`+"`"+`) or `+"`"+`diff_patch_workspace_file`+"`"+`.{{end}}
5. **File Existence**: {{if .IsCodeExecutionMode}}Before reading files in code, verify they exist (e.g., `+"`"+`os.path.exists()`+"`"+` in Python).{{else}}Use `+"`"+`execute_shell_command(command="ls ...", ...)`+"`"+` to verify files exist before reading.{{end}}
6. **Parallel Tools**: When you need multiple independent operations (e.g., reading several files, making unrelated tool calls), call them ALL in a single response for parallel execution.
{{if .IsCodeExecutionMode}}7. **Code Quality**: Read dependencies FIRST, parse with `+"`"+`json.loads()`+"`"+` before processing. For CSV/delimited text, use Python's `+"`"+`csv`+"`"+` module or `+"`"+`pandas`+"`"+`. Write one comprehensive script with helper functions rather than fragmented commands. Verify success programmatically — print "PASS: [detail]" or "FAIL: [reason]" + `+"`"+`sys.exit(1)`+"`"+`.{{end}}

{{if .PreviousStepsSummary}}
## Previous Steps Summary
{{.PreviousStepsSummary}}
{{end}}

{{if eq .HasLearnings "true"}}
## Learning Application (Secondary Guidance)
{{.LearningHistory}}

- **Workflows**: Use validated sequences from learnings, but adapt args to this specific step.
- **Patterns**: Use tool hints/error recovery patterns from learnings.
- **Conflict**: If learning conflicts with step requirement, the step wins.
{{if eq .KeepLearningFull "false"}}
- **Note**: These learnings are incomplete. Rely primarily on the step description and your own capabilities.
{{end}}
{{end}}

## File System Access (Folder Guard Enforced)
**Allowed READ paths**: {{.FolderGuardReadPaths}}
**Allowed WRITE paths**: {{.FolderGuardWritePaths}}

- **Step Folder**: '{{.StepExecutionPath}}/' - **VOLATILE**. Deleted on re-execution/restart. Only write your primary results here.
{{if eq .UseKnowledgebase "true"}}- **Knowledgebase**: '{{.KnowledgebasePath}}/' - **PERSISTENT**. Shared across all runs. Use for templates, reference data, or global configs that must survive across execution attempts.
{{end}}- **Rule**: Use the EXACT paths above. Read from any allowed read path, write only to allowed write paths. Path validation is strictly enforced.
- **Path Quoting**: Always wrap paths in single quotes in shell commands since folder names may contain spaces.

{{if .HasLoop}}
## Loop Execution
- **Condition**: {{.LoopCondition}}
- **Iteration**: {{.CurrentIteration}} / {{.MaxIterations}}
- **Action**: Update/Append to '{{.StepContextOutput}}' after EVERY iteration to preserve progress.
{{end}}

{{if .ValidationSchema}}
## Validation Schema (Output Requirement)
Your '{{.StepContextOutput}}' MUST match this structure:
{{printf "%s" .ValidationSchema}}
{{end}}

{{if eq .IsEvaluationMode "true"}}
## Evaluation Mode
You are running as an **evaluation agent** — your job is to **verify and assess** outputs from a previous execution run, NOT to create new artifacts.

- **Read** the target execution outputs referenced in your step description
- **Check** whether outputs meet the defined criteria (file existence, content correctness, data quality)
- **Write** your evaluation findings to your context_output file as structured JSON
- **Do NOT** re-execute or modify the original workflow outputs — only read and assess them
- Focus on evidence-based assessment: quote specific content from files, reference exact field values
{{end}}

{{if eq .SkipExecutionCleanup "true"}}
## State Verification Required (Skip Cleanup Mode)

Previous execution outputs are preserved. Existing progress files (tasks.md, todos.json, step outputs) may contain completed work from prior runs.

**IMPORTANT**: Do NOT assume existing "completed" state is still valid. Step configurations or requirements may have changed since the last run.

Before proceeding:
1. Review the CURRENT step description and success criteria carefully
2. Compare against any existing progress/todos to check alignment
3. If requirements changed, update todos or restart work as needed
4. Only consider tasks complete if they satisfy the CURRENT success criteria
{{end}}

{{if .DecisionEvaluationQuestion}}
## Output Formatting for Evaluation
**Evaluation Question**: {{.DecisionEvaluationQuestion}}
Include:
1. **Clear Status**: Succeeded or Failed.
2. **Evidence**: Specific details (file sizes, grep matches, API status codes) that answer the evaluation question.
{{end}}

## Completion
End your response with exactly one of:
- STATUS: COMPLETED — if '{{.StepContextOutput}}' was created successfully.
- STATUS: FAILED — if the step cannot be completed. Explain the reason.`)

var executionOnlyUserTemplate = MustRegisterTemplate("executionOnlyUser", `**DESCRIPTION**: {{.StepDescription}}
**LOCATION**: {{.StepExecutionPath}}/ (Workspace: {{.WorkspacePath}})

{{if eq .HasLoop "true"}}
### Loop: Iteration {{.CurrentIteration}} / {{.MaxIterations}}
**Stop Condition**: {{.LoopCondition}}
{{if .LoopDescription}}**Context**: {{.LoopDescription}}{{end}}
*Update/Append to {{.StepContextOutput}} after this iteration.*
{{end}}

{{if .PreviousIterationOutput}}
### Previous Attempt Results
{{.PreviousIterationOutput}}
*Adjust your approach to avoid repeating previous failures.*
{{end}}

{{if .ValidationFeedback}}
### Validation Issues
{{.ValidationFeedback}}
*Fix these errors in your next execution.*
{{end}}

{{if .HumanFeedback}}
### HUMAN GUIDANCE (MAX PRIORITY)
{{.HumanFeedback}}
**CRITICAL**: Strictly follow this guidance over all other instructions.
{{end}}

{{if .DecisionReasoning}}
### Routing Context
{{.DecisionReasoning}}
*Consider why you were routed to this step during execution.*
{{end}}

### Requirements
- **Inputs**: {{.StepContextDependencies}}
- **Output File**: {{.StepContextOutput}} (Create in '{{.StepExecutionPath}}/')`)

// WorkflowExecutionOnlyTemplate holds template variables for execution-only agent prompts
type WorkflowExecutionOnlyTemplate struct {
	StepTitle                  string
	StepDescription            string
	StepContextDependencies    string
	StepContextOutput          string
	WorkspacePath              string
	IsCodeExecutionMode        string // "true" or "false" - indicates if code execution mode is enabled
	ValidationFeedback         string
	HumanFeedback              string // Human guidance provided after validation failure (highest priority)
	PreviousIterationOutput    string // Previous loop iteration execution output (for loop steps)
	VariableNames              string // Variable names with descriptions ({{VAR_NAME}} - description)
	VariableValues             string // Variable names with actual values ({{VAR_NAME}} = value)
	HasLoop                    string // "true" or "false" as string
	LoopCondition              string // Loop condition description (required when HasLoop="true")
	LoopDescription            string // Human-readable explanation of the loop (optional)
	CurrentIteration           string // Current iteration number
	MaxIterations              string // Max iterations allowed
	LearningHistory            string // Formatted learning conversation history (REQUIRED for execution-only mode)
	LearningFilePaths          string // Learning file paths (when KeepLearningFull is false)
	StepNumber                 string // Step identifier (e.g., "step-8" or "step-3-if-true-0")
	StepExecutionPath          string // Full execution folder path (e.g., "execution/step-8")
	DecisionReasoning          string // Context from decision step that routed to this step (empty if not routed from decision)
	DecisionEvaluationQuestion string // Evaluation question for decision inner steps (used to format output for LLM evaluation)
	PreviousStepsSummary       string // Summary of previous completed steps (titles, descriptions, outputs)
}

// WorkflowExecutionOnlyAgent executes steps using pre-discovered learning context
// This agent does NOT discover learnings - it receives learning history from readLearningHistory() method
type WorkflowExecutionOnlyAgent struct {
	*agents.BaseOrchestratorAgent
}

// NewWorkflowExecutionOnlyAgent creates a new execution-only agent
func NewWorkflowExecutionOnlyAgent(config *agents.OrchestratorAgentConfig, logger loggerv2.Logger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) *WorkflowExecutionOnlyAgent {
	baseAgent := agents.NewBaseOrchestratorAgentWithEventBridge(
		config,
		logger,
		tracer,
		agents.TodoPlannerExecutionAgentType, // Reuse execution agent type for consistency
		eventBridge,
	)

	return &WorkflowExecutionOnlyAgent{
		BaseOrchestratorAgent: baseAgent,
	}
}

// Execute implements the OrchestratorAgent interface
func (hctpeoa *WorkflowExecutionOnlyAgent) Execute(ctx context.Context, templateVars map[string]string, conversationHistory []llmtypes.MessageContent) (string, []llmtypes.MessageContent, error) {
	// Generate system prompt and user message separately
	systemPrompt := hctpeoa.executionOnlySystemPromptProcessor(templateVars)
	userMessage := hctpeoa.executionOnlyUserMessageProcessor(templateVars)

	// Create a simple input processor that returns the user message
	inputProcessor := func(map[string]string) string {
		return userMessage
	}

	// Use ExecuteWithTemplateValidation with system prompt (overwrite=true to replace default MCP prompt with agent-specific prompt)
	return hctpeoa.BaseOrchestratorAgent.ExecuteWithTemplateValidation(ctx, templateVars, inputProcessor, conversationHistory, nil, systemPrompt, true)
}

// executionOnlySystemPromptProcessor generates the system prompt for execution-only agent
func (hctpeoa *WorkflowExecutionOnlyAgent) executionOnlySystemPromptProcessor(templateVars map[string]string) string {
	workspacePath := templateVars["WorkspacePath"]
	hasLoop := templateVars["HasLoop"] == "true"
	stepContextOutput := templateVars["StepContextOutput"]
	isCodeExecutionMode := templateVars["IsCodeExecutionMode"] == "true"
	learningHistory := templateVars["LearningHistory"]
	// Feature flag: KeepLearningFull (set by controller with priority: step config > env var > default false)
	keepLearningFullStr := templateVars["KeepLearningFull"]
	keepLearningFull := keepLearningFullStr == "true"
	stepNumber := templateVars["StepNumber"]               // e.g., "step-8" or "step-3-if-true-0"
	stepExecutionPath := templateVars["StepExecutionPath"] // e.g., "execution/step-8"
	previousStepsSummary := templateVars["PreviousStepsSummary"]
	knowledgebasePath := templateVars["KnowledgebasePath"] // Knowledgebase folder path (persistent files across runs)

	// Get current date and time
	now := time.Now()
	currentDate := now.Format("2006-01-02")
	currentTime := now.Format("15:04:05")

	// Get code execution instructions (reuse from builder.go)
	codeExecutionInstructions := ""
	if isCodeExecutionMode {
		// Get the reusable instructions - keep {{TOOL_STRUCTURE}} placeholder
		// agent.go will automatically replace it with actual tool structure when SetSystemPrompt is called
		// Pass workspacePath so {{.WorkspacePath}} is substituted with actual path in examples
		codeExecutionInstructions = prompt.GetCodeExecutionInstructions(workspacePath)
	}

	// Get tool search instructions (reuse from builder.go)
	toolSearchInstructions := ""
	useToolSearchMode := templateVars["UseToolSearchMode"] == "true"
	if useToolSearchMode {
		// Get the reusable instructions
		toolSearchInstructions = prompt.GetToolSearchInstructions()
	}

	// Get variable names and values for system prompt
	variableNames := templateVars["VariableNames"]
	variableValues := templateVars["VariableValues"]
	decisionEvaluationQuestion := templateVars["DecisionEvaluationQuestion"]
	validationSchema := templateVars["ValidationSchema"] // Validation schema JSON string
	folderGuardReadPaths := templateVars["FolderGuardReadPaths"]
	folderGuardWritePaths := templateVars["FolderGuardWritePaths"]

	// Read workflow memory from memory/memory.md (falls back to legacy instructions.md)
	customInstructions := ""
	if workspacePath != "" {
		memoryPath := workspacePath + "/memory/memory.md"
		if content, err := skills.ReadFile(memoryPath); err == nil && strings.TrimSpace(content) != "" {
			customInstructions = strings.TrimSpace(content)
		} else {
			// Fallback: legacy instructions.md
			instructionsPath := workspacePath + "/instructions.md"
			if content, err := skills.ReadFile(instructionsPath); err == nil && strings.TrimSpace(content) != "" {
				customInstructions = strings.TrimSpace(content)
			}
		}
	}

	// Execute the pre-parsed template
	var result strings.Builder
	err := executionOnlySystemTemplate.Execute(&result, map[string]interface{}{
		"WorkspacePath":              workspacePath,
		"CustomInstructions":         customInstructions,
		"IsCodeExecutionMode":        isCodeExecutionMode,
		"CodeExecutionInstructions":  codeExecutionInstructions,
		"UseToolSearchMode":          useToolSearchMode,
		"ToolSearchInstructions":     toolSearchInstructions,
		"HasLoop":                    hasLoop,
		"LoopCondition":              templateVars["LoopCondition"],
		"CurrentIteration":           templateVars["CurrentIteration"],
		"MaxIterations":              templateVars["MaxIterations"],
		"StepContextOutput":          stepContextOutput,
		"CurrentDate":                currentDate,
		"CurrentTime":                currentTime,
		"LearningHistory":            learningHistory,
		"HasLearnings":               fmt.Sprintf("%t", learningHistory != ""),
		"KeepLearningFull":           fmt.Sprintf("%t", keepLearningFull),
		"VariableNames":              variableNames,
		"VariableValues":             variableValues,
		"StepNumber":                 stepNumber,
		"StepExecutionPath":          stepExecutionPath,
		"PreviousStepsSummary":       previousStepsSummary,
		"DecisionEvaluationQuestion": decisionEvaluationQuestion,
		"ValidationSchema":           validationSchema,                        // Validation schema JSON string
		"KnowledgebasePath":          knowledgebasePath,                       // Knowledgebase folder path
		"UseKnowledgebase":           templateVars["UseKnowledgebase"],        // Whether knowledgebase is enabled
		"FolderGuardReadPaths":       folderGuardReadPaths,                    // Folder guard read paths for agent guidance
		"FolderGuardWritePaths":      folderGuardWritePaths,                   // Folder guard write paths for agent guidance
		"SkipExecutionCleanup":       templateVars["SkipExecutionCleanup"],    // Skip cleanup mode flag
		"IsEvaluationMode":           templateVars["IsEvaluationMode"],       // Evaluation mode flag
		"WorkflowRoot":               templateVars["WorkflowRoot"],           // Workflow root path for absolute cwd display
	})
	if err != nil {
		return fmt.Sprintf("Error executing execution-only system prompt template: %v", err)
	}

	return result.String()
}

// executionOnlyUserMessageProcessor generates the user message for execution-only agent
func (hctpeoa *WorkflowExecutionOnlyAgent) executionOnlyUserMessageProcessor(templateVars map[string]string) string {
	// Create template data
	templateData := WorkflowExecutionOnlyTemplate{
		StepTitle:               templateVars["StepTitle"],
		StepDescription:         templateVars["StepDescription"],
		StepContextDependencies: templateVars["StepContextDependencies"],
		StepContextOutput:       templateVars["StepContextOutput"],
		WorkspacePath:           templateVars["WorkspacePath"],
		IsCodeExecutionMode:     templateVars["IsCodeExecutionMode"],
		ValidationFeedback:      templateVars["ValidationFeedback"],
		HumanFeedback:           templateVars["HumanFeedback"],
		PreviousIterationOutput: templateVars["PreviousIterationOutput"],
		VariableNames:           templateVars["VariableNames"],
		VariableValues:          templateVars["VariableValues"],
		HasLoop:                 templateVars["HasLoop"],
		LoopCondition:           templateVars["LoopCondition"],
		LoopDescription:         templateVars["LoopDescription"],
		CurrentIteration:        templateVars["CurrentIteration"],
		MaxIterations:           templateVars["MaxIterations"],
		LearningHistory:         templateVars["LearningHistory"],
		LearningFilePaths:       templateVars["LearningFilePaths"],
		StepNumber:              templateVars["StepNumber"],
		StepExecutionPath:       templateVars["StepExecutionPath"],
		DecisionReasoning:       templateVars["DecisionReasoning"],
		PreviousStepsSummary:    templateVars["PreviousStepsSummary"],
	}

	// Execute the pre-parsed template
	var result strings.Builder
	if err := executionOnlyUserTemplate.Execute(&result, templateData); err != nil {
		return fmt.Sprintf("Error executing execution-only user message template: %v", err)
	}

	return result.String()
}
