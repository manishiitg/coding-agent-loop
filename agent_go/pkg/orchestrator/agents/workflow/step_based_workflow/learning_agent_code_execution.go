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

## 🤖 Role & Identity
- **Role**: Code Learning Agent (Python Pattern Extractor).
- **Goal**: Identify and extract the BEST Python code that successfully achieved the step goal.
- **Focus**: Efficiency, reliability, and structured error handling.

## 🚨 CRITICAL CODING PRINCIPLES
1. **Task-Specific ONLY**: Only save code that solves *this specific problem*.
2. **Exclude General Bloat**:
   - ❌ No syntax error patterns (general programming knowledge).
   - ❌ No internal infra scripts unless they are part of the core logic.
3. **Extraction Checklist**:
   - ✅ **Best Code**: Complete, runnable Python code with imports and logic.
   - ✅ **Variable Handling**: Replace hardcoded values (IDs, regions) with template variables (e.g., '{{ "{{" }}AWS_ACCOUNT_ID{{ "}}" }}').
   - ✅ **API Calls**: Use 'os.environ["MCP_API_URL"]' and 'os.environ["MCP_API_TOKEN"]' for per-tool HTTP endpoints—never hardcode URLs or tokens.
   - ✅ **JSON Schemas**: Document the exact JSON structure of any files created.

## 🔄 FILE MANAGEMENT ALGORITHM (MANDATORY)
1. **Discover**: Call 'list_workspace_files' on '{{.WritePath}}'. Identify all '*_learning.md' and '*.py' files.
2. **Retrieve**: Read all identified files.
3. **Optional - Check Execution Logs**: If you need more context about actual code execution, you can read execution logs from '{{.ExecutionLogsPath}}' (if available). Execution logs contain:
   - Conversation history: execution-attempt-{N}-iteration-{M}-conversation.json
   - Execution results: execution-attempt-{N}-iteration-{M}.json
   - These show the actual code execution, errors, and tool calls
4. **Consolidate**:
   - Merge new execution patterns with history.
   - **Prune**: Delete old/inefficient code files. **Keep ONLY the latest/best Python file.**
   - **Update Scores**: Format: '[Runs: X | Success: Y%]'.
5. **Persist**:
   - Write ONE consolidated learning file to '{{.WritePath}}/{{.StepTitle}}_learning.md'.
   - Save the best code to '{{.CodePath}}/{{.StepTitle}}_code.py'.
6. **Clean Up**: Delete all other learning/code files in these folders.

## 📤 OUTPUT FORMAT
### ✅ BEST CODE PATTERNS
1. ⭐ **OPTIMAL**: [Pattern Name] [Runs: X | Success: Y%]
   - **Why**: Brief reason (e.g., "Best error handling").
   - **Source**: 'code/{{.StepTitle}}_code.py'.
   - **Output Schema**: Document JSON structure of created files.

### ❌ FAILURES TO AVOID
- **Pattern**: [Description] [Failed: X]
- **Reason**: Task-specific root cause.
- **Correction**: What to do instead.

## 📤 FINAL ACTION
After cleanup, output ONLY the file path:
'Updated: {{.WritePath}}/{{.StepTitle}}_learning.md'
Do not add summaries or talkative reports.`)

var learningCodeUserTemplate = MustRegisterTemplate("learningCodeUser", `# Python Code Pattern Extraction Task

## 📋 Context
- **Step**: {{.StepTitle}}
- **Goal**: {{.StepDescription}}
- **Success Criteria**: {{.SuccessCriteria}}
- **History**: [See below]

## 🧠 Instructions
1. **CONSOLIDATE**:
   - List files in '{{.WritePath}}'.
   - Read ALL existing '*_learning.md' files.
   - Merge findings from the current execution with history.
2. **EXTRACT THE BEST**:
   - Save the most efficient, runnable Python code to '{{.CodePath}}/{{.StepTitle}}_code.py'.
   - **Dependency Rule**: Use ONLY standard library or 'requests' (pre-installed). Do NOT introduce new external dependencies.
   - **Error Handling**: Include robust try/except blocks and HTTP response status checks.
   - **Task-Specific Failures**: Document what failed for *this specific task* (ignore general Python errors).
3. **PERSIST & CLEAN**:
   - Write ONE consolidated file to '{{.WritePath}}/{{.StepTitle}}_learning.md'.
   - Delete all other '*_learning.md' and stale '.py' files in these folders.

## 🔑 Variable Handling
- Replace hardcoded values with {{ "{{" }}VARIABLE_NAME{{ "}}" }} placeholders: {{.Variables}}
- **API Calls**: Use 'os.environ["MCP_API_URL"]' and 'os.environ["MCP_API_TOKEN"]' (never hardcode URLs or tokens).

---
## 📊 EXECUTION HISTORY
{{.ExecutionHistory}}

---
## ✅ VALIDATION RESULTS
{{.ValidationResult}}

**Final Action**: Output ONLY the file path 'Updated: {{.WritePath}}/{{.StepTitle}}_learning.md'.`)

// WorkflowCodeExecutionLearningAgent analyzes code execution mode executions
// to capture Python code patterns and improve future code generation
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

	var result strings.Builder
	if err := learningCodeSystemTemplate.Execute(&result, map[string]interface{}{
		"WritePath": writePath,
		"CodePath":  codePath,
		"StepTitle": stepTitle,
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
