package todo_creation_human

import (
	"context"
	"fmt"
	"strings"
	"text/template"

	"mcp-agent/agent_go/internal/observability"
	"mcp-agent/agent_go/internal/utils"
	"mcp-agent/agent_go/pkg/mcpagent"
	"mcp-agent/agent_go/pkg/orchestrator/agents"

	"github.com/tmc/langchaingo/llms"
)

// HumanControlledTodoPlannerExecutionTemplate holds template variables for human-controlled execution prompts
type HumanControlledTodoPlannerExecutionTemplate struct {
	StepNumber      string
	TotalSteps      string
	StepTitle       string
	StepDescription string
	WorkspacePath   string
}

// HumanControlledTodoPlannerExecutionAgent executes the objective using MCP servers in human-controlled mode
type HumanControlledTodoPlannerExecutionAgent struct {
	*agents.BaseOrchestratorAgent
}

// NewHumanControlledTodoPlannerExecutionAgent creates a new human-controlled todo planner execution agent
func NewHumanControlledTodoPlannerExecutionAgent(config *agents.OrchestratorAgentConfig, logger utils.ExtendedLogger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) *HumanControlledTodoPlannerExecutionAgent {
	baseAgent := agents.NewBaseOrchestratorAgentWithEventBridge(
		config,
		logger,
		tracer,
		agents.TodoPlannerExecutionAgentType,
		eventBridge,
	)

	return &HumanControlledTodoPlannerExecutionAgent{
		BaseOrchestratorAgent: baseAgent,
	}
}

// Execute implements the OrchestratorAgent interface
func (hctpea *HumanControlledTodoPlannerExecutionAgent) Execute(ctx context.Context, templateVars map[string]string, conversationHistory []llms.MessageContent) (string, error) {
	// Extract workspace path from template variables
	// Human-controlled execution agent - executes plan directly without iteration complexity
	workspacePath := templateVars["WorkspacePath"]

	// Prepare template variables
	executionTemplateVars := map[string]string{
		"StepNumber":      templateVars["StepNumber"],
		"TotalSteps":      templateVars["TotalSteps"],
		"StepTitle":       templateVars["StepTitle"],
		"StepDescription": templateVars["StepDescription"],
		"WorkspacePath":   workspacePath,
	}

	// Execute using input processor
	return hctpea.ExecuteWithInputProcessor(ctx, executionTemplateVars, hctpea.humanControlledExecutionInputProcessor, conversationHistory)
}

// humanControlledExecutionInputProcessor processes inputs specifically for human-controlled plan execution
func (hctpea *HumanControlledTodoPlannerExecutionAgent) humanControlledExecutionInputProcessor(templateVars map[string]string) string {
	// Create template data
	templateData := HumanControlledTodoPlannerExecutionTemplate{
		StepNumber:      templateVars["StepNumber"],
		TotalSteps:      templateVars["TotalSteps"],
		StepTitle:       templateVars["StepTitle"],
		StepDescription: templateVars["StepDescription"],
		WorkspacePath:   templateVars["WorkspacePath"],
	}

	// Define the template
	templateStr := `## 🎯 PRIMARY TASK - EXECUTE SINGLE STEP

**CURRENT STEP**: {{.StepNumber}}/{{.TotalSteps}} - {{.StepTitle}}
**STEP DESCRIPTION**: {{.StepDescription}}
**WORKSPACE**: {{.WorkspacePath}}

## 🤖 AGENT IDENTITY
- **Role**: Execution Agent
- **Responsibility**: Execute a single step from the plan using MCP tools
- **Mode**: Single step execution (step {{.StepNumber}} of {{.TotalSteps}})

## 📁 FILE PERMISSIONS
**READ:**
- {{.WorkspacePath}}/todo_creation_human/planning/plan.md (current plan)
- {{.WorkspacePath}}/todo_creation_human/execution/completed_steps.md (if exists)

**WRITE:**
- **CREATE** {{.WorkspacePath}}/todo_creation_human/execution/step_{{.StepNumber}}_execution_results.md
- **CREATE** {{.WorkspacePath}}/todo_creation_human/execution/completed_steps.md
- **CREATE** files in {{.WorkspacePath}}/todo_creation_human/execution/evidence/ (critical steps only)
- **CREATE/UPDATE** any files needed to complete step {{.StepNumber}}

**RESTRICTIONS:**
- Only modify files within {{.WorkspacePath}}/todo_creation_human/
- Single execution - no iteration complexity
- Evidence ONLY for: quantitative claims, assumptions, failures, decisions

## 🎯 CURRENT STEP EXECUTION

**Step {{.StepNumber}}/{{.TotalSteps}} - {{.StepTitle}}**
**Description**: {{.StepDescription}}

**Your Task**: Execute this specific step using the available MCP tools. The step details are provided above, but you can also read the plan from {{.WorkspacePath}}/todo_creation_human/planning/plan.md for additional context.

## 🔍 EXECUTION GUIDELINES

1. **Understand Current Step**: You are executing step {{.StepNumber}}/{{.TotalSteps}} - "{{.StepTitle}}"
2. **Step Description**: {{.StepDescription}}
3. **Execute Step**: Use MCP tools to complete this specific step
4. **Document Results**: Record execution results and any evidence
5. **Single-Go Execution**: Complete the entire step in one execution

` + GetTodoCreationHumanMemoryRequirements() + `

## 📤 Output Format

**CREATE** {{.WorkspacePath}}/todo_creation_human/execution/step_{{.StepNumber}}_execution_results.md

---

## Step {{.StepNumber}} Execution Results
**Step**: {{.StepNumber}}/{{.TotalSteps}}
**Status**: [COMPLETED/FAILED/IN_PROGRESS]

### Actions Taken
- [List the specific actions you took to complete this step]

### Results
- [Describe the outcome of executing this step]

### Files Created/Updated
**Important Files Modified:**
- [List specific files created or updated during this step]
- [Include file paths and brief description of what was done to each file]

### Evidence Files
- [List any evidence files created, if applicable]

### Next Steps
- [Any follow-up actions needed for this step]

---

**Note**: Execute step {{.StepNumber}} of {{.TotalSteps}} completely. Read the plan from the workspace file to understand what needs to be done. Focus on completing the step fully in a single execution. **IMPORTANT**: Clearly list all files you create or update so the validation agent can verify your work. Create step-specific execution results file: step_{{.StepNumber}}_execution_results.md`

	// Parse and execute the template
	tmpl, err := template.New("execution").Parse(templateStr)
	if err != nil {
		return fmt.Sprintf("Error parsing execution template: %v", err)
	}

	var result strings.Builder
	if err := tmpl.Execute(&result, templateData); err != nil {
		return fmt.Sprintf("Error executing execution template: %v", err)
	}

	return result.String()
}
