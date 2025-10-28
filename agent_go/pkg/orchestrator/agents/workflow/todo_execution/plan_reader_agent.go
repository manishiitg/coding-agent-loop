package todo_execution

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

// FlexibleContextOutput handles both string and array formats for context_output
type FlexibleContextOutput string

// UnmarshalJSON implements custom unmarshaling for FlexibleContextOutput
// Handles both string and array formats to prevent parsing errors
func (f *FlexibleContextOutput) UnmarshalJSON(data []byte) error {
	// Try to unmarshal as string first
	var str string
	if err := json.Unmarshal(data, &str); err == nil {
		*f = FlexibleContextOutput(str)
		return nil
	}

	// Try to unmarshal as array
	var arr []string
	if err := json.Unmarshal(data, &arr); err == nil {
		// Convert array to comma-separated string
		*f = FlexibleContextOutput(strings.Join(arr, ", "))
		return nil
	}

	return json.Unmarshal(data, f)
}

// PlanStep represents a step in the execution plan
type PlanStep struct {
	Title               string                `json:"title"`
	Description         string                `json:"description"`
	SuccessCriteria     string                `json:"success_criteria"`
	WhyThisStep         string                `json:"why_this_step"`
	ContextDependencies []string              `json:"context_dependencies"`
	ContextOutput       FlexibleContextOutput `json:"context_output"`
	SuccessPatterns     []string              `json:"success_patterns,omitempty"`
	FailurePatterns     []string              `json:"failure_patterns,omitempty"`
}

// PlanningResponse represents the structured response from plan reading
type PlanningResponse struct {
	Steps []PlanStep `json:"steps"`
}

// TodoStep represents a todo step for execution
type TodoStep struct {
	Title               string   `json:"title"`
	Description         string   `json:"description"`
	SuccessCriteria     string   `json:"success_criteria"`
	WhyThisStep         string   `json:"why_this_step"`
	ContextDependencies []string `json:"context_dependencies"`
	ContextOutput       string   `json:"context_output"`
	SuccessPatterns     []string `json:"success_patterns,omitempty"`
	FailurePatterns     []string `json:"failure_patterns,omitempty"`
}

// PlanReaderAgent reads markdown todo_final.md file and converts to structured JSON format
// This agent reads files but does NOT write files - it only returns structured data
type PlanReaderAgent struct {
	*agents.BaseOrchestratorAgent
}

// NewPlanReaderAgent creates a new plan reader agent for todo execution
func NewPlanReaderAgent(config *agents.OrchestratorAgentConfig, logger utils.ExtendedLogger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) *PlanReaderAgent {
	baseAgent := agents.NewBaseOrchestratorAgentWithEventBridge(
		config,
		logger,
		tracer,
		agents.PlanBreakdownAgentType,
		eventBridge,
	)

	return &PlanReaderAgent{
		BaseOrchestratorAgent: baseAgent,
	}
}

// ExecuteStructured executes the plan reader agent and returns structured output
func (pra *PlanReaderAgent) ExecuteStructured(ctx context.Context, templateVars map[string]string, conversationHistory []llms.MessageContent) (*PlanningResponse, error) {
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
	result, err := agents.ExecuteStructuredWithInputProcessor[PlanningResponse](pra.BaseOrchestratorAgent, ctx, templateVars, pra.planReaderInputProcessor, conversationHistory, schema)
	if err != nil {
		return nil, err
	}

	return &result, nil
}

// Execute implements the OrchestratorAgent interface
func (pra *PlanReaderAgent) Execute(ctx context.Context, templateVars map[string]string, conversationHistory []llms.MessageContent) (string, []llms.MessageContent, error) {
	// Use ExecuteWithInputProcessor to get agent events (orchestrator_agent_start/end)
	return pra.ExecuteWithInputProcessor(ctx, templateVars, pra.planReaderInputProcessor, conversationHistory)
}

// planReaderInputProcessor processes inputs for plan reading and conversion
func (pra *PlanReaderAgent) planReaderInputProcessor(templateVars map[string]string) string {
	// Create template data
	templateData := map[string]string{
		"Objective":     templateVars["Objective"],
		"WorkspacePath": templateVars["WorkspacePath"],
		"PlanMarkdown":  templateVars["PlanMarkdown"],
		"VariableNames": templateVars["VariableNames"],
	}

	// Define the template for plan reading and conversion
	templateStr := `## 📖 PRIMARY TASK - CONVERT MARKDOWN TODO LIST TO STRUCTURED JSON ONLY

**OBJECTIVE**: {{.Objective}}
**WORKSPACE**: {{.WorkspacePath}}

## 🤖 AGENT IDENTITY
- **Role**: Todo List Reader Agent
- **Responsibility**: Convert markdown todo list content to structured JSON format
- **Input**: Markdown todo_final.md file
- **Output**: Structured JSON data
- **NO EXECUTION**: This agent does NOT execute todos - only converts format
- **READ ONLY**: This agent reads files but does NOT write any files

## 📁 FILE PERMISSIONS
**READ:**
- **{{.WorkspacePath}}/../../todo_final.md** (read markdown todo list)

**NO WRITE PERMISSIONS:**
- This agent does NOT write any files - only reads and converts

## 🔍 STEP 1 - VARIABLE DETECTION AND RESOLUTION

**BEFORE converting to JSON**, you must detect and resolve any variables in the plan using the human_feedback tool:

**CRITICAL VARIABLE DETECTION**:
1. Scan the entire markdown content below for double-curly-brace variable patterns
2. **Extract ALL unique variable names** (e.g., AWS_ACCOUNT_ID, GITHUB_REPO_URL)
3. **For each variable found**, you MUST use the **human_feedback tool** (from human_tools.go) to ask the human for its value
4. **Replace all variable placeholders** with actual values from the human
5. **ONLY AFTER all variables are resolved**, proceed to convert to JSON

**IMPORTANT - Use human_feedback Tool**:
- **Tool Name**: human_feedback (available in your workspace tools)
- **Purpose**: Pause execution and request input from the human
- **Parameters Required**:
  - unique_id: Generate a UUID for each request (e.g., "550e8400-e29b-41d4-a716-446655440000")
  - message_for_user: Your message asking for the variable value

**Variable Detection Rules**:
- Look for patterns like VARIABLE_NAME wrapped in double curly braces
- Common variables: AWS_ACCOUNT_ID, GITHUB_REPO_URL, DB_PASSWORD, etc.
- Extract each unique variable name
- **Use the human_feedback tool** to ask: "Please provide the value for variable: VARIABLE_NAME"
- **Generate a unique_id** (UUID) for each feedback request
- **Replace variable placeholders** with the actual value provided by the human
- **Continue this process** for ALL variables until the plan has no more placeholders

**Tool Usage Example**:x
` + "```json" + `
{
  "unique_id": "550e8400-e29b-41d4-a716-446655440000",
  "message_for_user": "Please provide the value for variable: AWS_ACCOUNT_ID"
}
` + "```" + `

**Example Flow**:
- **Input**: "Deploy to AWS account [VARIABLE_PLACEHOLDER]"
- **Action**: Call the human_feedback tool asking for AWS_ACCOUNT_ID value
- **Human provides**: "123456789012"
- **Replace**: "Deploy to AWS account 123456789012"
- **Then**: Proceed with JSON conversion

**NOTE**: The human_feedback tool will pause execution and wait for the human to respond. Use this for every variable that needs a value.

## 📋 STEP 2 - CONVERSION GUIDELINES

**Your ONLY Job** (after variables are resolved):
1. Read the markdown todo list from todo_final.md
2. Parse the markdown structure into structured JSON format
3. Extract steps
4. Convert each step's details into the required JSON structure
5. Return structured JSON response

{{if .VariableNames}}
## 🔑 AVAILABLE VARIABLES

These variables may appear in the plan as placeholders (see list below):
{{.VariableNames}}

**CRITICAL**: When converting the plan to JSON, preserve ALL variable placeholders exactly as written. 
DO NOT replace them with actual values. Keep placeholders like AWS_ACCOUNT_ID or GITHUB_REPO_URL intact.
The output JSON must maintain variable placeholders, not resolved values.
{{end}}

**DO NOT**:
- Execute any todos from the list
- Modify the todo list content
- Add execution logic
- Write or create any files
- Save JSON output to files

**JSON Structure Requirements**:
- steps: Extract from "## Steps" section or "## 📝 Step-by-Step Execution Plan" section, each step with:
  - title: From "### Step X: [Title]"
  - description: From "- **Description**: [content]"
  - success_criteria: From "- **Success Criteria**: [content]"
  - why_this_step: From "- **Why This Step**: [content]"
  - context_dependencies: From "- **Context Dependencies**: [content]" - See conversion rules below
  - context_output: From "- **Context Output**: [content]"

**Context Dependencies Conversion Rules**:
- "none" → [] (empty array)
- "step_1_results.md" → ["step_1_results.md"]
- "step_1_results.md, step_2_data.json" → ["step_1_results.md", "step_2_data.json"]
- If section is missing → [] (empty array)

**Error Handling**:
- Missing required sections (title, description, etc.) → Include step with available fields only
- Malformed markdown structure → Skip problematic step and continue with others
- Empty file or no steps found → Return ` + "`{\"steps\": []}`" + `
- Invalid step format → Log warning and attempt best-effort parsing

## 📤 Output Format

**RETURN STRUCTURED JSON RESPONSE ONLY**

Convert the markdown todo list to structured JSON format. Return ONLY the JSON object that matches the required schema.

## 📊 MARKDOWN TODO LIST CONTENT
{{.PlanMarkdown}}

**IMPORTANT NOTES**: 
1. Read the markdown todo list file from todo_final.md
2. Parse the markdown structure carefully to extract all required fields
3. Convert markdown lists and formatting to clean JSON values
4. Ensure all steps are properly extracted with their details
5. Return ONLY valid JSON - no explanations or additional text
6. This agent ONLY converts format - execution is handled by other agents
7. Focus on accurate parsing, not todo list modification or execution
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
