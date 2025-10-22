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

// HumanControlledTodoPlannerWriterTemplate holds template variables for human-controlled todo writing prompts
type HumanControlledTodoPlannerWriterTemplate struct {
	Objective       string
	WorkspacePath   string
	TotalIterations string
}

// HumanControlledTodoPlannerWriterAgent creates optimal todo list based on execution experience in human-controlled mode
type HumanControlledTodoPlannerWriterAgent struct {
	*agents.BaseOrchestratorAgent
}

// NewHumanControlledTodoPlannerWriterAgent creates a new human-controlled todo planner writer agent
func NewHumanControlledTodoPlannerWriterAgent(config *agents.OrchestratorAgentConfig, logger utils.ExtendedLogger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) *HumanControlledTodoPlannerWriterAgent {
	baseAgent := agents.NewBaseOrchestratorAgentWithEventBridge(
		config,
		logger,
		tracer,
		agents.TodoPlannerWriterAgentType,
		eventBridge,
	)

	return &HumanControlledTodoPlannerWriterAgent{
		BaseOrchestratorAgent: baseAgent,
	}
}

// Execute implements the OrchestratorAgent interface
func (hctpwa *HumanControlledTodoPlannerWriterAgent) Execute(ctx context.Context, templateVars map[string]string, conversationHistory []llms.MessageContent) (string, []llms.MessageContent, error) {
	// Extract variables from template variables
	// Human-controlled writer - synthesizes from single execution for todo list creation
	objective := templateVars["Objective"]
	workspacePath := templateVars["WorkspacePath"]
	totalIterations := templateVars["TotalIterations"]
	if strings.TrimSpace(totalIterations) == "" {
		totalIterations = "1"
	}

	// Prepare template variables
	writerTemplateVars := map[string]string{
		"Objective":       objective,
		"WorkspacePath":   workspacePath,
		"TotalIterations": totalIterations,
	}

	// Create template data for validation
	templateData := HumanControlledTodoPlannerWriterTemplate{
		Objective:       objective,
		WorkspacePath:   workspacePath,
		TotalIterations: totalIterations,
	}

	// Execute using template validation
	return hctpwa.ExecuteWithTemplateValidation(ctx, writerTemplateVars, hctpwa.humanControlledWriterInputProcessor, conversationHistory, templateData)
}

// humanControlledWriterInputProcessor processes inputs specifically for human-controlled todo list creation
func (hctpwa *HumanControlledTodoPlannerWriterAgent) humanControlledWriterInputProcessor(templateVars map[string]string) string {
	// Create template data
	totalIterationsForTemplate := templateVars["TotalIterations"]
	if strings.TrimSpace(totalIterationsForTemplate) == "" {
		totalIterationsForTemplate = "1"
	}
	templateData := HumanControlledTodoPlannerWriterTemplate{
		Objective:       templateVars["Objective"],
		WorkspacePath:   templateVars["WorkspacePath"],
		TotalIterations: totalIterationsForTemplate,
	}

	// Define the template - simplified for direct todo list creation
	templateStr := `## 🎯 PRIMARY TASK - CREATE FINAL TODO LIST

**OBJECTIVE**: {{.Objective}}
**WORKSPACE**: {{.WorkspacePath}}

## 🤖 AGENT IDENTITY
- **Role**: Writer Agent
- **Responsibility**: Create final todo list based on execution results
- **Mode**: Direct synthesis (create actionable todo list from execution experience)

## 📁 FILE PERMISSIONS
**READ:**
- {{.WorkspacePath}}/planning/plan.md (plan)
- {{.WorkspacePath}}/execution/step_*_execution_results.md (all step execution results)
- {{.WorkspacePath}}/execution/completed_steps.md (completed work)
- {{.WorkspacePath}}/execution/evidence/ (evidence)
- {{.WorkspacePath}}/validation/step_*_validation_report.md (all step validation reports)

**WRITE:**
- **CREATE** {{.WorkspacePath}}/todo_final.md (final todo list)

**RESTRICTIONS:**
- Only modify files within {{.WorkspacePath}}/
- Single execution mode - no iteration analysis needed
- Keep todo_final.md concise and actionable

## 📋 SYNTHESIS GUIDELINES
- **Read All Step Results**: Review all step_*_execution_results.md files to understand what was executed
- **Review Validation Reports**: Check step_*_validation_report.md files to see what was validated
- **Focus on Success**: Emphasize steps that worked in execution
- **Learn from Execution**: Use execution results to create better todo items
- **Actionable Steps**: Each todo item should be concrete and executable
- **Clear Success Criteria**: Define how to verify each todo item
- **Logical Order**: Arrange todo items in logical sequence
- **MCP Tools**: Include specific MCP tools and arguments that worked

` + GetTodoCreationHumanMemoryRequirements() + `

## 📤 Output Format

**CREATE** {{.WorkspacePath}}/todo_final.md

---

## 📋 Final Todo List: {{.Objective}}
**Date**: [Current date/time]

### Objective Summary
**What we need to achieve**: {{.Objective}}
**Execution Approach**: [Brief description based on execution results]

### Execution Learnings
**What Worked**: [Successful approaches from execution]
**What Didn't Work**: [Failed approaches to avoid]
**Key Insights**: [Important discoveries for todo list creation]

### Todo Items

#### 1. [First todo item]
- **Description**: [What needs to be done - detailed and clear]
- **MCP Server**: [Server to use]
- **MCP Tool**: [Tool name]
- **Tool Arguments**: [Specific arguments that worked]
- **Success Criteria**: [How to verify completion]
- **Priority**: [HIGH/MEDIUM/LOW]
- **Dependencies**: [Any prerequisites]

#### 2. [Second todo item]
- **Description**: [What needs to be done]
- **MCP Server**: [Server to use]
- **MCP Tool**: [Tool name]
- **Tool Arguments**: [Specific arguments]
- **Success Criteria**: [How to verify completion]
- **Priority**: [HIGH/MEDIUM/LOW]
- **Dependencies**: [Any prerequisites]

#### 3. [Third todo item]
- **Description**: [What needs to be done]
- **MCP Server**: [Server to use]
- **MCP Tool**: [Tool name]
- **Tool Arguments**: [Specific arguments]
- **Success Criteria**: [How to verify completion]
- **Priority**: [HIGH/MEDIUM/LOW]
- **Dependencies**: [Any prerequisites]

### Execution Notes
- **Proven Methods**: [MCP tools/approaches that worked well]
- **Avoid These**: [Approaches that failed or didn't work]
- **Critical Steps**: [Steps that are essential for success]

### Next Steps
- [What should be done first]
- [Any follow-up actions needed]
- [Human review recommendations]

---

**Note**: Focus on creating actionable todo items based on what worked in the execution. Each item should be concrete and executable with clear success criteria.`

	// Parse and execute the template
	tmpl, err := template.New("human_controlled_writer").Parse(templateStr)
	if err != nil {
		return fmt.Sprintf("Error parsing human-controlled writer template: %v", err)
	}

	var result strings.Builder
	if err := tmpl.Execute(&result, templateData); err != nil {
		return fmt.Sprintf("Error executing human-controlled writer template: %v", err)
	}

	return result.String()
}
