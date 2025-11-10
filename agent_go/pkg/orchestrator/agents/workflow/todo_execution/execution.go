package todo_execution

import (
	"context"
	"fmt"
	"strings"
	"text/template"

	"mcp-agent/agent_go/internal/llmtypes"
	"mcp-agent/agent_go/internal/observability"
	"mcp-agent/agent_go/internal/utils"
	"mcp-agent/agent_go/pkg/mcpagent"
	"mcp-agent/agent_go/pkg/orchestrator/agents"
)

// TodoExecutionTemplate holds template variables for todo execution prompts
type TodoExecutionTemplate struct {
	Objective     string // The workflow objective
	WorkspacePath string // The workspace path extracted from objective
	RunOption     string // Selected run option: use_same_run, create_new_runs_always, create_new_run_once_daily
}

// TodoExecutionAgent extends BaseOrchestratorAgent with todo execution functionality
type TodoExecutionAgent struct {
	*agents.BaseOrchestratorAgent // ✅ REUSE: All base functionality
}

// NewTodoExecutionAgent creates a new todo execution agent
func NewTodoExecutionAgent(config *agents.OrchestratorAgentConfig, logger utils.ExtendedLogger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) *TodoExecutionAgent {
	baseAgent := agents.NewBaseOrchestratorAgentWithEventBridge(
		config,
		logger,
		tracer,
		agents.TodoExecutionAgentType, // 🆕 NEW: Agent type
		eventBridge,
	)

	return &TodoExecutionAgent{
		BaseOrchestratorAgent: baseAgent, // ✅ REUSE: All base functionality
	}
}

// executionSystemPromptProcessor generates the system prompt for execution agent
func (tea *TodoExecutionAgent) executionSystemPromptProcessor(templateVars map[string]string) string {
	workspacePath := templateVars["WorkspacePath"]
	hasLoop := templateVars["HasLoop"] == "true"
	loopCondition := templateVars["LoopCondition"]
	stepContextOutput := templateVars["StepContextOutput"]

	// Define the system prompt template
	templateStr := `# Execution Agent

## 🤖 AGENT IDENTITY
- **Role**: Todo Execution Agent
- **Responsibility**: Execute a single step from the plan using MCP tools
- **Mode**: Single step execution

## 📁 FILE PERMISSIONS

**WRITE:**
- {{.WorkspacePath}}/outputs/* (any files created during execution, if needed)

**EXECUTION FOCUS:**
- Execute the step using MCP tools
- Create files in outputs/ only if required by the step
- No need to create summary or documentation files
- The orchestrator will capture your execution results

## EXECUTION STRATEGY
1. **Check Context Dependencies**: Ensure prerequisites are satisfied before starting
2. **Follow Success Patterns Exactly**: These are validated approaches that worked before
3. **Avoid All Failure Patterns**: These approaches have failed and should not be used
4. **Execute the Step**: Use proven tools and approaches from Success Patterns
{{if .HasLoop}}
5. **Work Towards Loop Condition**: Focus on making progress towards "{{.LoopCondition}}"
6. **Save Progress After Each Iteration**: Update/append to context output file ({{.StepContextOutput}}) after each iteration
{{else}}
5. **Produce Context Output**: Ensure this step produces what subsequent steps need
6. **Verify Success Criteria**: Confirm all criteria are met before completion
{{end}}

` + GetTodoExecutionMemoryRequirements() + `

**IMPORTANT**: 
- The workspace path has been pre-configured to use the correct run folder
- Focus on executing the step using MCP tools
- You don't need to create summary or documentation files
{{if .HasLoop}}
- **CRITICAL**: Save progress after EACH iteration - don't wait until the loop completes
{{end}}

Focus on executing this step effectively using proven approaches and avoiding failed patterns.`

	// Parse and execute the template
	tmpl, err := template.New("executionSystemPrompt").Parse(templateStr)
	if err != nil {
		return fmt.Sprintf("Error parsing execution system prompt template: %v", err)
	}

	var result strings.Builder
	err = tmpl.Execute(&result, map[string]interface{}{
		"WorkspacePath":     workspacePath,
		"HasLoop":           hasLoop,
		"LoopCondition":     loopCondition,
		"StepContextOutput": stepContextOutput,
	})
	if err != nil {
		return fmt.Sprintf("Error executing execution system prompt template: %v", err)
	}

	return result.String()
}

// executionUserMessageProcessor generates the user message for execution agent
func (tea *TodoExecutionAgent) executionUserMessageProcessor(templateVars map[string]string) string {
	// Check if this is a loop step
	hasLoop := templateVars["HasLoop"] == "true"
	loopCondition := templateVars["LoopCondition"]
	loopDescription := templateVars["LoopDescription"]
	currentIteration := templateVars["CurrentIteration"]
	maxIterations := templateVars["MaxIterations"]
	previousIterationOutput := templateVars["PreviousIterationOutput"]

	// Define the user message template
	templateStr := `## 🎯 PRIMARY TASK - EXECUTE SINGLE STEP

**STEP**: {{.StepNumber}}/{{.TotalSteps}}
**TITLE**: {{.StepTitle}}
**OBJECTIVE**: {{.StepDescription}}

{{if .VariableNames}}
## 📋 AVAILABLE VARIABLES

**Variable Names and Descriptions:**
{{.VariableNames}}

{{if .VariableValues}}
**Variable Values (for reference):**
{{.VariableValues}}
{{end}}

**Important**: Variables have been resolved in step descriptions above. Use these variable names/values as reference when executing the step.
{{end}}

## STEP DETAILS

**Success Criteria:**
{{.StepSuccessCriteria}}

**Context Dependencies:**
{{.StepContextDependencies}}

**Context Output to Produce:**
{{.StepContextOutput}}

{{if .HasLoop}}
## 🔄 LOOP MODE ACTIVE

**This step is executing in LOOP MODE** - you will execute this step repeatedly until the loop condition is met.

**Loop Condition**: {{.LoopCondition}}
{{if .LoopDescription}}
**Loop Description**: {{.LoopDescription}}
{{end}}

**Current Status**:
- **Current Iteration**: {{.CurrentIteration}} / {{.MaxIterations}}
- **Max Iterations**: {{.MaxIterations}}

**Your Task in Loop Mode**:
- Execute the step as described below
- Work towards meeting the loop condition: "{{.LoopCondition}}"
- The step will continue looping until this condition is met OR max iterations reached
- After each execution, the validation agent will check if the loop condition is met
- **Focus on making progress towards the loop condition** - you may need to check status, poll services, retry operations, etc.
- **CRITICAL**: Save progress after EACH iteration by updating/appending to the context output file ({{.StepContextOutput}}) - don't wait until the loop completes. Each iteration's progress must be preserved so the next iteration can see what was accomplished.

**Important**: 
- The loop condition ({{.LoopCondition}}) is the same as the success criteria
- Once the loop condition is met, the step will exit the loop and be marked as completed
- Continue executing until the condition is satisfied
{{end}}

## PROVEN APPROACHES (Follow These)

**Success Patterns (What Worked):**
{{.StepSuccessPatterns}}

**Failure Patterns (Avoid These):**
{{.StepFailurePatterns}}

{{if .PreviousFeedback}}
## ⚠️ PREVIOUS FEEDBACK
**Previous Validation Feedback**: {{.PreviousFeedback}}

**IMPORTANT**: Use this feedback to improve your execution. Address any issues mentioned and follow the recommendations provided.
{{end}}

{{if .PreviousIterationOutput}}
## 🔄 PREVIOUS LOOP ITERATION EXECUTION OUTPUT

{{.PreviousIterationOutput}}

**Important**: This is the execution output from the previous loop iteration. Review what was done previously to understand the context and avoid repeating the same actions unnecessarily.
{{end}}`

	// Parse and execute the template
	tmpl, err := template.New("executionUserMessage").Parse(templateStr)
	if err != nil {
		return fmt.Sprintf("Error parsing execution user message template: %v", err)
	}

	var result strings.Builder
	err = tmpl.Execute(&result, map[string]interface{}{
		"StepNumber":              templateVars["StepNumber"],
		"TotalSteps":              templateVars["TotalSteps"],
		"StepTitle":               templateVars["StepTitle"],
		"StepDescription":         templateVars["StepDescription"],
		"StepSuccessCriteria":     templateVars["StepSuccessCriteria"],
		"StepContextDependencies": templateVars["StepContextDependencies"],
		"StepContextOutput":       templateVars["StepContextOutput"],
		"StepSuccessPatterns":     templateVars["StepSuccessPatterns"],
		"StepFailurePatterns":     templateVars["StepFailurePatterns"],
		"PreviousFeedback":        templateVars["PreviousFeedback"],
		"HasLoop":                 hasLoop,
		"LoopCondition":           loopCondition,
		"LoopDescription":         loopDescription,
		"CurrentIteration":        currentIteration,
		"MaxIterations":           maxIterations,
		"PreviousIterationOutput": previousIterationOutput,
		"VariableNames":           templateVars["VariableNames"],
		"VariableValues":          templateVars["VariableValues"],
	})
	if err != nil {
		return fmt.Sprintf("Error executing execution user message template: %v", err)
	}

	return result.String()
}

// todoExecutionInputProcessor processes inputs specifically for single step execution
// This is kept for backward compatibility but now just returns the user message
func (tea *TodoExecutionAgent) todoExecutionInputProcessor(templateVars map[string]string) string {
	return tea.executionUserMessageProcessor(templateVars)
}

// Execute processes the todo execution request using separate system prompt and user message
func (tea *TodoExecutionAgent) Execute(ctx context.Context, templateVars map[string]string, conversationHistory []llmtypes.MessageContent) (string, []llmtypes.MessageContent, error) {
	// Generate system prompt and user message separately
	systemPrompt := tea.executionSystemPromptProcessor(templateVars)
	userMessage := tea.executionUserMessageProcessor(templateVars)

	// Create a simple input processor that returns the user message
	inputProcessor := func(map[string]string) string {
		return userMessage
	}

	// Use ExecuteWithTemplateValidation with system prompt (overwrite=true to replace default MCP prompt with agent-specific prompt)
	return tea.BaseOrchestratorAgent.ExecuteWithTemplateValidation(ctx, templateVars, inputProcessor, conversationHistory, nil, systemPrompt, true)
}
