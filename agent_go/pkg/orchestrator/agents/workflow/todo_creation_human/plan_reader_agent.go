package todo_creation_human

import (
	"context"
	"fmt"
	"strings"
	"text/template"

	"mcp-agent/agent_go/internal/llmtypes"
	"mcp-agent/agent_go/internal/observability"
	"mcp-agent/agent_go/internal/utils"
	"mcp-agent/agent_go/pkg/mcpagent"
	"mcp-agent/agent_go/pkg/orchestrator/agents"
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
		agents.PlanReaderAgentType,
		eventBridge,
	)

	return &HumanControlledPlanReaderAgent{
		BaseOrchestratorAgent: baseAgent,
	}
}

// ExecuteStructured executes the plan reader agent and returns structured output
func (hcpra *HumanControlledPlanReaderAgent) ExecuteStructured(ctx context.Context, templateVars map[string]string, conversationHistory []llmtypes.MessageContent) (*PlanningResponse, error) {
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
func (hcpra *HumanControlledPlanReaderAgent) Execute(ctx context.Context, templateVars map[string]string, conversationHistory []llmtypes.MessageContent) (string, []llmtypes.MessageContent, error) {
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
		"PlanMarkdown":  templateVars["PlanMarkdown"],  // Markdown plan content
		"VariableNames": templateVars["VariableNames"], // Available variables
	}

	// Define the template for plan reading and conversion
	templateStr := `## 📖 PRIMARY TASK - CONVERT MARKDOWN PLAN TO STRUCTURED JSON ONLY

**OBJECTIVE**: {{.Objective}}
**WORKSPACE**: {{.WorkspacePath}}

## 🤖 AGENT IDENTITY
- **Role**: Plan Reader Agent
- **Responsibility**: Convert markdown plan content to structured JSON format
- **Input**: Markdown plan.md file
- **Output**: Structured JSON data
- **NO EXECUTION**: This agent does NOT execute plans - only converts format
- **READ ONLY**: This agent reads files but does NOT write any files

## 📁 FILE PERMISSIONS
**READ:**
- **{{.WorkspacePath}}/todo_creation_human/planning/plan.md** (read markdown plan)
- **{{.WorkspacePath}}/todo_creation_human/learnings/success_patterns.md** (success learning insights - if exists)
- **{{.WorkspacePath}}/todo_creation_human/learnings/failure_analysis.md** (failure patterns to avoid - if exists)
- **{{.WorkspacePath}}/todo_creation_human/learnings/step_*_learning.md** (per-step learning details - if exists)

**NO WRITE PERMISSIONS:**
- This agent does NOT write any files - only reads and converts

## 📋 CONVERSION GUIDELINES

**Your ONLY Job**:
1. Read the markdown plan from plan.md
2. **Read learnings files** from learnings/ directory (if they exist):
   - Read learnings/success_patterns.md to extract success patterns
   - Read learnings/failure_analysis.md to extract failure patterns
   - Read learnings/step_*_learning.md for per-step learning details
3. Parse the markdown structure into structured JSON format
4. Extract steps
5. Convert each step's details into the required JSON structure
6. **Incorporate learnings** into success_patterns and failure_patterns fields when available
7. Return structured JSON response

{{if .VariableNames}}
## 🔑 AVAILABLE VARIABLES

These variables may appear in the plan as placeholders (see list below):
{{.VariableNames}}

**CRITICAL**: When converting the plan to JSON, preserve ALL variable placeholders exactly as written. 
DO NOT replace them with actual values. Keep placeholders like AWS_ACCOUNT_ID or GITHUB_REPO_URL intact.
The output JSON must maintain variable placeholders, not resolved values.
{{end}}

**DO NOT**:
- Execute any steps from the plan
- Modify the plan content
- Add execution logic
- Write or create any files
- Save JSON output to files

**JSON Structure Requirements**:
- steps: Extract from "## Steps" section, each step with:
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

**Incorporating Learnings**:
When learnings files exist, enhance the extracted patterns:
- Read learnings/success_patterns.md to find additional success patterns not in the plan
- Read learnings/failure_analysis.md to find failure patterns to include
- Read step-specific learnings/step_X_learning.md to get detailed insights for each step
- Merge learnings patterns with patterns extracted from the plan
- If same pattern appears in both source and learnings, include it only once

Special Cases:
- If section has "none" or is empty → omit field entirely (don't include empty array)
- If section is missing → omit field entirely
- If nested bullets exist → flatten to single-level array
- Preserve original text without modification
- If learnings files don't exist → use only patterns from the plan

**Error Handling**:
- Missing required sections (title, description, etc.) → Include step with available fields only
- Malformed markdown structure → Skip problematic step and continue with others
- Empty file or no steps found → Return ` + "`{\"steps\": []}`" + `
- Invalid step format → Log warning and attempt best-effort parsing

## 📤 Output Format

**RETURN STRUCTURED JSON RESPONSE ONLY**

Convert the markdown plan to structured JSON format. Return ONLY the JSON object that matches the required schema.

## 📊 MARKDOWN PLAN CONTENT
{{.PlanMarkdown}}

**IMPORTANT NOTES**: 
1. Read the markdown plan file from plan.md
2. **Read learnings files** from todo_creation_human/learnings/ directory if they exist (handle gracefully if missing)
3. Parse the markdown structure carefully to extract all required fields
4. **Incorporate learnings** into success_patterns and failure_patterns when available
5. Convert markdown lists and formatting to clean JSON values
6. Ensure all steps are properly extracted with their details
7. Return ONLY valid JSON - no explanations or additional text
8. This agent ONLY converts format - execution is handled by other agents
9. Focus on accurate parsing, not plan modification or execution
10. This agent reads markdown files and returns structured JSON - NO file writing
11. Learnings enhance the output with real-world execution patterns and insights`

	// Parse and execute the template
	tmpl, err := template.New("plan_reader").Parse(templateStr)
	if err != nil {
		return fmt.Sprintf("Error parsing plan reader template: %w", err)
	}

	var result strings.Builder
	if err := tmpl.Execute(&result, templateData); err != nil {
		return fmt.Sprintf("Error executing plan reader template: %w", err)
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
