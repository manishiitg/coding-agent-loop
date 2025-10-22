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
	StepNumber              string
	TotalSteps              string
	StepTitle               string
	StepDescription         string
	StepSuccessCriteria     string
	StepWhyThisStep         string
	StepContextDependencies string
	StepContextOutput       string
	WorkspacePath           string
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
func (hctpea *HumanControlledTodoPlannerExecutionAgent) Execute(ctx context.Context, templateVars map[string]string, conversationHistory []llms.MessageContent) (string, []llms.MessageContent, error) {
	// Extract workspace path from template variables
	// Human-controlled execution agent - executes plan directly without iteration complexity
	workspacePath := templateVars["WorkspacePath"]

	// Prepare template variables
	executionTemplateVars := map[string]string{
		"StepNumber":              templateVars["StepNumber"],
		"TotalSteps":              templateVars["TotalSteps"],
		"StepTitle":               templateVars["StepTitle"],
		"StepDescription":         templateVars["StepDescription"],
		"StepSuccessCriteria":     templateVars["StepSuccessCriteria"],
		"StepWhyThisStep":         templateVars["StepWhyThisStep"],
		"StepContextDependencies": templateVars["StepContextDependencies"],
		"StepContextOutput":       templateVars["StepContextOutput"],
		"WorkspacePath":           workspacePath,
	}

	// Create template data for validation
	templateData := HumanControlledTodoPlannerExecutionTemplate{
		StepNumber:              executionTemplateVars["StepNumber"],
		TotalSteps:              executionTemplateVars["TotalSteps"],
		StepTitle:               executionTemplateVars["StepTitle"],
		StepDescription:         executionTemplateVars["StepDescription"],
		StepSuccessCriteria:     executionTemplateVars["StepSuccessCriteria"],
		StepWhyThisStep:         executionTemplateVars["StepWhyThisStep"],
		StepContextDependencies: executionTemplateVars["StepContextDependencies"],
		StepContextOutput:       executionTemplateVars["StepContextOutput"],
		WorkspacePath:           executionTemplateVars["WorkspacePath"],
	}

	// Execute using template validation
	return hctpea.ExecuteWithTemplateValidation(ctx, executionTemplateVars, hctpea.humanControlledExecutionInputProcessor, conversationHistory, templateData)
}

// humanControlledExecutionInputProcessor processes inputs specifically for human-controlled plan execution
func (hctpea *HumanControlledTodoPlannerExecutionAgent) humanControlledExecutionInputProcessor(templateVars map[string]string) string {
	// Create template data
	templateData := HumanControlledTodoPlannerExecutionTemplate{
		StepNumber:              templateVars["StepNumber"],
		TotalSteps:              templateVars["TotalSteps"],
		StepTitle:               templateVars["StepTitle"],
		StepDescription:         templateVars["StepDescription"],
		StepSuccessCriteria:     templateVars["StepSuccessCriteria"],
		StepWhyThisStep:         templateVars["StepWhyThisStep"],
		StepContextDependencies: templateVars["StepContextDependencies"],
		StepContextOutput:       templateVars["StepContextOutput"],
		WorkspacePath:           templateVars["WorkspacePath"],
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
- **CREATE** {{.WorkspacePath}}/execution/step_{{.StepNumber}}_execution_results.md
- **CREATE** {{.WorkspacePath}}/execution/completed_steps.md
- **CREATE/UPDATE** any files needed to complete step {{.StepNumber}}

**RESTRICTIONS:**
- Only modify files within {{.WorkspacePath}}/
- Single execution - no iteration complexity
- Focus on execution, not evidence creation

## 🎯 CURRENT STEP EXECUTION

**Step {{.StepNumber}}/{{.TotalSteps}} - {{.StepTitle}}**
**Description**: {{.StepDescription}}

### 📋 Complete Step Information
**Success Criteria**: {{.StepSuccessCriteria}}
**Why This Step**: {{.StepWhyThisStep}}
**Context Dependencies**: {{.StepContextDependencies}}
**Context Output**: {{.StepContextOutput}}

### 🔍 Step Context Analysis
**Success Criteria**: Use the success criteria above to verify completion
**Why This Step**: The why this step field explains how this contributes to the objective
**Context Dependencies**: Check context dependencies for files from previous steps
**Context Output**: Create the context output file specified above for other agents

**Your Task**: Execute this specific step using the available MCP tools. Use the complete step information above, including success criteria, context dependencies, and context output requirements.

## 🔍 EXECUTION GUIDELINES

1. **Understand Current Step**: You are executing step {{.StepNumber}}/{{.TotalSteps}} - "{{.StepTitle}}"
2. **Step Description**: {{.StepDescription}}
3. **Success Criteria**: Verify completion using the success criteria above
4. **Context Dependencies**: Read any files specified in context dependencies from previous steps
5. **Context Output**: Create the context output file specified above for other agents
6. **Execute Step**: Use MCP tools to complete this specific step
7. **Document Results**: Record execution results and any evidence
8. **Single-Go Execution**: Complete the entire step in one execution

` + GetTodoCreationHumanMemoryRequirements() + `

## 📤 Output Format

**CREATE** {{.WorkspacePath}}/execution/step_{{.StepNumber}}_execution_results.md

---

## Step {{.StepNumber}} Execution Results
**Step**: {{.StepNumber}}/{{.TotalSteps}}
**Status**: [COMPLETED/FAILED/IN_PROGRESS]

### Actions Taken
- [Brief summary of what was accomplished]

### Key Results
- [Main outcome and evidence that success criteria was met]

### Files Modified
- [List key files created/updated with brief descriptions]

---

**Note**: Execute step {{.StepNumber}} of {{.TotalSteps}} completely. Read the plan from the workspace file to understand what needs to be done. Focus on completing the step fully in a single execution. **IMPORTANT**: Clearly list all files you create or update so the validation agent can verify your work. Create step-specific execution results file: step_{{.StepNumber}}_execution_results.md. Focus on execution quality and tool usage effectiveness.`

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
