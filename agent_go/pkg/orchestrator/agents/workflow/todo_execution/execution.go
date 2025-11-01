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

// todoExecutionInputProcessor processes inputs specifically for single step execution
func (tea *TodoExecutionAgent) todoExecutionInputProcessor(templateVars map[string]string) string {

	// Define the template for single step execution
	templateStr := `## PRIMARY TASK - EXECUTE SINGLE STEP

**STEP**: {{.StepNumber}}/{{.TotalSteps}}
**TITLE**: {{.StepTitle}}
**OBJECTIVE**: {{.StepDescription}}

## STEP DETAILS

**Why This Step:**
{{.StepWhyThisStep}}

**Success Criteria:**
{{.StepSuccessCriteria}}

**Context Dependencies:**
{{.StepContextDependencies}}

**Context Output to Produce:**
{{.StepContextOutput}}

## PROVEN APPROACHES (Follow These)

**Success Patterns (What Worked):**
{{.StepSuccessPatterns}}

**Failure Patterns (Avoid These):**
{{.StepFailurePatterns}}

{{if .PreviousFeedback}}
## 🔄 PREVIOUS FEEDBACK
**Previous Validation Feedback**: {{.PreviousFeedback}}

**IMPORTANT**: Use this feedback to improve your execution. Address any issues mentioned and follow the recommendations provided.
{{end}}

## 🤖 AGENT IDENTITY
- **Role**: Todo Execution Agent

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
5. **Produce Context Output**: Ensure this step produces what subsequent steps need
6. **Verify Success Criteria**: Confirm all criteria are met before completion

` + GetTodoExecutionMemoryRequirements() + `

**IMPORTANT**: 
- The workspace path has been pre-configured to use the correct run folder
- Focus on executing the step using MCP tools
- You don't need to create summary or documentation files

Focus on executing this step effectively using proven approaches and avoiding failed patterns.`

	// Parse and execute the template
	tmpl, err := template.New("todoExecution").Parse(templateStr)
	if err != nil {
		return fmt.Sprintf("Error parsing template: %w", err)
	}

	var result strings.Builder
	err = tmpl.Execute(&result, map[string]interface{}{
		"StepNumber":              templateVars["StepNumber"],
		"TotalSteps":              templateVars["TotalSteps"],
		"StepTitle":               templateVars["StepTitle"],
		"StepDescription":         templateVars["StepDescription"],
		"StepWhyThisStep":         templateVars["StepWhyThisStep"],
		"StepSuccessCriteria":     templateVars["StepSuccessCriteria"],
		"StepContextDependencies": templateVars["StepContextDependencies"],
		"StepContextOutput":       templateVars["StepContextOutput"],
		"StepSuccessPatterns":     templateVars["StepSuccessPatterns"],
		"StepFailurePatterns":     templateVars["StepFailurePatterns"],
		"PreviousFeedback":        templateVars["PreviousFeedback"],
		"WorkspacePath":           templateVars["WorkspacePath"],
		"RunOption":               templateVars["RunOption"],
	})
	if err != nil {
		return fmt.Sprintf("Error executing template: %w", err)
	}

	return result.String()
}

// Execute processes the todo execution request using the input processor
func (tea *TodoExecutionAgent) Execute(ctx context.Context, templateVars map[string]string, conversationHistory []llmtypes.MessageContent) (string, []llmtypes.MessageContent, error) {
	// Use the base orchestrator agent's Execute method with our custom input processor
	return tea.BaseOrchestratorAgent.ExecuteWithInputProcessor(ctx, templateVars, tea.todoExecutionInputProcessor, conversationHistory)
}
