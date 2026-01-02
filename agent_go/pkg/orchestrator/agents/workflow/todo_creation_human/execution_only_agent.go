package todo_creation_human

import (
	"context"
	"fmt"
	"strings"
	"text/template"
	"time"

	"mcp-agent-builder-go/agent_go/pkg/orchestrator/agents"
	mcpagent "mcpagent/agent"
	"mcpagent/agent/prompt"
	loggerv2 "mcpagent/logger/v2"
	"mcpagent/observability"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

// HumanControlledTodoPlannerExecutionOnlyTemplate holds template variables for execution-only agent prompts
type HumanControlledTodoPlannerExecutionOnlyTemplate struct {
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
	OtherAgentsCapabilities    string // Summary of other sub-agents' capabilities (only for sub-agents in orchestration steps)
}

// HumanControlledTodoPlannerExecutionOnlyAgent executes steps using pre-discovered learning context
// This agent does NOT discover learnings - it receives learning history from readLearningHistory() method
type HumanControlledTodoPlannerExecutionOnlyAgent struct {
	*agents.BaseOrchestratorAgent
}

// NewHumanControlledTodoPlannerExecutionOnlyAgent creates a new execution-only agent
func NewHumanControlledTodoPlannerExecutionOnlyAgent(config *agents.OrchestratorAgentConfig, logger loggerv2.Logger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) *HumanControlledTodoPlannerExecutionOnlyAgent {
	baseAgent := agents.NewBaseOrchestratorAgentWithEventBridge(
		config,
		logger,
		tracer,
		agents.TodoPlannerExecutionAgentType, // Reuse execution agent type for consistency
		eventBridge,
	)

	return &HumanControlledTodoPlannerExecutionOnlyAgent{
		BaseOrchestratorAgent: baseAgent,
	}
}

// Execute implements the OrchestratorAgent interface
func (hctpeoa *HumanControlledTodoPlannerExecutionOnlyAgent) Execute(ctx context.Context, templateVars map[string]string, conversationHistory []llmtypes.MessageContent) (string, []llmtypes.MessageContent, error) {
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
func (hctpeoa *HumanControlledTodoPlannerExecutionOnlyAgent) executionOnlySystemPromptProcessor(templateVars map[string]string) string {
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
	otherAgentsCapabilities := templateVars["OtherAgentsCapabilities"]
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
		codeExecutionInstructions = prompt.GetCodeExecutionInstructions()
	}

	// Get variable names and values for system prompt
	variableNames := templateVars["VariableNames"]
	variableValues := templateVars["VariableValues"]
	prerequisiteRulesInfo := templateVars["PrerequisiteRulesInfo"]
	decisionEvaluationQuestion := templateVars["DecisionEvaluationQuestion"]
	validationSchema := templateVars["ValidationSchema"] // Validation schema JSON string

	// Define the system prompt template
	templateStr := `# Execution-Only Agent

## 📅 Context: {{.CurrentDate}} | {{.CurrentTime}}

## 🤖 Role & Responsibility
- **Identity**: Execution-Only Agent (Focused on completion, not discovery).
- **Goal**: Execute the current plan step using MCP tools or Go code.
{{if .LearningHistory}}- **Context**: Pre-discovered learning history available (read-only reference).{{end}}

{{if .IsCodeExecutionMode}}
## ⚡ Code Execution Mode
{{.CodeExecutionInstructions}}
{{end}}

{{if .VariableNames}}
## 🔑 Variables
{{.VariableNames}}
{{if .VariableValues}}**Values**: {{.VariableValues}}{{end}}

**Handling**: Step descriptions are already resolved. For Go code/tool calls, use the resolved values directly.
{{end}}

## 🚨 CRITICAL EXECUTION RULES
1. **Source of Truth**: The **Step Description** defines WHAT to do. It ALWAYS overrides learnings.
2. **Workspace Paths**:
   - **Go Code**: 'basePath := os.Args[1]'. ALWAYS use 'filepath.Join(basePath, "relative/path")'.
   - **Tool Args**: Pass '{{.WorkspacePath}}' as the first argument in 'args'.
   - **NEVER hardcode absolute paths** (e.g., /Users/...) as they change between runs.
3. **Pre-requisites**: Read all **Context Dependencies** before execution. They are inputs.
4. **Mandatory Output**: Create '{{.StepExecutionPath}}/{{.StepContextOutput}}' matching the provided schema.

{{if .PreviousStepsSummary}}
## 📋 Previous Steps Summary
{{.PreviousStepsSummary}}
{{end}}

{{if .PrerequisiteRulesInfo}}
## 📏 Project Rules
{{.PrerequisiteRulesInfo}}
{{end}}

{{if .OtherAgentsCapabilities}}
## 🤖 Other Agents
{{.OtherAgentsCapabilities}}
{{end}}

{{if .LearningHistory}}
## 📚 Learning Application (Secondary Guidance)
{{if eq .KeepLearningFull "true"}}{{.LearningHistory}}{{end}}

- **Workflows**: Use validated sequences from learnings, but adapt args to this specific step.
- **Patterns**: Use tool hints/error recovery patterns from learnings.
- **Conflict**: If learning conflicts with step requirement, the step wins.
{{if eq .KeepLearningFull "false"}}
- **Exploration Phase**: You are in an early learning phase. While learning files are available (see below), you are encouraged to **explore alternative or optimized approaches** to achieve the step goal.
- **Access**: Full learning files are listed in the user message. Read them if you need guidance, but feel free to innovate if you see a better path.
{{end}}
{{end}}

## 📁 File System Access
- **READ**: 'learnings/', 'execution/' (previous steps), 'knowledgebase/'.
- **WRITE/CLEANUP**:
- **Step Folder**: '{{.StepExecutionPath}}/' - **VOLATILE**. Deleted on re-execution/restart. Only write your primary results here.
- **Knowledgebase**: '{{.KnowledgebasePath}}/' - **PERSISTENT**. Shared across all runs. Use for templates, reference data, or global configs that must survive across execution attempts. Path validation is enforced.
- **Rule**: Read from any allowed folder (learnings, execution, knowledgebase), but only write to your specific step folder or the persistent knowledgebase.

{{if .HasLoop}}
## 🔄 Loop Execution
- **Condition**: {{.LoopCondition}}
- **Iteration**: {{.CurrentIteration}} / {{.MaxIterations}}
- **Action**: Update/Append to '{{.StepContextOutput}}' after EVERY iteration to preserve progress.
{{end}}

{{if .IsCodeExecutionMode}}
## 💻 Advanced Code Patterns
- **JSON Safety**: Read dependencies FIRST, define Go structs matching their JSON tags, then parse.
- **Verification**: Programmatically verify success. Print "✅ PASS: [detail]" or "❌ FAIL: [reason]" + 'os.Exit(1)'.
- **Repeatability**: Write one comprehensive program with helper functions rather than fragmented scripts.
{{end}}

{{if .ValidationSchema}}
## ✅ Validation Schema (Output Requirement)
Your '{{.StepContextOutput}}' MUST match this structure:
{{printf "%s" .ValidationSchema}}
{{end}}

{{if .DecisionEvaluationQuestion}}
## 🤖 Output Formatting for Evaluation
**Evaluation Question**: {{.DecisionEvaluationQuestion}}
Include:
1. **Clear Status**: Succeeded or Failed.
2. **Evidence**: Specific details (file sizes, grep matches, API status codes) that answer the evaluation question.
{{end}}

## 📤 Output Format
**Status**: [COMPLETED/FAILED/IN_PROGRESS]  
**Actions**: Tools used + results  
**Evidence**: Proof of completion  
**Context Output**: Path to file created`

	// Parse and execute the template
	tmpl, err := template.New("executionOnlySystemPrompt").Parse(templateStr)
	if err != nil {
		return fmt.Sprintf("Error parsing execution-only system prompt template: %v", err)
	}

	var result strings.Builder
	err = tmpl.Execute(&result, map[string]interface{}{
		"WorkspacePath":              workspacePath,
		"IsCodeExecutionMode":        isCodeExecutionMode,
		"CodeExecutionInstructions":  codeExecutionInstructions,
		"HasLoop":                    hasLoop,
		"StepContextOutput":          stepContextOutput,
		"CurrentDate":                currentDate,
		"CurrentTime":                currentTime,
		"LearningHistory":            learningHistory,
		"KeepLearningFull":           fmt.Sprintf("%t", keepLearningFull),
		"VariableNames":              variableNames,
		"VariableValues":             variableValues,
		"StepNumber":                 stepNumber,
		"StepExecutionPath":          stepExecutionPath,
		"PreviousStepsSummary":       previousStepsSummary,
		"OtherAgentsCapabilities":    otherAgentsCapabilities,
		"PrerequisiteRulesInfo":      prerequisiteRulesInfo,
		"DecisionEvaluationQuestion": decisionEvaluationQuestion,
		"ValidationSchema":           validationSchema,  // Validation schema JSON string
		"KnowledgebasePath":          knowledgebasePath, // Knowledgebase folder path
	})
	if err != nil {
		return fmt.Sprintf("Error executing execution-only system prompt template: %v", err)
	}

	return result.String()
}

// executionOnlyUserMessageProcessor generates the user message for execution-only agent
func (hctpeoa *HumanControlledTodoPlannerExecutionOnlyAgent) executionOnlyUserMessageProcessor(templateVars map[string]string) string {
	// Create template data
	templateData := HumanControlledTodoPlannerExecutionOnlyTemplate{
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

	// Define the user message template
	templateStr := `## 🎯 Task: {{.StepTitle}}

**DESCRIPTION**: {{.StepDescription}}  
**LOCATION**: {{.StepExecutionPath}}/ (Workspace: {{.WorkspacePath}})

{{if eq .HasLoop "true"}}
### 🔄 Loop: Iteration {{.CurrentIteration}} / {{.MaxIterations}}
**Stop Condition**: {{.LoopCondition}}  
{{if .LoopDescription}}**Context**: {{.LoopDescription}}{{end}}
*Update/Append to {{.StepContextOutput}} after this iteration.*
{{end}}

{{if .PreviousIterationOutput}}
### 🔄 Previous Attempt Results
{{.PreviousIterationOutput}}
*Adjust your approach to avoid repeating previous failures.*
{{end}}

{{if .ValidationFeedback}}
### ⚠️ Validation Issues
{{.ValidationFeedback}}
*Fix these errors in your next execution.*
{{end}}

{{if .HumanFeedback}}
### 👤 HUMAN GUIDANCE (MAX PRIORITY)
{{.HumanFeedback}}
**CRITICAL**: Strictly follow this guidance over all other instructions.
{{end}}

{{if .DecisionReasoning}}
### 🎯 Routing Context
{{.DecisionReasoning}}
*Consider why you were routed to this step during execution.*
{{end}}

{{if .LearningFilePaths}}
### 📚 Learning Resources
Full details available in:
{{.LearningFilePaths}}
*Use read_workspace_file to access these if needed.*
{{end}}

### 📋 Requirements
- **Inputs**: {{.StepContextDependencies}}  
- **Output File**: {{.StepContextOutput}} (Create in {{.StepExecutionPath}}/)`

	// Parse and execute the template
	tmpl, err := template.New("executionOnlyUserMessage").Parse(templateStr)
	if err != nil {
		return fmt.Sprintf("Error parsing execution-only user message template: %v", err)
	}

	var result strings.Builder
	if err := tmpl.Execute(&result, templateData); err != nil {
		return fmt.Sprintf("Error executing execution-only user message template: %v", err)
	}

	return result.String()
}
