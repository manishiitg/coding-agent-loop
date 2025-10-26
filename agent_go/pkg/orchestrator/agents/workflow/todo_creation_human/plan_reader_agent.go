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

// HumanControlledPlanReaderAgent reads markdown plan files and converts to structured JSON format
// This agent can read files but does NOT write files - it only returns structured data
// File reading is performed by the agent, file writing is handled by the orchestrator
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
			}
		},
		"required": ["steps"]
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
		"FileType":      templateVars["FileType"],     // "plan" or "todo_final"
	}

	// Define the template for plan reading and conversion
	templateStr := `## 📖 PRIMARY TASK - CONVERT MARKDOWN {{if eq .FileType "todo_final"}}TODO LIST{{else}}PLAN{{end}} TO STRUCTURED JSON ONLY

**OBJECTIVE**: {{.Objective}}
**WORKSPACE**: {{.WorkspacePath}}
**FILE TYPE**: {{.FileType}}

## 🤖 AGENT IDENTITY
- **Role**: {{if eq .FileType "todo_final"}}Todo List{{else}}Plan{{end}} Reader Agent
- **Responsibility**: Convert markdown {{if eq .FileType "todo_final"}}todo list{{else}}plan{{end}} content to structured JSON format
- **Input**: Markdown {{if eq .FileType "todo_final"}}todo_final.md{{else}}plan.md{{end}} file
- **Output**: Structured JSON data
- **NO EXECUTION**: This agent does NOT execute {{if eq .FileType "todo_final"}}todos{{else}}plans{{end}} - only converts format
- **READ ONLY**: This agent reads files but does NOT write any files

## 📁 FILE PERMISSIONS
**READ:**
- **{{if eq .FileType "todo_final"}}{{.WorkspacePath}}/todo_final.md{{else}}{{.WorkspacePath}}/todo_creation_human/planning/plan.md{{end}}** (read markdown {{if eq .FileType "todo_final"}}todo list{{else}}plan{{end}})

**NO WRITE PERMISSIONS:**
- This agent does NOT write any files - only reads and converts

## 📋 CONVERSION GUIDELINES

**Your ONLY Job**:
1. Read the markdown {{if eq .FileType "todo_final"}}todo list{{else}}plan{{end}} from {{if eq .FileType "todo_final"}}todo_final.md{{else}}plan.md{{end}}
2. Parse the markdown structure into structured JSON format
3. Extract steps
4. Convert each step's details into the required JSON structure
5. Return structured JSON response

**DO NOT**:
- Execute any {{if eq .FileType "todo_final"}}todos{{else}}steps{{end}} from the {{if eq .FileType "todo_final"}}list{{else}}plan{{end}}
- Modify the {{if eq .FileType "todo_final"}}todo list{{else}}plan{{end}} content
- Add execution logic
- Write or create any files
- Save JSON output to files

**JSON Structure Requirements**:
- steps: Extract from "## Steps" section{{if eq .FileType "todo_final"}} or "## 📝 Step-by-Step Execution Plan" section{{end}}, each step with:
  - title: From "### Step X: [Title]"
  - description: From "- **Description**: [content]"
  - success_criteria: From "- **Success Criteria**: [content]"
  - why_this_step: From "- **Why This Step**: [content]"
  - context_dependencies: From "- **Context Dependencies**: [content]" - See conversion rules below
  - context_output: From "- **Context Output**: [content]"
  - success_patterns: From "- **Success Patterns**: [bullet list]" - See parsing rules below
  - failure_patterns: From "- **Failure Patterns**: [bullet list]" - See parsing rules below

**Context Dependencies Conversion Rules**:
- "none" → [] (empty array)
- "step_1_results.md" → ["step_1_results.md"]
- "step_1_results.md, step_2_data.json" → ["step_1_results.md", "step_2_data.json"]
- If section is missing → [] (empty array)

**Pattern Parsing Rules**:
Markdown Input:
` + "```" + `
- **Success Patterns**:
  - Used grep tool with --type flag
  - read_file with line limits worked well
` + "```" + `

JSON Output:
` + "```json" + `
"success_patterns": [
  "Used grep tool with --type flag",
  "read_file with line limits worked well"
]
` + "```" + `

Special Cases:
- If section has "none" or is empty → omit field entirely (don't include empty array)
- If section is missing → omit field entirely
- If nested bullets exist → flatten to single-level array
- Preserve original text without modification

**Error Handling**:
- Missing required sections (title, description, etc.) → Include step with available fields only
- Malformed markdown structure → Skip problematic step and continue with others
- Empty file or no steps found → Return ` + "`{\"steps\": []}`" + `
- Invalid step format → Log warning and attempt best-effort parsing

## 📤 Output Format

**RETURN STRUCTURED JSON RESPONSE ONLY**

Convert the markdown {{if eq .FileType "todo_final"}}todo list{{else}}plan{{end}} to structured JSON format. Return ONLY the JSON object that matches the required schema.

## 📊 MARKDOWN {{if eq .FileType "todo_final"}}TODO LIST{{else}}PLAN{{end}} CONTENT
{{.PlanMarkdown}}

**IMPORTANT NOTES**: 
1. Read the markdown {{if eq .FileType "todo_final"}}todo list{{else}}plan{{end}} file from {{if eq .FileType "todo_final"}}todo_final.md{{else}}plan.md{{end}}
2. Parse the markdown structure carefully to extract all required fields
3. Convert markdown lists and formatting to clean JSON values
4. Ensure all steps are properly extracted with their details
5. Return ONLY valid JSON - no explanations or additional text
6. This agent ONLY converts format - execution is handled by other agents
7. Focus on accurate parsing, not {{if eq .FileType "todo_final"}}todo list{{else}}plan{{end}} modification or execution
8. This agent reads markdown files and returns structured JSON - NO file writing`

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
	// TODO: Implement actual file reading using MCP tools
	return "", fmt.Errorf("plan markdown reading not implemented yet - use templateVars")
}
