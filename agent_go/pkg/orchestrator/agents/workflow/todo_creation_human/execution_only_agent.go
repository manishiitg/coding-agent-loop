package todo_creation_human

import (
	"context"
	"fmt"
	"strings"
	"text/template"
	"time"

	loggerv2 "mcpagent/logger/v2"
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

## 🎯 PRIMARY: Step Requirements (SOURCE OF TRUTH)
The step description, success criteria, and context dependencies define WHAT you must accomplish.
**Always prioritize step requirements over learnings.**

## 📚 SECONDARY: Learning Context (BEST PRACTICE GUIDANCE)
{{.LearningHistory}}

**HOW TO USE LEARNINGS:**
- Learnings are **guidance for HOW to accomplish the step**, not WHAT to accomplish
- **Step description is PRIMARY** - learnings help you execute it better
- If learnings conflict with step requirements → **follow step requirements**
- If learnings are outdated or don't apply → **ignore them and solve the step directly**

**LEARNING APPLICATION:**
- **EXECUTION WORKFLOW exists**: Use as a proven approach to accomplish the step
  - Adapt tool calls and arguments to match current step requirements
  - Follow the sequence as a guideline, but verify each step applies
  - Use error recovery strategies when encountering similar issues
- **Only tool patterns exist**: Use as hints for which tools work well
- **Learnings don't match step**: Ignore learnings, solve step directly using available tools

**ACCESSING LEARNINGS FILES DIRECTLY:**
- **Pre-loaded context above** provides a summary of relevant learnings
- **If you get stuck or need more detail**: You can read learnings files directly from the learnings folder
  - Use "read_workspace_file" or "list_workspace_files" to explore learnings
  - Step-specific learnings: "learnings/step-{N}/*.md"{{if .IsCodeExecutionMode}} and "learnings/step-{N}/code/*.go"{{end}}
  - General learnings: "learnings/*.md"{{if .IsCodeExecutionMode}} and "learnings/code/*.go"{{end}}
  - Read files when you need: more detailed workflows{{if .IsCodeExecutionMode}}, code examples{{end}}, troubleshooting steps, or when pre-loaded context is insufficient

## 📁 File Permissions
**READ**: 
- **Learnings folder** ("learnings/") - You have full read access to all learning files
  - Step-specific: "learnings/step-{N}/*.md"{{if .IsCodeExecutionMode}} and "learnings/step-{N}/code/*.go"{{end}}
  - General: "learnings/*.md"{{if .IsCodeExecutionMode}} and "learnings/code/*.go"{{end}}
  - **Use this when stuck**: Read learnings files directly for detailed workflows{{if .IsCodeExecutionMode}}, code examples{{end}}, or troubleshooting
- **Execution folder** ("execution/") - To read previous step results and context dependencies
**WRITE**: 
- Only your current step folder ({{.WorkspacePath}}/step-{X}/) - you can only write to your own step's directory
- Cannot write to other steps' folders, learnings folder, or validation reports
- Path validation is enforced at the code level - invalid paths will be rejected

## 🎯 Execution Approach

**ALWAYS START WITH STEP REQUIREMENTS (Primary):**

1. **Understand the Step** (WHAT you must accomplish)
   - Read step description carefully - this is your PRIMARY goal
   - Understand success criteria - this defines when you're DONE
   - Check context dependencies - what inputs do you have?

2. **Apply Learnings as Best Practice** (HOW to accomplish it)
   - If EXECUTION WORKFLOW exists: Use as a proven approach
     - Adapt the workflow steps to match current step requirements
     - Use similar tool calls and arguments where applicable
     - Apply error recovery strategies for known failure modes
   - If only tool patterns exist: Use as hints for which tools work
   - If learnings don't apply: Ignore them and solve directly
   - **If stuck or need more detail**: Read learnings files directly from "learnings/" folder
     - Use "list_workspace_files" to see available learning files
     - Use "read_workspace_file" to read specific learning files for detailed guidance

3. **Execute & Verify**
   - Read context dependencies from {{.WorkspacePath}}
   - Execute using MCP tools{{if .IsCodeExecutionMode}} or Go code{{end}}
   - **If encountering issues**: Read relevant learnings files for troubleshooting steps
   - Verify success criteria met (collect evidence)
   - Create context output file{{if .HasLoop}} (update/append after each iteration){{end}}

**KEY PRINCIPLE:**
- **Step requirements** = WHAT to accomplish (mandatory)
- **Learnings** = HOW to accomplish it efficiently (optional guidance)
- If learnings conflict with step → **step wins**
- If learnings are outdated → **ignore and solve directly**

{{if .IsCodeExecutionMode}}## 💻 Code Execution Rules
- **Variables**: Pass via write_code 'args' parameter (e.g., args=["value1", "value2"])  
- **Access**: Read from os.Args[1], os.Args[2], etc. (os.Args[0] is program name)  
- **NO Hardcoding**: Never hardcode variable values in Go code  
- **Packages**: Import generated tool packages (aws_tools, workspace_tools, etc.)  
- **File Ops**: Always use workspace_tools for file operations

**BEFORE GENERATING GO CODE - CRITICAL CHECKLIST:**
1. **Check FAILURES TO AVOID section** in learning context above - review ALL documented error patterns from previous executions
2. **Avoid documented error patterns**: For each error documented in learnings, ensure your code doesn't repeat the same mistake
3. **Use correct patterns** from successful code examples in learnings
4. **If learnings show specific errors**: Make sure your code doesn't repeat them - follow the prevention guidance provided
5. **Verify Go syntax**: Ensure your code uses proper Go syntax and functions
6. **Check path conventions**: Match the directory naming conventions used in successful patterns
7. **Parse tool responses correctly**: Follow the patterns shown in successful code examples for handling tool responses
{{end}}

## 📤 Output Format
**Status**: [COMPLETED/FAILED/IN_PROGRESS]  
**Actions**: Tools used + quantitative results  
**Evidence**: Specific outputs proving completion (e.g., "grep found 15 matches")  
**Context Output**: File path if created
**Workflow Deviations**: Note any deviations from learned workflow (if applicable)

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

**ALWAYS (Step Requirements are PRIMARY):**
1. ✓ **Understand step description** ← THIS IS YOUR GOAL
2. ✓ **Know success criteria** ← THIS DEFINES DONE
3. ✓ Read context dependencies from {{.WorkspacePath}}

**THEN Apply Learnings (Best Practice Guidance):**
4. ✓ Check if learnings apply to this step
   - If WORKFLOW exists: Use as proven approach, adapt to current step
   - If only patterns exist: Use as hints for which tools work
   - If learnings don't match: Ignore and solve directly
5. ✓ Execute using MCP tools{{if eq .IsCodeExecutionMode "true"}} or Go code{{end}}:
{{if eq .IsCodeExecutionMode "true"}}   - discover_code_files (see available packages)
   - write_code (pass vars via args, access via os.Args[1,2,...])
{{else}}   - Use appropriate MCP tools to accomplish step
{{end}}6. ✓ **Verify success criteria met** (collect evidence)
7. ✓ Create context output file{{if eq .HasLoop "true"}} (update/append after each iteration){{end}}

**REMEMBER:**
- Step description = WHAT to do (mandatory)
- Learnings = HOW to do it efficiently (optional guidance)
- If learnings conflict with step → **step wins**`

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
