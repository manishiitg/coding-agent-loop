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
	ValidationFeedback      string
	LearningAgentOutput     string
	PreviousHumanFeedback   string
	StepSuccessPatterns     string // NEW - success patterns from previous executions
	StepFailurePatterns     string // NEW - failure patterns from previous executions
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
		"ValidationFeedback":      templateVars["ValidationFeedback"],
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
		ValidationFeedback:      executionTemplateVars["ValidationFeedback"],
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
		ValidationFeedback:      templateVars["ValidationFeedback"],
		LearningAgentOutput:     templateVars["LearningAgentOutput"],
		PreviousHumanFeedback:   templateVars["PreviousHumanFeedback"],
		StepSuccessPatterns:     templateVars["StepSuccessPatterns"],
		StepFailurePatterns:     templateVars["StepFailurePatterns"],
	}

	// 	## 📁 FILE PERMISSIONS
	// **READ:**
	// - {{.WorkspacePath}}/todo_creation_human/planning/plan.md (current plan)

	// Define the template
	templateStr := `## 🎯 PRIMARY TASK - EXECUTE SINGLE STEP

**CURRENT STEP**: {{.StepNumber}}/{{.TotalSteps}} - {{.StepTitle}}
**STEP DESCRIPTION**: {{.StepDescription}}
**WORKSPACE**: {{.WorkspacePath}}

## 🤖 AGENT IDENTITY
- **Role**: Execution Agent
- **Responsibility**: Execute a single step from the plan using MCP tools
- **Mode**: Single step execution (step {{.StepNumber}} of {{.TotalSteps}})

**FILE PERMISSIONS:**
- **READ ONLY**: planning/plan.md, context files from previous steps

**RESTRICTIONS:**
- Focus on executing the task using MCP tools
- Read workspace files for context as needed
- Create context output file if specified in step
- Return execution results in your response (no file writing)
- No documentation or report writing (validation agent handles that)

{{if .ValidationFeedback}}
## 🔄 RETRY WITH VALIDATION FEEDBACK

**Previous attempt failed. Address these issues:**
{{.ValidationFeedback}}

**Focus on:** 
- Fix specific issues mentioned above
- Improve tool selection and usage
- Ensure success criteria is met this time

{{end}}

{{if .LearningAgentOutput}}
## 🧠 LEARNING AGENT OUTPUT

**Learning Agent Analysis**: {{.LearningAgentOutput}}

**Important**: The learning agent has analyzed the previous execution and provided this refined guidance. Use this analysis to improve your execution approach.
{{end}}

{{if .StepSuccessPatterns}}
## ✅ SUCCESS PATTERNS FROM PREVIOUS EXECUTIONS

**What Worked Well Before:**
{{.StepSuccessPatterns}}

**Important**: These patterns show what worked in previous executions. Consider using these approaches and tools for this step.
{{end}}

{{if .StepFailurePatterns}}
## ❌ FAILURE PATTERNS FROM PREVIOUS EXECUTIONS

**What Failed Before:**
{{.StepFailurePatterns}}

**Important**: These patterns show what failed in previous executions. Avoid these approaches and tools for this step.
{{end}}

{{if .PreviousHumanFeedback}}
## 👥 PREVIOUS HUMAN FEEDBACK

**Human Guidance from Previous Steps:**
{{.PreviousHumanFeedback}}

**Important**: Use this feedback to improve your execution approach and avoid repeating previous mistakes. Consider the human's suggestions when selecting tools and executing this step.
{{end}}

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

1. **Read Context**: Check context dependencies for files from previous steps
2. **Use Success Patterns**: If success patterns are provided, consider using those approaches and tools
3. **Avoid Failure Patterns**: If failure patterns are provided, avoid those approaches and tools
4. **Use MCP Tools**: Select appropriate tools to accomplish the step objective
5. **Verify Completion**: Check if success criteria is met
6. **Create Output**: Generate context output file for next steps (if specified)
7. **Document Results**: Provide clear summary of what was accomplished

` + GetTodoCreationHumanMemoryRequirements() + `

## 📤 Output Format

Provide a clear execution summary in your response:

---

**Step {{.StepNumber}}/{{.TotalSteps}} Execution Summary**

**Status**: [COMPLETED/FAILED/IN_PROGRESS]

**Actions Taken**:
- Used [MCP Server].[Tool] with [arguments]
- Result: [what happened]
- Created/modified: [any files]

**Success Criteria Check**: 
- Criteria: {{.StepSuccessCriteria}}
- Met: [Yes/No with evidence]

**Context Output**: 
- [Path to context file created, if applicable]

---

**Note**: Focus on executing step {{.StepNumber}} completely using MCP tools. Read workspace files for context. Return results in your response. The validation agent will document and verify your execution.`

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
