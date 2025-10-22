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

// HumanControlledTodoPlannerPlanningTemplate holds template variables for human-controlled planning prompts
type HumanControlledTodoPlannerPlanningTemplate struct {
	Objective     string
	WorkspacePath string
}

// HumanControlledTodoPlannerPlanningAgent creates a fast, simplified plan from the objective
type HumanControlledTodoPlannerPlanningAgent struct {
	*agents.BaseOrchestratorAgent
}

// NewHumanControlledTodoPlannerPlanningAgent creates a new human-controlled todo planner planning agent
func NewHumanControlledTodoPlannerPlanningAgent(config *agents.OrchestratorAgentConfig, logger utils.ExtendedLogger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) *HumanControlledTodoPlannerPlanningAgent {
	baseAgent := agents.NewBaseOrchestratorAgentWithEventBridge(
		config,
		logger,
		tracer,
		agents.TodoPlannerPlanningAgentType, // Reuse the same type for now
		eventBridge,
	)

	return &HumanControlledTodoPlannerPlanningAgent{
		BaseOrchestratorAgent: baseAgent,
	}
}

// Execute implements the OrchestratorAgent interface
func (hctppa *HumanControlledTodoPlannerPlanningAgent) Execute(ctx context.Context, templateVars map[string]string, conversationHistory []llms.MessageContent) (string, []llms.MessageContent, error) {
	// Extract variables from template variables
	objective := templateVars["Objective"]
	workspacePath := templateVars["WorkspacePath"]

	// Prepare template variables - simplified for human-controlled mode
	planningTemplateVars := map[string]string{
		"Objective":     objective,
		"WorkspacePath": workspacePath,
	}

	// Create template data for validation
	templateData := HumanControlledTodoPlannerPlanningTemplate{
		Objective:     objective,
		WorkspacePath: workspacePath,
	}

	// Execute using template validation
	return hctppa.ExecuteWithTemplateValidation(ctx, planningTemplateVars, hctppa.humanControlledPlanningInputProcessor, conversationHistory, templateData)
}

// humanControlledPlanningInputProcessor processes inputs specifically for fast, simplified planning
func (hctppa *HumanControlledTodoPlannerPlanningAgent) humanControlledPlanningInputProcessor(templateVars map[string]string) string {
	// Create template data
	templateData := HumanControlledTodoPlannerPlanningTemplate{
		Objective:     templateVars["Objective"],
		WorkspacePath: templateVars["WorkspacePath"],
	}

	// Define the template - simplified for direct planning
	templateStr := `## 🚀 PRIMARY TASK - CREATE PLAN TO EXECUTE OBJECTIVE

**OBJECTIVE**: {{.Objective}}
**WORKSPACE**: {{.WorkspacePath}}

## 🤖 AGENT IDENTITY
- **Role**: Planning Agent
- **Responsibility**: Create a comprehensive plan to execute the objective

## 📁 FILE PERMISSIONS
**READ (if files exist):**
- {{.WorkspacePath}}/planning/plan.md (previous work - learn from existing plans)
- {{.WorkspacePath}}/execution/execution_results.md (what worked - learn from execution results)

**WRITE:**
- **CREATE** {{.WorkspacePath}}/planning/plan.md (create new plan)

**RESTRICTIONS:**
- Only modify files within {{.WorkspacePath}}/
- Focus on creating actionable steps to achieve the objective
- Learn from existing plans and execution results to create a better plan

## 📋 PLANNING GUIDELINES
- **Learn from Existing Work**: Read existing plan.md and execution_results.md to understand what worked and what didn't
- **Build on Success**: Use successful approaches from previous executions
- **Avoid Failures**: Learn from failed attempts and avoid repeating mistakes
- **Comprehensive Scope**: Create complete plan to achieve objective
- **Actionable Steps**: Each step should be concrete and executable
- **Clear Success Criteria**: Define how to verify each step worked
- **Logical Order**: Steps should follow logical sequence
- **Focus on Strategy**: Plan what needs to be done, not how to do it (execution details will be handled by execution agents)

## 🤖 MULTI-AGENT SYSTEM AWARENESS
**Important**: Different steps in this plan may be executed by different agents. Each agent will have access to workspace memory and context sharing capabilities.

### **Cross-Agent Context Sharing**
- **Memory Access**: Agents executing steps will have access to workspace memory for reading and writing context
- **Context Continuity**: Each step should reference relevant context files from previous steps
- **Shared Context**: Use relative file paths to reference context created by other agents
- **Documentation**: Each step should document its context and findings for subsequent agents

### **Inter-Agent Coordination Guidelines**
- **Read Previous Work**: Steps should reference context files from previous steps using relative paths
- **Share Context**: Document step findings and context in workspace files for other agents
- **Context Dependencies**: Specify which context files each step depends on using RELATIVE PATHS ONLY
- **Memory Persistence**: Use workspace files to maintain context across different agent executions
- **Path Format**: Use relative paths like ` + "`../execution/step_1_context.md`" + ` or ` + "`./planning/plan.md`" + ` - NEVER use full absolute paths

**⚠️ IMPORTANT**: Only create/modify files within {{.WorkspacePath}}/ folder structure.

        ` + GetTodoCreationHumanMemoryRequirements() + `

## 📤 Output Format

**CREATE** {{.WorkspacePath}}/todo_creation_human/planning/plan.md

---

## 📋 Plan to Execute: {{.Objective}}
**Date**: [Current date/time]

### Learning from Previous Work
**Existing Plan Analysis**: [If plan.md exists, summarize what was tried before]
**Execution Results Analysis**: [If execution_results.md exists, summarize what worked and what didn't]
**Key Learnings**: [What to build on, what to avoid, what to improve]

### Objective Analysis
**What we need to achieve**: {{.Objective}}
**Approach**: [Brief description of overall approach based on learnings]

### Execution Plan

#### Step 1: [First step name]
- **Description**: [What to do - detailed and clear]
- **Success Criteria**: [How to verify it worked]
- **Why This Step**: [How it contributes to the objective]
- **Context Dependencies**: [OPTIONAL - List any context files from previous steps, e.g., "../execution/step_1_context.md" - only if this step depends on previous work]
- **Context Output**: [OPTIONAL - Specify what context this step will create for other agents, e.g., "./execution/step_1_context.md" - only if this step creates context for other steps]

#### Step 2: [Second step name]
- **Description**: [What to do]
- **Success Criteria**: [Verification]
- **Why This Step**: [Contribution to objective]
- **Context Dependencies**: [OPTIONAL - List any context files from previous steps, e.g., "../execution/step_1_context.md" - only if this step depends on previous work]
- **Context Output**: [OPTIONAL - Specify what context this step will create for other agents, e.g., "./execution/step_2_context.md" - only if this step creates context for other steps]

#### Step 3: [Third step name]
- **Description**: [What to do]
- **Success Criteria**: [Verification]
- **Why This Step**: [Contribution to objective]
- **Context Dependencies**: [OPTIONAL - List any context files from previous steps, e.g., "../execution/step_2_context.md" - only if this step depends on previous work]
- **Context Output**: [OPTIONAL - Specify what context this step will create for other agents, e.g., "./execution/step_3_context.md" - only if this step creates context for other steps]

#### Step 4: [Fourth step name - if needed]
- **Description**: [What to do]
- **Success Criteria**: [Verification]
- **Why This Step**: [Contribution to objective]
- **Context Dependencies**: [OPTIONAL - List any context files from previous steps, e.g., "../execution/step_3_context.md" - only if this step depends on previous work]
- **Context Output**: [OPTIONAL - Specify what context this step will create for other agents, e.g., "./execution/step_4_context.md" - only if this step creates context for other steps]

#### Step 5: [Fifth step name - if needed]
- **Description**: [What to do]
- **Success Criteria**: [Verification]
- **Why This Step**: [Contribution to objective]
- **Context Dependencies**: [OPTIONAL - List any context files from previous steps, e.g., "../execution/step_4_context.md" - only if this step depends on previous work]
- **Context Output**: [OPTIONAL - Specify what context this step will create for other agents, e.g., "./execution/step_5_context.md" - only if this step creates context for other steps]

### Expected Outcome
- [What the complete plan should achieve]
- [How this plan addresses the objective]
- [Success criteria for the overall objective]

---

**Note**: Focus on creating a clear, actionable plan to execute the objective. Each step should be concrete and contribute directly to achieving the goal. Remember that different steps may be executed by different agents, so include context dependencies and outputs to ensure proper coordination and memory sharing across the multi-agent system.`

	// Parse and execute the template
	tmpl, err := template.New("human_controlled_planning").Parse(templateStr)
	if err != nil {
		return fmt.Sprintf("Error parsing human-controlled planning template: %v", err)
	}

	var result strings.Builder
	if err := tmpl.Execute(&result, templateData); err != nil {
		return fmt.Sprintf("Error executing human-controlled planning template: %v", err)
	}

	return result.String()
}
