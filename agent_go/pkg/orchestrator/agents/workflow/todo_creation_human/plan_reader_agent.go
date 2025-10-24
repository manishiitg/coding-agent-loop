package todo_creation_human

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"text/template"

	"mcp-agent/agent_go/internal/observability"
	"mcp-agent/agent_go/internal/utils"
	"mcp-agent/agent_go/pkg/mcpagent"
	"mcp-agent/agent_go/pkg/orchestrator/agents"

	"github.com/tmc/langchaingo/llms"
)

// HumanControlledPlanReaderAgent reads markdown plan and converts to structured JSON
type HumanControlledPlanReaderAgent struct {
	*agents.BaseOrchestratorAgent
}

// NewHumanControlledPlanReaderAgent creates a new plan reader agent
func NewHumanControlledPlanReaderAgent(config *agents.OrchestratorAgentConfig, logger utils.ExtendedLogger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) *HumanControlledPlanReaderAgent {
	baseAgent := agents.NewBaseOrchestratorAgentWithEventBridge(
		config,
		logger,
		tracer,
		agents.PlanBreakdownAgentType,
		eventBridge,
	)

	return &HumanControlledPlanReaderAgent{
		BaseOrchestratorAgent: baseAgent,
	}
}

// ExecuteStructured executes the plan reader agent and returns structured output
func (hcpra *HumanControlledPlanReaderAgent) ExecuteStructured(ctx context.Context, templateVars map[string]string, conversationHistory []llms.MessageContent) (*PlanningResponse, error) {
	// Define the JSON schema for plan conversion
	schema := `{
		"type": "object",
		"properties": {
			"objective_analysis": {
				"type": "string",
				"description": "Analysis of what needs to be achieved"
			},
			"approach": {
				"type": "string",
				"description": "Brief description of overall approach"
			},
			"steps": {
				"type": "array",
				"items": {
					"type": "object",
					"properties": {
						"title": {
							"type": "string",
							"description": "Short, clear title for the step"
						},
						"description": {
							"type": "string",
							"description": "Detailed description of what this step accomplishes"
						},
						"success_criteria": {
							"type": "string",
							"description": "How to verify this step was completed successfully"
						},
						"why_this_step": {
							"type": "string",
							"description": "How this step contributes to achieving the objective"
						},
						"context_dependencies": {
							"type": "array",
							"items": {
								"type": "string"
							},
							"description": "List of context files from previous steps that this step depends on"
						},
						"context_output": {
							"type": "string",
							"description": "What context file this step will create for other agents"
						},
						"success_patterns": {
							"type": "array",
							"items": {
								"type": "string"
							},
							"description": "List of approaches that worked, including tools used (extract from 'Success Patterns:' section)"
						},
						"failure_patterns": {
							"type": "array",
							"items": {
								"type": "string"
							},
							"description": "List of approaches that failed, including tools to avoid (extract from 'Failure Patterns:' section)"
						}
					},
					"required": ["title", "description", "success_criteria", "why_this_step"]
				}
			},
			"expected_outcome": {
				"type": "string",
				"description": "What the complete plan should achieve"
			}
		},
		"required": ["objective_analysis", "approach", "steps", "expected_outcome"]
	}`

	// Use the base orchestrator agent's ExecuteStructured method
	result, err := agents.ExecuteStructuredWithInputProcessor[PlanningResponse](hcpra.BaseOrchestratorAgent, ctx, templateVars, hcpra.planReaderInputProcessor, conversationHistory, schema)
	if err != nil {
		return nil, err
	}

	return &result, nil
}

// Execute implements the OrchestratorAgent interface
func (hcpra *HumanControlledPlanReaderAgent) Execute(ctx context.Context, templateVars map[string]string, conversationHistory []llms.MessageContent) (string, []llms.MessageContent, error) {
	// Use ExecuteWithInputProcessor to get agent events (orchestrator_agent_start/end)
	// This will automatically emit agent start/end events
	return hcpra.ExecuteWithInputProcessor(ctx, templateVars, hcpra.planReaderInputProcessor, conversationHistory)
}

// planReaderInputProcessor processes inputs for plan reading and conversion
func (hcpra *HumanControlledPlanReaderAgent) planReaderInputProcessor(templateVars map[string]string) string {
	// Create template data
	templateData := map[string]string{
		"Objective":     templateVars["Objective"],
		"WorkspacePath": templateVars["WorkspacePath"],
		"PlanMarkdown":  templateVars["PlanMarkdown"], // Markdown plan content
	}

	// Define the template for plan reading and conversion
	templateStr := `## 📖 PRIMARY TASK - CONVERT MARKDOWN PLAN TO STRUCTURED JSON ONLY

**OBJECTIVE**: {{.Objective}}
**WORKSPACE**: {{.WorkspacePath}}

## 🤖 AGENT IDENTITY
- **Role**: Plan Reader Agent
- **Responsibility**: Convert markdown plan to structured JSON format
- **NO EXECUTION**: This agent does NOT execute plans - only converts format

## 📁 FILE PERMISSIONS
**READ:**
- **{{.WorkspacePath}}/todo_creation_human/planning/plan.md** (read markdown plan)

**WRITE:**
- **{{.WorkspacePath}}/todo_creation_human/planning/plan.json** (write structured JSON)

## 📋 CONVERSION GUIDELINES

**Your ONLY Job**:
1. Read the markdown plan from plan.md
2. Parse the markdown structure into structured JSON format
3. Extract objective analysis, approach, steps, and expected outcome
4. Convert each step's details into the required JSON structure
5. Return structured JSON response

**DO NOT**:
- Execute any steps from the plan
- Modify the plan content
- Add execution logic
- Create additional files beyond plan.json

**JSON Structure Requirements**:
- objective_analysis: Extract from "## Objective Analysis" section
- approach: Extract from "## Approach" section  
- steps: Extract from "## Steps" section, each step with:
  - title: From "### Step X: [Title]"
  - description: From "- **Description**: [content]"
  - success_criteria: From "- **Success Criteria**: [content]"
  - why_this_step: From "- **Why This Step**: [content]"
  - context_dependencies: From "- **Context Dependencies**: [content]" (convert to array)
  - context_output: From "- **Context Output**: [content]"
  - success_patterns: From "- **Success Patterns**: [bullet list]" (convert to array, include if present)
  - failure_patterns: From "- **Failure Patterns**: [bullet list]" (convert to array, include if present)
- expected_outcome: Extract from "## Expected Outcome" section

## 📤 Output Format

**RETURN STRUCTURED JSON RESPONSE ONLY**

Convert the markdown plan to structured JSON format. Return ONLY the JSON object that matches the required schema.

## 📊 MARKDOWN PLAN CONTENT
{{.PlanMarkdown}}

**IMPORTANT NOTES**: 
1. Parse the markdown structure carefully to extract all required fields
2. Convert markdown lists and formatting to clean JSON values
3. Ensure all steps are properly extracted with their details
4. Return ONLY valid JSON - no explanations or additional text
5. This agent ONLY converts format - execution is handled by other agents
6. Focus on accurate parsing, not plan modification or execution`

	// Parse and execute the template
	tmpl, err := template.New("plan_reader").Parse(templateStr)
	if err != nil {
		return fmt.Sprintf("Error parsing plan reader template: %v", err)
	}

	var result strings.Builder
	if err := tmpl.Execute(&result, templateData); err != nil {
		return fmt.Sprintf("Error executing plan reader template: %v", err)
	}

	return result.String()
}

// ReadPlanMarkdown reads the markdown plan file and returns its content
func (hcpra *HumanControlledPlanReaderAgent) ReadPlanMarkdown(ctx context.Context, workspacePath string) (string, error) {
	// This would typically use MCP tools to read the file
	// For now, we'll assume the content is passed via templateVars
	return "", fmt.Errorf("plan markdown reading not implemented yet - use templateVars")
}

// WriteStructuredPlan writes the structured plan to JSON file
func (hcpra *HumanControlledPlanReaderAgent) WriteStructuredPlan(ctx context.Context, workspacePath string, plan *PlanningResponse) error {
	// Convert plan to JSON
	_, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal plan to JSON: %w", err)
	}

	// This would typically use MCP tools to write the file
	// For now, we'll assume the conversion happens in the agent execution
	// Note: Logger access would need to be implemented based on BaseOrchestratorAgent structure
	return nil
}
