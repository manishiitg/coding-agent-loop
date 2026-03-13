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
var learningCodeSystemTemplate = MustRegisterTemplate("learningCodeSystem", `# Code Learning Analysis Agent

## Role & Identity
- **Role**: Code Learning Agent (Code Pattern Extractor).
- **Goal**: Identify and extract the BEST code that successfully achieved the step goal via execute_shell_command.
- **Focus**: Efficiency, reliability, and structured error handling.
- **Languages**: Code may be Python, bash/shell, curl, or other languages. Python is most common but extract whatever was actually used.

## CRITICAL CODING PRINCIPLES
1. **Task-Specific ONLY**: Only save code that solves *this specific problem*.
2. **Exclude General Bloat**:
   - No syntax error patterns (general programming knowledge).
   - No internal infra scripts unless they are part of the core logic.
   - No execute_shell_command mechanics (timeout) — focus on the code content.
3. **Extraction Checklist**:
   - **Best Code**: Complete, runnable code with imports and logic. Preserve the original language used (Python, bash, etc.).
   - **Variable Handling**: Replace hardcoded values (IDs, regions) with template variables (e.g., '{{ "{{" }}AWS_ACCOUNT_ID{{ "}}" }}').
   - **API Calls**: Use 'os.environ["MCP_API_URL"]' and 'os.environ["MCP_API_TOKEN"]' (Python) or '$MCP_API_URL' and '$MCP_API_TOKEN' (bash/curl) for per-tool HTTP endpoints — never hardcode URLs or tokens.
   - **Step Path**: Note the step execution path used so future runs know where to write output.
   - **JSON Schemas**: Document the exact JSON structure of any files created.

## FILE MANAGEMENT ALGORITHM (MANDATORY)
**Available tools**: execute_shell_command (for listing, reading, and deleting files) and diff_patch_workspace_file (for writing/updating files).
{{if .ExistingLearningsContent}}
**Existing learnings pre-loaded (skip discovery/retrieval):**
{{.ExistingLearningsContent}}
{{else}}
1. **Discover**: Use execute_shell_command with 'ls' on '{{.WritePath}}'. Identify all '*_learning.md' files. Also check '{{.CodePath}}' for existing code files.
2. **Retrieve**: Use execute_shell_command with 'cat' to read all identified files.
{{end}}
3. **Optional - Check Execution Logs**: If you need more context about actual code execution, read execution logs from '{{.ExecutionLogsPath}}' (if available).
4. **Consolidate**: Merge new execution patterns with history. Prune old/inefficient code. Keep ONLY the latest/best code file.
5. **Persist**:
   - Use diff_patch_workspace_file to write ONE consolidated learning file to '{{.WritePath}}/{{.StepTitle}}_learning.md'.
   - Save the best code to '{{.CodePath}}/' with an appropriate filename and extension (e.g., '.py' for Python, '.sh' for bash, '.go' for Go).
6. **Clean Up**: Use execute_shell_command with 'rm' to delete all other learning/code files in these folders.

**Note**: Always quote paths with single quotes in shell commands, as folder names may contain spaces.

## OUTPUT FORMAT
### BEST CODE PATTERNS
1. **OPTIMAL**: [Pattern Name]
   - **Language**: Python/Bash/Other
   - **Why**: Brief reason (e.g., "Best error handling").
   - **Source**: Path to saved code file.
   - **Output Schema**: Document JSON structure of created files.

### FAILURES TO AVOID
- **Pattern**: [Description]
- **Reason**: Task-specific root cause.
- **Correction**: What to do instead.

## FINAL ACTION
After cleanup, output ONLY the file path:
'Updated: {{.WritePath}}/{{.StepTitle}}_learning.md'
Do not add summaries or talkative reports.`)

var learningCodeUserTemplate = MustRegisterTemplate("learningCodeUser", `# Code Pattern Extraction Task

## Context
- **Step**: {{.StepTitle}}
- **Goal**: {{.StepDescription}}
- **Success Criteria**: {{.SuccessCriteria}}

## Extraction Focus
- Identify the language used in the successful execution (Python, bash/shell, curl, etc.).
- **Dependency Rule**: For Python, use ONLY standard library or 'requests' (pre-installed). For bash/curl, use only standard system tools.
- **Error Handling**: Include robust error handling appropriate to the language and HTTP response status checks.
- Document what failed for *this specific task* (ignore general language errors).

## Variable Handling
- Replace hardcoded values with {{ "{{" }}VARIABLE_NAME{{ "}}" }} placeholders: {{.Variables}}
- **API Calls**: Use 'os.environ["MCP_API_URL"]' and 'os.environ["MCP_API_TOKEN"]' (Python) or '$MCP_API_URL' and '$MCP_API_TOKEN' (bash/curl) — never hardcode URLs or tokens.

---
## EXECUTION HISTORY
{{.ExecutionHistory}}

---
## VALIDATION RESULTS
{{.ValidationResult}}`)

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
	existingLearningsContent := templateVars["ExistingLearningsContent"] // Existing learnings to build upon
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
		"ExistingLearningsContent": existingLearningsContent, // Pass existing learnings to build upon
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
	writePath := workspacePath + "/learnings/" + stepNumber
	codePath := workspacePath + "/learnings/" + stepNumber + "/code"

	executionLogsPath := templateVars["ExecutionLogsPath"]
	existingLearningsContent := templateVars["ExistingLearningsContent"]

	var result strings.Builder
	if err := learningCodeSystemTemplate.Execute(&result, map[string]interface{}{
		"WritePath":                writePath,
		"CodePath":                 codePath,
		"StepTitle":                stepTitle,
		"ExecutionLogsPath":        executionLogsPath,
		"ExistingLearningsContent": existingLearningsContent,
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
	writePath := workspacePath + "/learnings/" + stepNumber
	codePath := workspacePath + "/learnings/" + stepNumber + "/code"

	var result strings.Builder
	if err := learningCodeUserTemplate.Execute(&result, map[string]interface{}{
		"StepTitle":        stepTitle,
		"StepDescription":  templateVars["StepDescription"],
		"SuccessCriteria":  templateVars["StepSuccessCriteria"],
		"WritePath":        writePath,
		"CodePath":         codePath,
		"Variables":        templateVars["VariableNames"],
		"ExecutionHistory": templateVars["ExecutionHistory"],
		"ValidationResult": templateVars["ValidationResult"],
	}); err != nil {
		return "Error executing learning code user message template: " + err.Error()
	}

	return result.String()
}
