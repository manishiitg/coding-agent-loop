package step_based_workflow

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
		learningDetailLevel = "general"
	}

	workspacePath := templateVars["WorkspacePath"]
	stepNumber := templateVars["StepNumber"]
	stepTitle := templateVars["StepTitle"]
	writePath := workspacePath + "/learnings/" + stepNumber
	scriptsPath := workspacePath + "/learnings/" + stepNumber + "/scripts"

	isExact := learningDetailLevel == "exact"

	templateStr := `# Learning Analysis Agent

## 🤖 Identity & Mode
- **Role**: Learning Agent (Efficiency Optimizer)
- **Mode**: {{.Mode}}
- **Focus**: {{if .IsExact}}Extract WORKFLOW-CENTRIC execution sequence with dependencies and data flow.{{else}}Extract tool names and high-level patterns + Python scripts.{{end}}

## 🚨 CRITICAL LEARNING PRINCIPLES
1. **Task-Specific ONLY**: Only save learnings that help a future agent perform *this specific task* better.
2. **Exclude General Knowledge**: 
   - ❌ Syntax/Compilation errors (LLMs already know Go/Python rules).
   - ❌ Internal workspace tools (read_workspace_file, etc.).
   - ❌ Generic naming or formatting feedback.
3. **Include (The "Best Stuff")**:
   - ✅ **Patterns**: MCP tool calling sequences ('server.tool') with arguments.
   - ✅ **Success Criteria**: Exact JSON structures, field names, and data types found in outputs.
   - ✅ **Failures to Avoid**: Task-specific dead-ends (e.g., "Tool X doesn't work for PDF extraction in this repo").
   - ✅ **Scripts**: Full content of successful Python scripts (save to '{{.ScriptsPath}}').

## 🔄 FILE MANAGEMENT ALGORITHM (MANDATORY)
1. **Discover**: Call 'list_workspace_files' on '{{.WritePath}}'. Identify all existing '*_learning.md' files.
2. **Retrieve**: Read ALL identified learning files.
3. **Consolidate**:
   - Merge current execution findings with all history.
   - **Prioritize Latest Success**: Latest successful logs override older successful logs.
   - **Update Scores**: Format: '[Runs: X | Success: Y%]'.
   - **Prune**: Remove patterns mismatched with the current step description.
4. **Persist**: Write ONE final consolidated file to '{{.WritePath}}/{{.StepTitle}}_learning.md'.
5. **Clean Up**: Use 'delete_workspace_file' to remove all other '*_learning.md' files in that folder. **Only the final file should remain.**

## 📤 OUTPUT FORMAT
{{if .IsExact}}
### 🎯 EXECUTION WORKFLOW (EXACT MODE)
⭐ **OPTIMAL PATH** [Runs: X | Success: Y%]
1. **server.tool**:
   - arguments: {COMPLETE JSON - replace hardcode paths with {{ "{{" }}WORKSPACE_PATH{{ "}}" }} }
   - prerequisites: [Condition]
   - outputs: [Description]
   - on_error: [Specific recovery]

### 📊 DATA FLOW
Step 1 Output -> Step 2 Input. Trace the flow accurately.
{{else}}
### ✅ SUCCESS PATTERN
- **Tools**: server.tool [Runs: X | Success: Y%]
- **Approach**: Brief description of the strategy.
{{end}}

### 📄 OUTPUT FILE FORMATS
- **File**: filename.json
- **Structure**: { "field": "type" } - Provide exact structure for consistency.

### ❌ FAILURES TO AVOID
- server.tool [Failed: X] - [Task-specific reason]. Use [Correct Approach] instead.

## 📤 FINAL ACTION
After cleanup, output ONLY the file path:
'Updated: {{.WritePath}}/{{.StepTitle}}_learning.md'
Do not add summaries or talkative reports.`

	tmpl, err := template.New("learningSystemPrompt").Parse(templateStr)
	if err != nil {
		return "Error parsing learning system prompt template: " + err.Error()
	}
	var result strings.Builder
	if err := tmpl.Execute(&result, map[string]interface{}{
		"Mode":        strings.ToUpper(learningDetailLevel),
		"IsExact":     isExact,
		"WritePath":   writePath,
		"ScriptsPath": scriptsPath,
		"StepTitle":   stepTitle,
	}); err != nil {
		return "Error executing learning system prompt template: " + err.Error()
	}

	return result.String()
}

// learningUserMessageProcessor creates the user message that always instructs to capture both success and failure patterns
// learningUserMessageProcessor creates the user message that always instructs to capture both success and failure patterns
func (agent *WorkflowLearningAgent) learningUserMessageProcessor(templateVars map[string]string) string {
	learningDetailLevel := templateVars["LearningDetailLevel"]
	if learningDetailLevel == "" {
		learningDetailLevel = "general"
	}

	workspacePath := templateVars["WorkspacePath"]
	stepNumber := templateVars["StepNumber"]
	stepTitle := templateVars["StepTitle"]
	writePath := workspacePath + "/learnings/" + stepNumber

	isExact := learningDetailLevel == "exact"

	templateStr := `# Learning Task: {{if .IsExact}}Workflow Extraction{{else}}Tool Extraction{{end}}

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
2. **EXTRACT**:
   - {{if .IsExact}}Extract the COMPLETE, REPLAYABLE sequence of MCP tool calls.{{else}}Extract successful tool names and Python recipes.{{end}}
   - **Task-Specific Failures**: Document what failed for *this specific task* (ignore general Go/Python errors).
3. **PERSIST & CLEAN**:
   - Write ONE consolidated file to '{{.WritePath}}/{{.StepTitle}}_learning.md'.
   - Delete all other '*_learning.md' files in that folder.

## 🔑 Variable Handling
- Replace hardcoded IDs/paths with {{ "{{" }}VARIABLE_NAME{{ "}}" }} placeholders: {{.Variables}}
- **Workspace Paths**: Always replace with {{ "{{" }}WORKSPACE_PATH{{ "}}" }} or relative paths.

---
## 📊 EXECUTION HISTORY
{{.ExecutionHistory}}

---
## ✅ VALIDATION RESULTS
{{.ValidationResult}}

**Final Action**: Output ONLY the file path 'Updated: {{.WritePath}}/{{.StepTitle}}_learning.md'.`

	tmpl, err := template.New("learningUserMessage").Parse(templateStr)
	if err != nil {
		return "Error parsing learning user message template: " + err.Error()
	}
	var result strings.Builder
	if err := tmpl.Execute(&result, map[string]interface{}{
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
