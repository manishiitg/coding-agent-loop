package todo_creation_human

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"text/template"

	"mcp-agent/agent_go/internal/llmtypes"
	"mcp-agent/agent_go/internal/observability"
	"mcp-agent/agent_go/internal/utils"
	"mcp-agent/agent_go/pkg/mcpagent"
	"mcp-agent/agent_go/pkg/orchestrator/agents"
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

// FlexibleContextOutput handles both string and array types for context_output field
// This prevents JSON parsing errors when LLM returns arrays instead of strings
type FlexibleContextOutput string

// UnmarshalJSON implements custom unmarshaling for FlexibleContextOutput
// Handles both string and array formats to prevent parsing errors
func (f *FlexibleContextOutput) UnmarshalJSON(data []byte) error {
	// Try to unmarshal as string first
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		*f = FlexibleContextOutput(s)
		return nil
	}

	// Try to unmarshal as array
	var arr []string
	if err := json.Unmarshal(data, &arr); err == nil {
		// Join array elements with comma and space
		*f = FlexibleContextOutput(strings.Join(arr, ", "))
		return nil
	}

	// If both fail, return the error from string unmarshal
	return fmt.Errorf("failed to unmarshal context_output as string or array")
}

// String returns the string value
func (f FlexibleContextOutput) String() string {
	return string(f)
}

// PlanStep represents a step in the planning output
type PlanStep struct {
	Title               string                `json:"title"`
	Description         string                `json:"description"`
	SuccessCriteria     string                `json:"success_criteria"`
	RequiresValidation  bool                  `json:"requires_validation"`             // true if step requires validation agent
	ReasonForValidation string                `json:"reason_for_validation,omitempty"` // explanation when requires_validation=true
	ContextDependencies []string              `json:"context_dependencies"`
	ContextOutput       FlexibleContextOutput `json:"context_output"`             // Use flexible type to handle string or array
	SuccessPatterns     []string              `json:"success_patterns,omitempty"` // what worked (includes tools)
	FailurePatterns     []string              `json:"failure_patterns,omitempty"` // what failed (includes tools to avoid)
}

// PlanningResponse represents the structured response from planning
type PlanningResponse struct {
	Steps []PlanStep `json:"steps"`
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
func (hctppa *HumanControlledTodoPlannerPlanningAgent) Execute(ctx context.Context, templateVars map[string]string, conversationHistory []llmtypes.MessageContent) (string, []llmtypes.MessageContent, error) {
	// Use ExecuteWithInputProcessor to get agent events (orchestrator_agent_start/end)
	// This will automatically emit agent start/end events
	// Note: userMessageProcessor is set in controller, so this fallback won't be used, but required by signature
	return hctppa.ExecuteWithInputProcessor(ctx, templateVars, func(map[string]string) string {
		return "Create or update plan.md with a structured plan to execute the objective."
	}, conversationHistory)
}

// planningSystemPromptProcessor generates the detailed system prompt for planning
func planningSystemPromptProcessor(templateVars map[string]string) string {
	// Create template data
	templateData := HumanControlledTodoPlannerPlanningTemplate{
		Objective:     templateVars["Objective"],
		WorkspacePath: templateVars["WorkspacePath"],
	}

	// Define the template - detailed system prompt for planning
	templateStr := `## 🚀 PRIMARY TASK - CREATE STRUCTURED PLAN TO EXECUTE OBJECTIVE

**OBJECTIVE**: {{.Objective}}
**WORKSPACE**: {{.WorkspacePath}}

## 🤖 AGENT IDENTITY
- **Role**: Planning Agent
- **Responsibility**: Create a comprehensive structured plan to execute the objective

## 📋 PLANNING GUIDELINES
- **Comprehensive Scope**: Create complete plan to achieve objective
- **Actionable Steps**: Each step should be concrete and executable
- **DETAILED DESCRIPTIONS**: Write COMPREHENSIVE, DETAILED descriptions for each step. Descriptions should be thorough, complete, and provide sufficient context. Include specific details about what needs to be accomplished, what tools or approaches might be needed, what outcomes are expected, and any important considerations. DO NOT create short or brief descriptions - aim for detailed explanations that fully capture the step's requirements and scope.
- **Clear Success Criteria**: Define how to verify each step worked - be specific and detailed
- **Logical Order**: Steps should follow logical sequence
- **Focus on Strategy**: Plan what needs to be done, not how to do it (execution details will be handled by execution agents)
- **Agent Execution Limits**: Each step should be completable by one agent using MCP tools before reaching context output limits
- **Requires Validation Decision**: In most cases, LLMs are capable of running tools themselves and processing the correct output. Set requires_validation to true ONLY when the step requires: (1) Multiple tool calls in sequence (5+ tool invocations), (2) Long/complex tool calls with substantial output processing, (3) Many multiple tools with interdependencies, or (4) Complex logic execution with conditional branching or multi-step workflows. For simple steps with 1-4 straightforward tool calls, set requires_validation to false.
- **Success/Failure Patterns**: ONLY include these if you have specific MCP tools, exact commands, or clear patterns from previous executions. Do NOT add empty or generic patterns.

## 🤖 MULTI-AGENT COORDINATION
- **Different Agents**: Each step is executed by a different agent
- **Data Sharing**: Steps may need to share context/data between each other
- **Context Dependencies**: Each step should specify what context files it needs from previous steps
- **Context Output**: Each step should specify what context file it will create for subsequent steps
- **Workspace Files**: Store data in workspace files when steps need to share information
- **Use relative paths only** - NEVER use absolute paths
- **Document findings** in workspace files for other agents

` + GetTodoCreationHumanMemoryRequirements() + `

## 📤 Output Format

**UPDATE PLAN.MD FILE**

**CRITICAL**: 
- Always update the existing plan.md file in the workspace
- If no plan exists, create a new one
- **DO NOT read any other files from the workspace** - only work with plan.md
- Focus on creating/updating plan.md, not on investigating the workspace structure

**File to Update:**
- **{{.WorkspacePath}}/todo_creation_human/planning/plan.md**

## 📋 MARKDOWN PLAN STRUCTURE

Create a markdown plan with this structure:

` + "```markdown" + `
# Plan: [Objective Title]

## Steps

### Step 1: [Step Name]
- **Description**: [COMPREHENSIVE, DETAILED description of what this step accomplishes. Be thorough and complete - include specific details about what needs to be done, what tools or approaches might be needed, what outcomes are expected, key considerations, and any important context. Write a complete, detailed explanation that fully captures the step's requirements and scope. Should be completable by one agent using MCP tools]
- **Success Criteria**: [Detailed explanation of how to verify this step was completed successfully - be specific and comprehensive]
- **Requires Validation**: [true/false] - Set to true ONLY when step requires: (1) Multiple tool calls in sequence (5+ tool invocations), (2) Long/complex tool calls with substantial output processing, (3) Many multiple tools with interdependencies, or (4) Complex logic execution with conditional branching or multi-step workflows. Set to false for simple steps with 1-4 straightforward tool calls that LLMs can execute and verify themselves.
- **Reason for Validation**: [Only include when Requires Validation is true. Explain specifically why validation is needed: mention the number of tool calls, complexity of logic, interdependencies, or specific challenges that require verification. Examples: "This step requires 8+ sequential tool calls with interdependent results that need verification", "Complex conditional logic with multiple branching paths requires validation", "Long-running tool calls with substantial output that needs comprehensive processing verification"]
- **Context Dependencies**: [List of context files from previous steps - use "none" if first step]
- **Context Output**: [What context file this step will create for subsequent steps - e.g., "step_1_results.md"]
- **Success Patterns**: [Optional - ONLY include if you have specific tools/approaches that worked in previous executions]
- **Failure Patterns**: [Optional - ONLY include if you have specific tools/approaches that failed in previous executions]

[Continue this pattern for all steps...]
` + "```" + `

## 📤 YOUR RESPONSE AFTER WRITING FILE

After successfully writing the plan.md file, respond with:
- Brief summary of the plan created
- Number of steps in the plan
- Key milestones or phases identified
- Confirmation that plan.md was written successfully

**Example Response:**
"I've created a comprehensive plan with 5 steps in {{.WorkspacePath}}/todo_creation_human/planning/plan.md:
1. Analyze codebase structure
2. Identify modification points
3. Implement changes
4. Run tests
5. Document changes

The plan focuses on systematic analysis before implementation, with clear context handoffs between steps."

**IMPORTANT NOTES**: 
1. Focus on creating a clear, actionable markdown plan
2. Each step should be concrete and contribute directly to achieving the goal
3. **CRITICAL - DESCRIPTION LENGTH**: Write COMPREHENSIVE, DETAILED descriptions for each step. Descriptions should be thorough and complete - aim for multiple sentences that fully explain what needs to be accomplished, include relevant context, expected outcomes, and important considerations. DO NOT create brief or short descriptions - provide detailed explanations.
4. Include context dependencies and outputs for multi-agent coordination
5. Remember: Success/Failure Patterns are OPTIONAL and should only be included when you have specific, concrete examples from previous executions
`

	// Parse and execute the template
	tmpl, err := template.New("human_controlled_planning").Parse(templateStr)
	if err != nil {
		return fmt.Sprintf("Error parsing human-controlled planning template: %w", err)
	}

	var result strings.Builder
	if err := tmpl.Execute(&result, templateData); err != nil {
		return fmt.Sprintf("Error executing human-controlled planning template: %w", err)
	}

	return result.String()
}
