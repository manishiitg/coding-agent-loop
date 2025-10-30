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

// Variable represents a single variable definition
type Variable struct {
	Name        string `json:"name"`        // e.g., "AWS_ACCOUNT_ID"
	Value       string `json:"value"`       // Original value from objective
	Description string `json:"description"` // e.g., "AWS account number for deployment"
}

// VariablesManifest contains all extracted variables
type VariablesManifest struct {
	Objective      string     `json:"objective"` // Templated objective with {{VARS}}
	Variables      []Variable `json:"variables"` // List of variables
	ExtractionDate string     `json:"extraction_date"`
}

// VariableExtractionAgent extracts variables from objective
type VariableExtractionAgent struct {
	*agents.BaseOrchestratorAgent
}

// NewVariableExtractionAgent creates a new variable extraction agent
func NewVariableExtractionAgent(
	config *agents.OrchestratorAgentConfig,
	logger utils.ExtendedLogger,
	tracer observability.Tracer,
	eventBridge mcpagent.AgentEventListener,
) *VariableExtractionAgent {
	baseAgent := agents.NewBaseOrchestratorAgentWithEventBridge(
		config,
		logger,
		tracer,
		agents.VariableExtractionAgentType,
		eventBridge,
	)

	return &VariableExtractionAgent{
		BaseOrchestratorAgent: baseAgent,
	}
}

// Execute extracts variables from objective
func (vea *VariableExtractionAgent) Execute(ctx context.Context, templateVars map[string]string, conversationHistory []llms.MessageContent) (string, []llms.MessageContent, error) {
	return vea.ExecuteWithInputProcessor(ctx, templateVars, vea.variableExtractionInputProcessor, conversationHistory)
}

// variableExtractionInputProcessor creates the prompt for variable extraction
func (vea *VariableExtractionAgent) variableExtractionInputProcessor(templateVars map[string]string) string {
	templateData := struct {
		Objective     string
		WorkspacePath string
	}{
		Objective:     templateVars["Objective"],
		WorkspacePath: templateVars["WorkspacePath"],
	}

	templateStr := `## 🎯 PRIMARY TASK - EXTRACT VARIABLES FROM OBJECTIVE

**YOUR INPUT - THE OBJECTIVE TO ANALYZE:**
{{.Objective}}

**WORKSPACE**: {{.WorkspacePath}}

## 🎯 YOUR JOB - READ CAREFULLY

**Extract variables from the OBJECTIVE TEXT shown above.**

**Process:**
1. Look at the OBJECTIVE text above - that is your ONLY data source
2. **PRIORITY**: If the user explicitly mentions variables (e.g., "variables:", "AWS_ACCOUNT_ID=123", lists variables), extract those FIRST and use them as-is
3. Find hard-coded values in that objective text (URLs, account IDs, passwords, etc.) and convert them to variables
4. DO NOT search the workspace - only use the objective text above

## 📂 VARIABLES DIRECTORY
**IMPORTANT**: Variables should be saved to:
- **Directory**: {{.WorkspacePath}}/todo_creation_human/variables/
- **File**: variables.json
- **Full Path**: {{.WorkspacePath}}/todo_creation_human/variables/variables.json

**Note**: If variables.json already exists at this path, the orchestrator will check for it before calling you. You are responsible for creating this file with your extracted variables.

## 🤖 AGENT IDENTITY
- **Role**: Variable Extraction Agent
- **Responsibility**: Identify hard-coded values in objective and convert them to reusable variables

## 📋 WHAT TO EXTRACT

**PRIORITY - User-Mentioned Variables:**
- If the user explicitly mentions variables (e.g., "variables:", "AWS_ACCOUNT_ID=123", variable lists), extract those FIRST with their exact names and values

**Extract These Types of Values:**
- URLs (https://github.com/user/repo), account IDs (123456789), ports (3306)
- Credentials (passwords, API keys), resource names (mydb-prod, s3-bucket)
- Environment values (us-east-1, production), hosts/endpoints
- Specific identifiers, paths, configurations

**DO NOT Extract:**
- Generic terms (repository, database, account - these are descriptive)
- Action words (deploy, configure, setup)
- Technology names (Spring Boot, React, PostgreSQL)

**For Each Value:**
1. Generate UPPER_SNAKE_CASE variable name
2. Keep original value
3. Add description of what it represents
4. Replace in objective with {{"{{"}}VARIABLE_NAME{{"}}"}}

## 📝 OUTPUT FORMAT

**You MUST output STRUCTURED JSON:**

` + "```json" + `
{
  "objective": "Deploy Spring Boot app to AWS account {{"{{"}}AWS_ACCOUNT_ID{{"}}"}} from GitHub {{"{{"}}GITHUB_REPO_URL{{"}}"}}",
  "variables": [
    {
      "name": "AWS_ACCOUNT_ID",
      "value": "123456789012",
      "description": "AWS account number for deployment target"
    },
    {
      "name": "GITHUB_REPO_URL",
      "value": "https://github.com/user/repo",
      "description": "GitHub repository URL to clone"
    }
  ],
  "extraction_date": "2025-01-27T14:30:25Z"
}
` + "```" + `

## 📤 YOUR TASKS

**ALL YOUR DATA COMES FROM THE OBJECTIVE TEXT SHOWN ABOVE - DO NOT SEARCH FILES**

1. **Check if user explicitly mentioned variables** - if yes, extract those FIRST with their exact names
2. **Analyze the OBJECTIVE text above** - find all hard-coded values (URLs, IDs, credentials, resource names, etc.)
3. **For each hard-coded value**, create a variable (or use the user-provided variable name if they specified it)
4. **Create variable definitions** with name, value, description
5. **Generate templated objective** with {{"{{"}}VARIABLES{{"}}"}} replacing the original values
6. **Create JSON file** at {{.WorkspacePath}}/todo_creation_human/variables/variables.json (create directory if needed)
7. **Output the complete JSON** in your response so the orchestrator can parse it

## 🔑 CRITICAL RULES

1. **User-mentioned variables take PRIORITY** - if user explicitly lists variables, use those FIRST with exact names and values
2. **Every hard-coded VALUE** must become a variable (or skip if already covered by user-mentioned variables)
3. **Preserve the objective structure** - only replace values with {{"{{"}}VARS{{"}}"}}
4. **Use descriptive variable names** - UPPER_SNAKE_CASE, descriptive (or user-provided names)
5. **Provide clear descriptions** - what does this variable represent?
6. **Write JSON to**: {{.WorkspacePath}}/todo_creation_human/variables/variables.json ONLY
7. **DO NOT** search the entire workspace or create files elsewhere

` + GetTodoCreationHumanMemoryRequirements() + `
`

	// Parse and execute the template
	tmpl, err := template.New("variable_extraction").Parse(templateStr)
	if err != nil {
		return fmt.Sprintf("Error parsing template: %v", err)
	}

	var result strings.Builder
	if err := tmpl.Execute(&result, templateData); err != nil {
		return fmt.Sprintf("Error executing template: %v", err)
	}

	return result.String()
}
