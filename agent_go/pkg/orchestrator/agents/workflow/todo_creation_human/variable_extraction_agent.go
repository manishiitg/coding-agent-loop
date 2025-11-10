package todo_creation_human

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"text/template"

	"mcp-agent/agent_go/internal/llmtypes"
	"mcp-agent/agent_go/internal/observability"
	"mcp-agent/agent_go/internal/utils"
	"mcp-agent/agent_go/pkg/mcpagent"
	"mcp-agent/agent_go/pkg/orchestrator/agents"
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

// ExecuteStructured executes the variable extraction agent and returns structured JSON output
// userMessage: The user message to send (e.g., "Extract variables..." for first attempt, or human feedback for revisions)
func (vea *VariableExtractionAgent) ExecuteStructured(ctx context.Context, templateVars map[string]string, conversationHistory []llmtypes.MessageContent, userMessage string) (*VariablesManifest, []llmtypes.MessageContent, error) {
	// Define the JSON schema for variable extraction
	schema := `{
		"type": "object",
		"properties": {
			"objective": {
				"type": "string",
				"description": "The EXACT original objective text provided by the user, with ONLY hard-coded values replaced by {{VARIABLE_NAME}} placeholders. Preserve all original wording, punctuation, structure, and formatting exactly as given."
			},
			"variables": {
				"type": "array",
				"items": {
					"type": "object",
					"properties": {
						"name": {
							"type": "string",
							"description": "Variable name in UPPER_SNAKE_CASE format (e.g., AWS_ACCOUNT_ID)"
						},
						"value": {
							"type": "string",
							"description": "Original hard-coded value from the objective"
						},
						"description": {
							"type": "string",
							"description": "Clear description of what this variable represents"
						}
					},
					"required": ["name", "value", "description"]
				}
			},
			"extraction_date": {
				"type": "string",
				"description": "ISO 8601 timestamp of when variables were extracted (e.g., 2025-01-27T14:30:25Z)"
			}
		},
		"required": ["objective", "variables", "extraction_date"]
	}`

	// Generate system prompt using the processor
	systemPrompt := variableExtractionSystemPromptProcessor(templateVars)

	// Create an input processor that returns the user message
	// In first attempt: userMessage is "Extract variables..."
	// In revision attempts: userMessage is human feedback
	inputProcessor := func(map[string]string) string {
		return userMessage
	}

	// Use ExecuteStructuredWithInputProcessorViaTool with generics
	toolName := "submit_variable_extraction_response"
	toolDescription := "Submit the final structured variable extraction response in JSON format. This tool should be called when you have completed variable extraction and are ready to provide the structured output."

	result, updatedHistory, err := agents.ExecuteStructuredWithInputProcessorViaTool[VariablesManifest](
		vea.BaseOrchestratorAgent,
		ctx,
		templateVars,
		inputProcessor,
		conversationHistory,
		schema,
		systemPrompt,
		false, // Don't overwrite system prompt, append to it
		toolName,
		toolDescription,
	)
	if err != nil {
		// Check if this is a non-structured response error (text response instead of structured output)
		// IMPORTANT: Return the error directly without wrapping, so the controller can detect it
		if agents.IsNonStructuredResponseError(err) {
			// Return the original NonStructuredResponseError with UpdatedHistory so the controller can handle it
			// Don't wrap it - wrapping breaks the error type check
			var nonStructuredErr *agents.NonStructuredResponseError
			if errors.As(err, &nonStructuredErr) {
				return nil, nonStructuredErr.UpdatedHistory, err
			}
			return nil, updatedHistory, err
		}
		return nil, nil, err
	}

	return &result, updatedHistory, nil
}

// Execute extracts variables from objective
// NOTE: This method is deprecated - use ExecuteStructured() instead for better type safety and reliability
func (vea *VariableExtractionAgent) Execute(ctx context.Context, templateVars map[string]string, conversationHistory []llmtypes.MessageContent) (string, []llmtypes.MessageContent, error) {
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
- **Directory**: {{.WorkspacePath}}/variables/
- **File**: variables.json
- **Full Path**: {{.WorkspacePath}}/variables/variables.json

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
4. Replace ONLY the hard-coded value in objective with {{"{{"}}VARIABLE_NAME{{"}}"}} - preserve ALL other text exactly

## 📝 OUTPUT FORMAT

**You MUST output STRUCTURED JSON:**

` + "```json" + `
{
  "objective": "Deploy the Spring Boot application to AWS account {{"{{"}}AWS_ACCOUNT_ID{{"}}"}} from GitHub repository {{"{{"}}GITHUB_REPO_URL{{"}}"}}",
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

**IMPORTANT**: The "objective" field above shows EXACT text preservation - notice "the Spring Boot application" and "from GitHub repository" are preserved exactly as in the original, with ONLY the values replaced.

## 📤 YOUR TASKS

**ALL YOUR DATA COMES FROM THE OBJECTIVE TEXT SHOWN ABOVE - DO NOT SEARCH FILES**

1. **Check if user explicitly mentioned variables** - if yes, extract those FIRST with their exact names
2. **Analyze the OBJECTIVE text above** - find all hard-coded values (URLs, IDs, credentials, resource names, etc.)
3. **For each hard-coded value**, create a variable (or use the user-provided variable name if they specified it)
4. **Create variable definitions** with name, value, description
5. **Generate templated objective** - use the EXACT original text, replacing ONLY hard-coded values with {{"{{"}}VARIABLE_NAME{{"}}"}} placeholders
6. **Create JSON file** at {{.WorkspacePath}}/variables/variables.json (create directory if needed)
7. **Output the complete JSON** in your response so the orchestrator can parse it

## 🔑 CRITICAL RULES

1. **User-mentioned variables take PRIORITY** - if user explicitly lists variables, use those FIRST with exact names and values
2. **Every hard-coded VALUE** must become a variable (or skip if already covered by user-mentioned variables)
3. **EXACT TEXT PRESERVATION** - The objective field MUST be the EXACT original text word-for-word, with ONLY hard-coded values replaced by {{"{{"}}VARIABLE_NAME{{"}}"}} placeholders. Preserve:
   - All original wording exactly as written
   - All punctuation and formatting
   - All sentence structure and order
   - All capitalization
   - All spacing and line breaks
   - Everything else unchanged
4. **Use descriptive variable names** - UPPER_SNAKE_CASE, descriptive (or user-provided names)
5. **Provide clear descriptions** - what does this variable represent?
6. **Write JSON to**: {{.WorkspacePath}}/variables/variables.json ONLY
7. **DO NOT** search the entire workspace or create files elsewhere
8. **DO NOT** rephrase, summarize, or modify the objective text - only replace values with placeholders

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

// variableExtractionSystemPromptProcessor generates the system prompt for variable extraction
func variableExtractionSystemPromptProcessor(templateVars map[string]string) string {
	templateData := struct {
		Objective     string
		WorkspacePath string
	}{
		Objective:     templateVars["Objective"],
		WorkspacePath: templateVars["WorkspacePath"],
	}

	templateStr := `## 🤖 AGENT IDENTITY
- **Role**: Variable Extraction Agent
- **Responsibility**: Identify hard-coded values in objective and convert them to reusable variables
- **Output Format**: Structured JSON via submit_variable_extraction_response tool (not markdown, not files)

## 🎯 PRIMARY TASK - EXTRACT VARIABLES FROM OBJECTIVE

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
4. Replace ONLY the hard-coded value in objective with {{"{{"}}VARIABLE_NAME{{"}}"}} - preserve ALL other text exactly

## 🔑 CRITICAL RULES

1. **User-mentioned variables take PRIORITY** - if user explicitly lists variables, use those FIRST with exact names and values
2. **Every hard-coded VALUE** must become a variable (or skip if already covered by user-mentioned variables)
3. **EXACT TEXT PRESERVATION** - The objective field MUST be the EXACT original text word-for-word, with ONLY hard-coded values replaced by {{"{{"}}VARIABLE_NAME{{"}}"}} placeholders. Preserve:
   - All original wording exactly as written
   - All punctuation and formatting
   - All sentence structure and order
   - All capitalization
   - All spacing and line breaks
   - Everything else unchanged
4. **Use descriptive variable names** - UPPER_SNAKE_CASE, descriptive (or user-provided names)
5. **Provide clear descriptions** - what does this variable represent?
6. **DO NOT** search the entire workspace or create files - use structured output tool instead
7. **DO NOT** rephrase, summarize, or modify the objective text - only replace values with placeholders

` + GetTodoCreationHumanMemoryRequirements() + `

## 📤 OUTPUT REQUIREMENTS

**CRITICAL**: 
- Call submit_variable_extraction_response tool with structured JSON data when extraction is complete
- Do NOT read/write files, include markdown formatting, or output JSON in text - just call the tool with structured data
- The tool expects a JSON object with: objective (string), variables (array), extraction_date (ISO 8601 string)

## 📝 EXAMPLE - EXACT TEXT PRESERVATION

**Original Objective:**
"Deploy the Spring Boot application to AWS account 123456789012 in region us-east-1. The app should connect to database mydb-prod on port 3306."

**Correct Output (EXACT preservation with only values replaced):**
` + "```json" + `
{
  "objective": "Deploy the Spring Boot application to AWS account {{"{{"}}AWS_ACCOUNT_ID{{"}}"}} in region {{"{{"}}AWS_REGION{{"}}"}}. The app should connect to database {{"{{"}}DATABASE_NAME{{"}}"}} on port {{"{{"}}DATABASE_PORT{{"}}"}}.",
  "variables": [
    {
      "name": "AWS_ACCOUNT_ID",
      "value": "123456789012",
      "description": "AWS account number for deployment"
    },
    {
      "name": "AWS_REGION",
      "value": "us-east-1",
      "description": "AWS region for deployment"
    },
    {
      "name": "DATABASE_NAME",
      "value": "mydb-prod",
      "description": "Database name to connect to"
    },
    {
      "name": "DATABASE_PORT",
      "value": "3306",
      "description": "Database port number"
    }
  ],
  "extraction_date": "2025-01-27T14:30:25Z"
}
` + "```" + `

**WRONG (rephrased or modified):**
- "Deploy Spring Boot app to AWS account {{"{{"}}AWS_ACCOUNT_ID{{"}}"}}..." (changed "the Spring Boot application" to "Spring Boot app")
- "Deploy the Spring Boot application to AWS account {{"{{"}}AWS_ACCOUNT_ID{{"}}"}} in the {{"{{"}}AWS_REGION{{"}}"}} region..." (added "the" before region)

**Remember**: The objective field must be IDENTICAL to the original, with ONLY values replaced by placeholders.
`

	// Parse and execute the template
	tmpl, err := template.New("variable_extraction_system_prompt").Parse(templateStr)
	if err != nil {
		return fmt.Sprintf("Error parsing variable extraction system prompt template: %v", err)
	}

	var result strings.Builder
	if err := tmpl.Execute(&result, templateData); err != nil {
		return fmt.Sprintf("Error executing variable extraction system prompt template: %v", err)
	}

	return result.String()
}
