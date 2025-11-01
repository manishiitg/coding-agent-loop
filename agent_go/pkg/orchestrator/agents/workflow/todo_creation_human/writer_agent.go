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

// HumanControlledTodoPlannerWriterTemplate holds template variables for human-controlled todo writing prompts
type HumanControlledTodoPlannerWriterTemplate struct {
	Objective       string
	WorkspacePath   string
	TotalIterations string
	VariableNames   string // Available variables for masking
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
func (hctpwa *HumanControlledTodoPlannerWriterAgent) Execute(ctx context.Context, templateVars map[string]string, conversationHistory []llmtypes.MessageContent) (string, []llmtypes.MessageContent, error) {
	// Extract variables from template variables
	// Human-controlled writer - synthesizes from single execution for todo list creation
	objective := templateVars["Objective"]
	workspacePath := templateVars["WorkspacePath"]
	totalIterations := templateVars["TotalIterations"]
	variableNames := templateVars["VariableNames"] // Optional - may be empty if no variables
	if strings.TrimSpace(totalIterations) == "" {
		totalIterations = "1"
	}

	// Prepare template variables
	writerTemplateVars := map[string]string{
		"Objective":       objective,
		"WorkspacePath":   workspacePath,
		"TotalIterations": totalIterations,
	}

	// Add variable names if provided
	if variableNames != "" {
		writerTemplateVars["VariableNames"] = variableNames
	}

	// Create template data for validation
	templateData := HumanControlledTodoPlannerWriterTemplate{
		Objective:       objective,
		WorkspacePath:   workspacePath,
		TotalIterations: totalIterations,
		VariableNames:   variableNames,
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
		VariableNames:   templateVars["VariableNames"], // Optional - may be empty
	}

	// Define the template - structured format matching planner agent for LLM execution
	templateStr := `## 🎯 PRIMARY TASK - CREATE STRUCTURED TODO LIST

**OBJECTIVE**: {{.Objective}}
**WORKSPACE**: {{.WorkspacePath}}

## 🤖 AGENT IDENTITY
- **Role**: Writer Agent
- **Responsibility**: Create structured, execution-ready todo list based on original plan and learnings
- **Mode**: Structured synthesis (create step-by-step plan format that another LLM can efficiently execute)

## 📁 FILE PERMISSIONS (Writer Agent)

**READ:**
- planning/plan.md (original plan - primary source)
- learnings/success_patterns.md (success learning insights - if exists)
- learnings/failure_analysis.md (failure patterns to avoid - if exists)
- learnings/step_*_learning.md (per-step learning details - if exists)

**WRITE:**
- todo_final.md (final structured todo list - writes to workspace root: {{.WorkspacePath}}/todo_final.md)

**RESTRICTIONS:**
- Read from todo_creation_human/ folder (planning/ and learnings/ only)
- Focus on extracting patterns and insights from plan.md and learnings (ignore execution/validation data)
- Write todo_final.md to workspace root (NOT inside todo_creation_human/)
- Format MUST be parseable and executable by another LLM
- Handle missing learning files gracefully (they may not exist for all steps)

{{if .VariableNames}}
## 🔑 VARIABLE MASKING REQUIREMENT

**IMPORTANT**: plan.md, success_learnings, and failure_learnings files may contain ACTUAL VALUES (like real account IDs, URLs, etc.)

Available variables to mask:
{{.VariableNames}}

**CRITICAL INSTRUCTIONS**:
- **REPLACE** any actual values from ALL sources (plan.md, learning files) with the corresponding {{"{{"}}VARIABLE{{"}}"}} placeholder
- **PRESERVE** any {{"{{"}}VARIABLE{{"}}"}} placeholders already in plan.md
- The final todo_final.md must use ONLY placeholders like {{"{{"}}VARIABLE_NAME{{"}}"}}, NEVER actual values
- Match actual values to their variable names and replace with the appropriate {{"{{"}}VARIABLE_NAME{{"}}"}} placeholder
- Apply masking to all content: plan descriptions, success patterns, failure patterns, and step details

**Examples**:
- If plan.md has actual value "account-123456" and variable name is "AWS_ACCOUNT_ID" → todo_final.md should have {{"{{"}}AWS_ACCOUNT_ID{{"}}"}}
- If learning files have actual URL "https://example.com/repo/abc123" and variable name is "REPO_URL" → todo_final.md should have {{"{{"}}REPO_URL{{"}}"}}
- If source files have actual ID "xyz789" and variable name is "DEPLOYMENT_ID" → todo_final.md should have {{"{{"}}DEPLOYMENT_ID{{"}}"}}

{{end}}

## 📋 SYNTHESIS GUIDELINES
- **Read Original Plan and Learnings**: Review plan.md and all learning files to extract patterns and insights
- **Extract Success Patterns**: From learnings/success_patterns.md - capture what worked, which tools, which approaches
- **Extract Failure Patterns**: From learnings/failure_analysis.md - capture what failed, which tools to avoid, anti-patterns
- **Use Structured Format**: Each step must have: title, description, success_criteria, why_this_step, context_dependencies, context_output, success_patterns, failure_patterns
- **Be Specific**: Include exact MCP server, tool names, and successful approaches
- **Make It Executable**: Another LLM should be able to read this and execute without ambiguity

**Critical Structure Requirements**:
- **title**: Clear, concise step name
- **description**: Detailed what and how, including specific tools and approaches that worked
- **success_criteria**: Measurable completion criteria
- **why_this_step**: Explain purpose and value
- **context_dependencies**: What needs to be done before this step
- **context_output**: What this step produces for subsequent steps
- **success_patterns**: List of approaches/tools that WORKED (from learning reports)
- **failure_patterns**: List of approaches/tools that FAILED (from learning reports)

` + GetTodoCreationHumanMemoryRequirements() + `

## 📤 Output Format

**CREATE** {{.WorkspacePath}}/todo_final.md

**CRITICAL: Output format is JSON, saved in a .md file (not markdown!):**

The JSON structure must be:
{
  "steps": [
    {
      "title": "[Step title from plan]",
      "description": "[Detailed description including specific tools, MCP servers, commands, arguments that worked]",
      "success_criteria": "[How to verify completion]",
      "why_this_step": "[Purpose and value]",
      "context_dependencies": ["List of files from previous steps"],
      "context_output": "[What this step produces]",
      "success_patterns": ["Approach that worked", "Tool that succeeded"],
      "failure_patterns": ["Approach that failed", "Tool to avoid"]
    },
    {
      "title": "[Step 2]",
      "description": "...",
      "success_criteria": "...",
      "why_this_step": "...",
      "context_dependencies": ["step_1_results.md"],
      "context_output": "...",
      "success_patterns": [...],
      "failure_patterns": [...]
    }
  ]
}

**IMPORTANT FORMATTING RULES**:
- Output ONLY valid JSON - no markdown, no explanations
- Each step must include all 8 fields: title, description, success_criteria, why_this_step, context_dependencies, context_output, success_patterns, failure_patterns
- context_dependencies is an array of strings (file paths from previous steps)
- success_patterns and failure_patterns are arrays of strings
- If no dependencies/patterns exist, use empty arrays []
- Use proper JSON escaping for quotes and special characters
- The entire output must be valid, parseable JSON

**Extraction Guidelines**:
- Read planning/plan.md to get all steps with their structure
- Read learnings/success_patterns.md to extract successful approaches for success_patterns arrays
- Read learnings/failure_analysis.md to extract failed approaches for failure_patterns arrays
- Read learnings/step_*_learning.md for per-step specific learnings
- Each step must be converted to the JSON structure above

**Variable Masking (CRITICAL)**:
- Match actual values to variable names and replace with {{"{{"}}VARIABLE_NAME{{"}}"}} placeholders
- Use FUZZY MATCHING: If URL looks like https://github.com/user/repo → replace with {{"{{"}}GITHUB_REPO_URL{{"}}"}} even if not exact
- If ID looks like "account-123" → replace with {{"{{"}}AWS_ACCOUNT_ID{{"}}"}} even if slightly different
- If URL pattern similar to a variable → replace it (e.g., any github.com URL → {{"{{"}}GITHUB_REPO_URL{{"}}"}})
- Replace ANY value that matches a variable pattern, even if not 100% exact match

**File Path Handling**:
- All file paths must be RELATIVE (not absolute)
- Relative to workspace root: Use "docs/file.md" not "/workspace/docs/file.md"
- Use "config.json" not "/full/path/to/config.json"
- context_dependencies arrays should contain relative paths only
- context_output should be relative path only

**Return ONLY the JSON object, nothing else.**`

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
