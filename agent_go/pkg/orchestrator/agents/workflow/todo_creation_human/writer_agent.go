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
- planning/plan.json (plan)
- validation/step_*_validation_report.md (all step validation reports with execution summaries)
- learnings/success_patterns.md (success learning insights)
- learnings/failure_analysis.md (failure patterns to avoid)
- learnings/step_*_learning.md (per-step learning details)

**WRITE:**
- {{.WorkspacePath}}/todo_final.md (final todo list - outside todo_creation_human/)

**RESTRICTIONS:**
- Read from planning/, validation/, learnings/ folders
- Validation reports contain execution summaries from execution agent
- Write todo_final.md to workspace root
- Keep todo_final.md concise and actionable

## 📋 SYNTHESIS GUIDELINES
- **Read Workspace Files**: Review plan.json, validation reports (which include execution summaries), and learnings/ folder
- **Review Learnings**: Read learnings/success_patterns.md for what worked and learnings/failure_analysis.md for what to avoid
- **Get Execution Details**: Validation reports contain execution conversation and tool usage details
- **Prioritize by Success**: High priority for steps with strong success patterns, medium for refinements, low for optional improvements
- **Be Specific**: Include exact MCP server, tool, and arguments from success patterns
- **Handle Missing Data**: If learnings files missing, use validation reports only

**Prioritization Logic**:
- **HIGH**: Steps validated as successful with strong evidence
- **MEDIUM**: Steps needing minor adjustments based on validation feedback
- **LOW**: Optional improvements or nice-to-have enhancements

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
**What Worked**: [Successful approaches from execution and learning reports]
**What Didn't Work**: [Failed approaches to avoid based on learning reports]
**Key Insights**: [Important discoveries from learning analysis for todo list creation]
**Learning-Based Improvements**: [Specific improvements based on learning reports]

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
- **Proven Methods**: [MCP tools/approaches that worked well based on learning reports]
- **Avoid These**: [Approaches that failed or didn't work based on learning analysis]
- **Critical Steps**: [Steps that are essential for success based on learnings]
- **Learning-Based Recommendations**: [Specific recommendations from learning reports]

### Next Steps
- [What should be done first]
- [Any follow-up actions needed]
- [Human review recommendations]

---

**Note**: Focus on creating actionable todo items based on execution results AND learning reports. Each item should be concrete and executable with clear success criteria, incorporating insights from the learning analysis.`

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
