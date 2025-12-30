package todo_creation_human

import (
	"context"
	"strings"
	"text/template"

	"mcp-agent-builder-go/agent_go/pkg/orchestrator/agents"
	mcpagent "mcpagent/agent"
	loggerv2 "mcpagent/logger/v2"
	"mcpagent/observability"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

// HumanControlledTodoPlannerCodeExecutionLearningAgent analyzes code execution mode executions
// to capture Go code patterns and improve future code generation
type HumanControlledTodoPlannerCodeExecutionLearningAgent struct {
	*agents.BaseOrchestratorAgent
}

// NewHumanControlledTodoPlannerCodeExecutionLearningAgent creates a new code execution learning agent
func NewHumanControlledTodoPlannerCodeExecutionLearningAgent(config *agents.OrchestratorAgentConfig, logger loggerv2.Logger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) *HumanControlledTodoPlannerCodeExecutionLearningAgent {
	baseAgent := agents.NewBaseOrchestratorAgentWithEventBridge(
		config,
		logger,
		tracer,
		agents.TodoPlannerSuccessLearningAgentType,
		eventBridge,
	)

	return &HumanControlledTodoPlannerCodeExecutionLearningAgent{
		BaseOrchestratorAgent: baseAgent,
	}
}

// Execute implements the OrchestratorAgent interface for code execution mode learning
func (agent *HumanControlledTodoPlannerCodeExecutionLearningAgent) Execute(ctx context.Context, templateVars map[string]string, conversationHistory []llmtypes.MessageContent) (string, []llmtypes.MessageContent, error) {
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
	templateData := HumanControlledTodoPlannerLearningTemplate{
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
func (agent *HumanControlledTodoPlannerCodeExecutionLearningAgent) learningSystemPromptProcessorCodeExecution(templateVars map[string]string) string {
	workspacePath := templateVars["WorkspacePath"]
	stepNumber := templateVars["StepNumber"]
	stepTitle := templateVars["StepTitle"]
	writePath := workspacePath + "/learnings/" + stepNumber
	codePath := workspacePath + "/learnings/" + stepNumber + "/code"

	templateStr := `# Code Learning Analysis Agent

## 🤖 Role & Identity
- **Role**: Code Learning Agent (Go Pattern Extractor).
- **Goal**: Identify and extract the BEST Go code that successfully achieved the step goal. 
- **Focus**: Efficiency, reliability, and structured error handling.

## 🚨 CRITICAL CODING PRINCIPLES
1. **Task-Specific ONLY**: Only save code that solves *this specific problem*.
2. **Exclude General Bloat**: 
   - ❌ No syntax or compilation error patterns (general programming knowledge).
   - ❌ No internal infra scripts unless they are part of the core logic.
3. **Extraction Checklist**:
   - ✅ **Best Code**: Complete, runnable Go code with imports and logic.
   - ✅ **Variable Handling**: Replace hardcoded values (IDs, regions) with template variables (e.g., '{{ "{{" }}AWS_ACCOUNT_ID{{ "}}" }}').
   - ✅ **Workspace Paths**: Use 'os.Args[1]' and 'filepath.Join'—never hardcode paths.
   - ✅ **JSON Schemas**: Document the exact JSON structure of any files created.

## 🔄 FILE MANAGEMENT ALGORITHM (MANDATORY)
1. **Discover**: Call 'list_workspace_files' on '{{.WritePath}}'. Identify all '*_learning.md' and '*.go' files.
2. **Retrieve**: Read all identified files.
3. **Consolidate**:
   - Merge new execution patterns with history.
   - **Prune**: Delete old/inefficient code files. **Keep ONLY the latest/best Go file.**
   - **Update Scores**: Format: '[Runs: X | Success: Y%]'.
4. **Persist**: 
   - Write ONE consolidated learning file to '{{.WritePath}}/{{.StepTitle}}_learning.md'.
   - Save the best code to '{{.CodePath}}/{{.StepTitle}}_code.go'.
5. **Clean Up**: Delete all other learning/code files in these folders.

## 📤 OUTPUT FORMAT
### ✅ BEST CODE PATTERNS
1. ⭐ **OPTIMAL**: [Pattern Name] [Runs: X | Success: Y%]
   - **Why**: Brief reason (e.g., "Best error handling").
   - **Source**: 'code/{{.StepTitle}}_code.go'.
   - **Output Schema**: Document JSON structure of created files.

### ❌ FAILURES TO AVOID
- **Pattern**: [Description] [Failed: X]
- **Reason**: Task-specific root cause.
- **Correction**: What to do instead.

## 📤 FINAL ACTION
After cleanup, output ONLY the file path:
'Updated: {{.WritePath}}/{{.StepTitle}}_learning.md'
Do not add summaries or talkative reports.`

	tmpl, err := template.New("learningCodeSystemPrompt").Parse(templateStr)
	if err != nil {
		return "Error parsing learning code system prompt template: " + err.Error()
	}
	var result strings.Builder
	if err := tmpl.Execute(&result, map[string]interface{}{
		"WritePath": writePath,
		"CodePath":  codePath,
		"StepTitle": stepTitle,
	}); err != nil {
		return "Error executing learning code system prompt template: " + err.Error()
	}

	return result.String()
}

// learningUserMessageProcessorCodeExecution creates the user message for code execution mode learning
// learningUserMessageProcessorCodeExecution creates the user message for code execution mode learning
func (agent *HumanControlledTodoPlannerCodeExecutionLearningAgent) learningUserMessageProcessorCodeExecution(templateVars map[string]string) string {
	workspacePath := templateVars["WorkspacePath"]
	stepNumber := templateVars["StepNumber"]
	stepTitle := templateVars["StepTitle"]
	writePath := workspacePath + "/learnings/" + stepNumber
	codePath := workspacePath + "/learnings/" + stepNumber + "/code"

	templateStr := `# Go Code Pattern Extraction Task

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
   - Save the most efficient, runnable Go code to '{{.CodePath}}/{{.StepTitle}}_code.go'.
   - **Dependency Rule**: Use ONLY standard library or packages already in the workspace 'go.mod'. Do NOT introduce new external dependencies.
   - **Error Handling**: Include robust 'if err != nil' checks.
   - **Task-Specific Failures**: Document what failed for *this specific task* (ignore general Go errors).
3. **PERSIST & CLEAN**:
   - Write ONE consolidated file to '{{.WritePath}}/{{.StepTitle}}_learning.md'.
   - Delete all other '*_learning.md' and stale '.go' files in these folders.

## 🔑 Variable Handling
- Replace hardcoded values with {{ "{{" }}VARIABLE_NAME{{ "}}" }} placeholders: {{.Variables}}
- **Workspace Paths**: Use 'os.Args[1]' and 'filepath.Join' (never hardcode full paths).

---
## 📊 EXECUTION HISTORY
{{.ExecutionHistory}}

---
## ✅ VALIDATION RESULTS
{{.ValidationResult}}

**Final Action**: Output ONLY the file path 'Updated: {{.WritePath}}/{{.StepTitle}}_learning.md'.`

	tmpl, err := template.New("learningCodeUserMessage").Parse(templateStr)
	if err != nil {
		return "Error parsing learning code user message template: " + err.Error()
	}
	var result strings.Builder
	if err := tmpl.Execute(&result, map[string]interface{}{
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
