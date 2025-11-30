package todo_creation_human

import (
	"context"
	"fmt"
	"strings"
	"text/template"
	"time"

	"mcp-agent/agent_go/internal/utils"
	"mcp-agent/agent_go/pkg/orchestrator/agents"
	mcpagent "mcpagent/agent"
	"mcpagent/agent/prompt"
	"mcpagent/observability"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

// HumanControlledTodoPlannerExecutionOnlyTemplate holds template variables for execution-only agent prompts
type HumanControlledTodoPlannerExecutionOnlyTemplate struct {
	StepTitle               string
	StepDescription         string
	StepSuccessCriteria     string
	StepContextDependencies string
	StepContextOutput       string
	WorkspacePath           string
	IsCodeExecutionMode     string // "true" or "false" - indicates if code execution mode is enabled
	ValidationFeedback      string
	PreviousIterationOutput string // Previous loop iteration execution output (for loop steps)
	VariableNames           string // Variable names with descriptions ({{VAR_NAME}} - description)
	VariableValues          string // Variable names with actual values ({{VAR_NAME}} = value - description)
	HasLoop                 string // "true" or "false" as string
	LoopCondition           string // Loop condition description (required when HasLoop="true")
	LoopDescription         string // Human-readable explanation of the loop (optional)
	CurrentIteration        string // Current iteration number
	MaxIterations           string // Max iterations allowed
	LearningHistory         string // Formatted learning conversation history (REQUIRED for execution-only mode)
}

// HumanControlledTodoPlannerExecutionOnlyAgent executes steps using pre-discovered learning context
// This agent does NOT discover learnings - it receives learning history from LearningReadingAgent
type HumanControlledTodoPlannerExecutionOnlyAgent struct {
	*agents.BaseOrchestratorAgent
}

// NewHumanControlledTodoPlannerExecutionOnlyAgent creates a new execution-only agent
func NewHumanControlledTodoPlannerExecutionOnlyAgent(config *agents.OrchestratorAgentConfig, logger utils.ExtendedLogger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) *HumanControlledTodoPlannerExecutionOnlyAgent {
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

	// Define the system prompt template
	templateStr := `# Execution-Only Agent

## 📅 Current Session
**Date**: {{.CurrentDate}} | **Time**: {{.CurrentTime}}

## 🤖 Agent Identity
- **Role**: Execution-Only Agent  
- **Responsibility**: Execute a single plan step using MCP tools or Go code  
- **Mode**: Pre-discovered learning context available (read-only)
{{if .IsCodeExecutionMode}}

## ⚡ Code Execution Mode Active
{{.CodeExecutionInstructions}}
{{end}}
{{if .VariableNames}}

## 🔑 Available Variables
{{.VariableNames}}
{{if .VariableValues}}

**Current Values**: {{.VariableValues}}
{{end}}

**Variable Handling**:
- **Step descriptions already have variables resolved** - you'll see actual values in StepDescription, StepSuccessCriteria, etc.
- **For new tool calls or code**: Use actual values directly from the resolved step description{{if .IsCodeExecutionMode}}
- **For Go code**: Pass values as CLI arguments via write_code 'args' parameter, access via os.Args[1], os.Args[2], etc.{{end}}
- **Don't hardcode values** - reference them from the step context
{{end}}

## 📚 Learning Context (Pre-Discovered)
{{.LearningHistory}}

**Note**: Learning files have been read. Use insights above as **guidance**, not strict rules.
Many times learnings might get outdated, and you need to relearn again and solve the issues you face.

## 📁 File Permissions
**READ**: Context files ({{.WorkspacePath}}/step_X_results.md), workspace files (relative paths only)  
**WRITE**: Only context output files in {{.WorkspacePath}}/ (no validation reports, no files outside workspace)

## 🎯 Execution Approach

**Simple 3-Step Process:**

1. **Read Step Requirements** (PRIMARY SOURCE OF TRUTH)
   - Step description, success criteria, context dependencies
   - These requirements are already resolved with actual variable values

2. **Review Learning Context** (GUIDANCE ONLY)
   - Success patterns from above learning section
   - Failure patterns to avoid
   - {{if .IsCodeExecutionMode}}Code{{else}}Script{{end}} examples to adapt
   - **Important**: Learnings are guidance, not strict rules. The step description is PRIMARY.

3. **Execute & Document**
   - Read context dependencies from {{.WorkspacePath}}
   - Execute step using MCP tools{{if .IsCodeExecutionMode}} or Go code{{end}}
   - Adapt learning patterns to match current step requirements
   - Verify success criteria met (collect evidence)
   - Create context output file{{if .HasLoop}} (update/append after each iteration){{end}}

{{if .IsCodeExecutionMode}}## 💻 Code Execution Rules
- **Variables**: Pass via write_code 'args' parameter (e.g., args=["value1", "value2"])  
- **Access**: Read from os.Args[1], os.Args[2], etc. (os.Args[0] is program name)  
- **NO Hardcoding**: Never hardcode variable values in Go code  
- **Packages**: Import generated tool packages (aws_tools, workspace_tools, etc.)  
- **File Ops**: Always use workspace_tools for file operations
{{end}}

## 📤 Output Format
**Status**: [COMPLETED/FAILED/IN_PROGRESS]  
**Actions**: Tools used + quantitative results  
**Evidence**: Specific outputs proving completion (e.g., "grep found 15 matches")  
**Context Output**: File path if created

Validation agent will verify your work - focus on execution and evidence.`

	// Parse and execute the template
	tmpl, err := template.New("executionOnlySystemPrompt").Parse(templateStr)
	if err != nil {
		return fmt.Sprintf("Error parsing execution-only system prompt template: %v", err)
	}

	var result strings.Builder
	err = tmpl.Execute(&result, map[string]interface{}{
		"WorkspacePath":             workspacePath,
		"IsCodeExecutionMode":       isCodeExecutionMode,
		"CodeExecutionInstructions": codeExecutionInstructions,
		"HasLoop":                   hasLoop,
		"StepContextOutput":         stepContextOutput,
		"CurrentDate":               currentDate,
		"CurrentTime":               currentTime,
		"LearningHistory":           learningHistory,
		"VariableNames":             variableNames,
		"VariableValues":            variableValues,
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
		StepSuccessCriteria:     templateVars["StepSuccessCriteria"],
		StepContextDependencies: templateVars["StepContextDependencies"],
		StepContextOutput:       templateVars["StepContextOutput"],
		WorkspacePath:           templateVars["WorkspacePath"],
		IsCodeExecutionMode:     templateVars["IsCodeExecutionMode"],
		ValidationFeedback:      templateVars["ValidationFeedback"],
		PreviousIterationOutput: templateVars["PreviousIterationOutput"],
		VariableNames:           templateVars["VariableNames"],
		VariableValues:          templateVars["VariableValues"],
		HasLoop:                 templateVars["HasLoop"],
		LoopCondition:           templateVars["LoopCondition"],
		LoopDescription:         templateVars["LoopDescription"],
		CurrentIteration:        templateVars["CurrentIteration"],
		MaxIterations:           templateVars["MaxIterations"],
		LearningHistory:         templateVars["LearningHistory"],
	}

	// Define the user message template
	templateStr := `## 🎯 Execute Step: {{.StepTitle}}

**STEP DESCRIPTION**: {{.StepDescription}}  
**WORKSPACE**: {{.WorkspacePath}}

{{if .VariableNames}}## 📋 Variables
{{.VariableNames}}
{{if .VariableValues}}
**Values**: {{.VariableValues}}
{{end}}
{{if eq .IsCodeExecutionMode "true"}}
**Code Execution**: Pass variables as CLI args via write_code 'args' parameter, access via os.Args[1], os.Args[2], etc.
{{end}}{{end}}
{{if eq .HasLoop "true"}}
## 🔄 Loop Mode Active
**Loop Condition**: {{.LoopCondition}}  
{{if .LoopDescription}}**Loop Description**: {{.LoopDescription}}  
{{end}}**Iteration**: {{.CurrentIteration}} / {{.MaxIterations}}

**Task**: Execute step repeatedly until loop condition met. **Save progress after EACH iteration** to {{.WorkspacePath}}/{{.StepContextOutput}} (update/append, don't overwrite).
{{end}}
{{if .PreviousIterationOutput}}
## 🔄 Previous Iteration Output
{{.PreviousIterationOutput}}

Review what was done previously to avoid unnecessary repetition.
{{end}}
{{if .ValidationFeedback}}
## ⚠️ Validation Feedback
{{.ValidationFeedback}}

Address the issues above and improve your approach.
{{end}}

## 📋 Step Details
**Success Criteria**: {{.StepSuccessCriteria}}  
**Context Dependencies**: {{.StepContextDependencies}}  
**Context Output**: {{.StepContextOutput}}

## ✅ Execution Checklist
1. ✓ Read step requirements (description, success criteria) ← PRIMARY SOURCE
2. ✓ Review learning context (system prompt above) ← GUIDANCE
3. ✓ Read context dependencies (if any) from {{.WorkspacePath}}
4. ✓ Execute:
{{if eq .IsCodeExecutionMode "true"}}   - discover_code_files (see available packages)
   - write_code (pass vars via args, access via os.Args[1,2,...])
   - Adapt code patterns from learnings to match current step
{{else}}   - Use MCP tools to accomplish step
   - Adapt learnings to match current step
{{end}}5. ✓ Verify success criteria met (collect evidence)
6. ✓ Create context output file{{if eq .HasLoop "true"}} (update/append after each iteration){{end}}
7. ✓ Document results with quantitative evidence`

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
