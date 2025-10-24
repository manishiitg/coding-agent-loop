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

// HumanControlledStructuredPlanWriterAgent writes the approved plan to workspace files
type HumanControlledStructuredPlanWriterAgent struct {
	*agents.BaseOrchestratorAgent
}

// NewHumanControlledPlanWriterAgent creates a new human-controlled plan writer agent
func NewHumanControlledStructuredPlanWriterAgent(config *agents.OrchestratorAgentConfig, logger utils.ExtendedLogger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) *HumanControlledStructuredPlanWriterAgent {
	baseAgent := agents.NewBaseOrchestratorAgentWithEventBridge(
		config,
		logger,
		tracer,
		agents.PlanBreakdownAgentType,
		eventBridge,
	)

	return &HumanControlledStructuredPlanWriterAgent{
		BaseOrchestratorAgent: baseAgent,
	}
}

// Execute executes the plan writer agent using the standard agent pattern
func (hcpwa *HumanControlledStructuredPlanWriterAgent) Execute(ctx context.Context, templateVars map[string]string, conversationHistory []llms.MessageContent) (string, []llms.MessageContent, error) {
	// Use ExecuteWithInputProcessor to get agent events (orchestrator_agent_start/end)
	// This will automatically emit agent start/end events
	return hcpwa.ExecuteWithInputProcessor(ctx, templateVars, hcpwa.planWriterInputProcessor, conversationHistory)
}

// planWriterInputProcessor processes inputs for plan writing
func (hcpwa *HumanControlledStructuredPlanWriterAgent) planWriterInputProcessor(templateVars map[string]string) string {
	// Create template data
	templateData := map[string]string{
		"Objective":     templateVars["Objective"],
		"WorkspacePath": templateVars["WorkspacePath"],
		"PlanData":      templateVars["PlanData"], // Structured plan data from planning agent
	}

	// Define the template for plan writing
	templateStr := `## 📝 PRIMARY TASK - WRITE APPROVED PLAN TO WORKSPACE

**OBJECTIVE**: {{.Objective}}
**WORKSPACE**: {{.WorkspacePath}}

## 📁 FILE PERMISSIONS
**WRITE:**
- **CREATE** {{.WorkspacePath}}/todo_creation_human/planning/plan.json (write approved plan)

**RESTRICTIONS:**
- Only modify files within {{.WorkspacePath}}/
- Write the approved plan data to workspace files

## 📋 PLAN WRITING GUIDELINES

**Validation Steps**:
1. Verify JSON structure is valid (no syntax errors)
2. Check all required fields are present (objective_analysis, approach, steps, expected_outcome)
3. Validate relative paths format (no absolute paths like /home/user/)
4. Ensure step order is logical and dependencies are correct

**Safety Checks**:
- Backup existing plan.json if it exists (rename to plan.json.backup)
- After writing, verify new plan is readable and valid JSON
- Include timestamp in plan for tracking

## 📤 Output Format

**WRITE FILES TO WORKSPACE**

Write the following file:

1. **{{.WorkspacePath}}/todo_creation_human/planning/plan.json**
   - Create a comprehensive JSON plan file
   - Include objective analysis, approach, and all steps
   - Use proper JSON formatting with proper indentation

## 📊 APPROVED PLAN DATA
{{.PlanData}}

**Note**: Write the approved plan data to workspace files in structured JSON format. Ensure proper file organization and JSON formatting.`

	// Parse and execute the template
	tmpl, err := template.New("plan_writer").Parse(templateStr)
	if err != nil {
		return fmt.Sprintf("Error parsing plan writer template: %v", err)
	}

	var result strings.Builder
	if err := tmpl.Execute(&result, templateData); err != nil {
		return fmt.Sprintf("Error executing plan writer template: %v", err)
	}

	return result.String()
}
