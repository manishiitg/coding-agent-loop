package todo_creation_human

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"text/template"
	"time"

	"llm-providers/llmtypes"
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

// variablesFileMutex ensures thread-safe access to variables.json
var variablesFileMutex sync.Mutex

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

	// Generate system prompt using the appropriate processor (UPDATE mode if ExistingVariablesJSON is present)
	var systemPrompt string
	if templateVars["ExistingVariablesJSON"] != "" {
		systemPrompt = variableExtractionSystemPromptProcessorForUpdate(templateVars)
	} else {
		systemPrompt = variableExtractionSystemPromptProcessor(templateVars)
	}

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

// readVariablesFromFile reads variables.json from the workspace using BaseOrchestrator's ReadWorkspaceFile
func readVariablesFromFile(ctx context.Context, workspacePath string, readFile func(context.Context, string) (string, error)) (*VariablesManifest, error) {
	variablesPath := filepath.Join(workspacePath, "variables", "variables.json")

	variablesFileMutex.Lock()
	defer variablesFileMutex.Unlock()

	content, err := readFile(ctx, variablesPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read variables.json: %w", err)
	}

	var manifest VariablesManifest
	if err := json.Unmarshal([]byte(content), &manifest); err != nil {
		return nil, fmt.Errorf("failed to parse variables.json: %w", err)
	}

	return &manifest, nil
}

// writeVariablesToFile writes VariablesManifest to variables.json in the workspace using BaseOrchestrator's WriteWorkspaceFile
func writeVariablesToFile(ctx context.Context, workspacePath string, manifest *VariablesManifest, readFile func(context.Context, string) (string, error), writeFile func(context.Context, string, string) error, logger utils.ExtendedLogger) error {
	variablesPath := filepath.Join(workspacePath, "variables", "variables.json")

	variablesFileMutex.Lock()
	defer variablesFileMutex.Unlock()

	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal variables: %w", err)
	}

	if err := writeFile(ctx, variablesPath, string(data)); err != nil {
		return fmt.Errorf("failed to write variables.json: %w", err)
	}

	return nil
}

// getUpdateVariableSchema returns the JSON schema for update_variable tool
func getUpdateVariableSchema() string {
	return `{
		"type": "object",
		"properties": {
			"existing_variable_name": {
				"type": "string",
				"description": "Name of existing variable to update/delete (required for update/delete actions)"
			},
			"name": {
				"type": "string",
				"description": "Variable name in UPPER_SNAKE_CASE (required for add, optional for update)"
			},
			"value": {
				"type": "string",
				"description": "Variable value (optional)"
			},
			"description": {
				"type": "string",
				"description": "Variable description (optional)"
			},
			"action": {
				"type": "string",
				"enum": ["update", "add", "delete"],
				"description": "Action to perform: update existing, add new, or delete"
			}
		},
		"required": ["action"]
	}`
}

// getUpdateObjectiveSchema returns the JSON schema for update_objective tool
func getUpdateObjectiveSchema() string {
	return `{
		"type": "object",
		"properties": {
			"objective": {
				"type": "string",
				"description": "Updated templated objective with {{VARIABLE}} placeholders"
			}
		},
		"required": ["objective"]
	}`
}

// createUpdateVariableExecutor creates an executor function for update_variable tool
func createUpdateVariableExecutor(workspacePath string, logger utils.ExtendedLogger, readFile func(context.Context, string) (string, error), writeFile func(context.Context, string, string) error) func(context.Context, map[string]interface{}) (string, error) {
	return func(ctx context.Context, args map[string]interface{}) (string, error) {
		// Extract action
		actionRaw, ok := args["action"].(string)
		if !ok {
			return "", fmt.Errorf("invalid action argument")
		}
		action := actionRaw

		// Read current variables
		manifest, err := readVariablesFromFile(ctx, workspacePath, readFile)
		if err != nil {
			return "", fmt.Errorf("failed to read variables: %w", err)
		}

		switch action {
		case "add":
			// Extract new variable fields
			nameRaw, ok := args["name"].(string)
			if !ok || nameRaw == "" {
				return "", fmt.Errorf("name is required for add action")
			}
			name := nameRaw

			valueRaw, _ := args["value"].(string)
			value := valueRaw

			descriptionRaw, _ := args["description"].(string)
			description := descriptionRaw

			// Check if variable already exists
			for _, v := range manifest.Variables {
				if v.Name == name {
					return "", fmt.Errorf("variable %s already exists", name)
				}
			}

			// Add new variable
			newVar := Variable{
				Name:        name,
				Value:       value,
				Description: description,
			}
			manifest.Variables = append(manifest.Variables, newVar)
			logger.Infof("✅ Added new variable: %s", name)

		case "update":
			// Extract existing variable name
			existingNameRaw, ok := args["existing_variable_name"].(string)
			if !ok || existingNameRaw == "" {
				return "", fmt.Errorf("existing_variable_name is required for update action")
			}
			existingName := existingNameRaw

			// Find the variable to update
			found := false
			for i := range manifest.Variables {
				if manifest.Variables[i].Name == existingName {
					found = true
					// Update fields if provided
					if nameRaw, ok := args["name"].(string); ok && nameRaw != "" {
						// Check if new name conflicts with existing variable
						if nameRaw != existingName {
							for _, v := range manifest.Variables {
								if v.Name == nameRaw {
									return "", fmt.Errorf("variable %s already exists, cannot rename to it", nameRaw)
								}
							}
						}
						manifest.Variables[i].Name = nameRaw
					}
					if valueRaw, ok := args["value"].(string); ok {
						manifest.Variables[i].Value = valueRaw
					}
					if descriptionRaw, ok := args["description"].(string); ok {
						manifest.Variables[i].Description = descriptionRaw
					}
					logger.Infof("✅ Updated variable: %s", existingName)
					break
				}
			}
			if !found {
				return "", fmt.Errorf("variable %s not found", existingName)
			}

		case "delete":
			// Extract existing variable name
			existingNameRaw, ok := args["existing_variable_name"].(string)
			if !ok || existingNameRaw == "" {
				return "", fmt.Errorf("existing_variable_name is required for delete action")
			}
			existingName := existingNameRaw

			// Find and remove the variable
			found := false
			filtered := make([]Variable, 0, len(manifest.Variables))
			for _, v := range manifest.Variables {
				if v.Name == existingName {
					found = true
				} else {
					filtered = append(filtered, v)
				}
			}
			if !found {
				return "", fmt.Errorf("variable %s not found", existingName)
			}
			manifest.Variables = filtered
			logger.Infof("✅ Deleted variable: %s", existingName)

		default:
			return "", fmt.Errorf("invalid action: %s (must be 'add', 'update', or 'delete')", action)
		}

		// Preserve extraction_date
		if manifest.ExtractionDate == "" {
			manifest.ExtractionDate = time.Now().Format(time.RFC3339)
		}

		// Write updated variables
		if err := writeVariablesToFile(ctx, workspacePath, manifest, readFile, writeFile, logger); err != nil {
			return "", fmt.Errorf("failed to write variables: %w", err)
		}

		return fmt.Sprintf("Successfully performed %s action on variables", action), nil
	}
}

// createUpdateObjectiveExecutor creates an executor function for update_objective tool
func createUpdateObjectiveExecutor(workspacePath string, logger utils.ExtendedLogger, readFile func(context.Context, string) (string, error), writeFile func(context.Context, string, string) error) func(context.Context, map[string]interface{}) (string, error) {
	return func(ctx context.Context, args map[string]interface{}) (string, error) {
		// Extract objective
		objectiveRaw, ok := args["objective"].(string)
		if !ok || objectiveRaw == "" {
			return "", fmt.Errorf("invalid objective argument")
		}
		objective := objectiveRaw

		// Read current variables
		manifest, err := readVariablesFromFile(ctx, workspacePath, readFile)
		if err != nil {
			return "", fmt.Errorf("failed to read variables: %w", err)
		}

		// Update objective
		manifest.Objective = objective

		// Preserve extraction_date
		if manifest.ExtractionDate == "" {
			manifest.ExtractionDate = time.Now().Format(time.RFC3339)
		}

		// Write updated variables
		if err := writeVariablesToFile(ctx, workspacePath, manifest, readFile, writeFile, logger); err != nil {
			return "", fmt.Errorf("failed to write variables: %w", err)
		}

		logger.Infof("✅ Updated objective in variables.json")
		return "Successfully updated objective", nil
	}
}

// ExecuteStructuredUpdate executes the variable extraction agent in UPDATE mode using 2 custom tools that directly update variables.json
// readFile and writeFile are BaseOrchestrator's ReadWorkspaceFile and WriteWorkspaceFile methods
func (vea *VariableExtractionAgent) ExecuteStructuredUpdate(ctx context.Context, templateVars map[string]string, conversationHistory []llmtypes.MessageContent, userMessage string, readFile func(context.Context, string) (string, error), writeFile func(context.Context, string, string) error) (*VariablesManifest, []llmtypes.MessageContent, error) {
	// Get workspace path from template vars
	workspacePath := templateVars["WorkspacePath"]
	if workspacePath == "" {
		return nil, nil, fmt.Errorf("WorkspacePath not found in template vars")
	}

	// Get the underlying MCP agent
	baseAgent := vea.BaseOrchestratorAgent.BaseAgent()
	if baseAgent == nil {
		return nil, nil, fmt.Errorf("base agent is not initialized")
	}
	mcpAgent := baseAgent.Agent()
	if mcpAgent == nil {
		return nil, nil, fmt.Errorf("MCP agent is not initialized")
	}

	// Parse schemas and register the 2 custom tools
	updateVariableSchema := getUpdateVariableSchema()
	updateVariableParams, err := parseSchemaForToolParameters(updateVariableSchema)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to parse update_variable schema: %w", err)
	}

	updateObjectiveSchema := getUpdateObjectiveSchema()
	updateObjectiveParams, err := parseSchemaForToolParameters(updateObjectiveSchema)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to parse update_objective schema: %w", err)
	}

	// Get logger from MCP agent (it has a Logger field)
	logger := mcpAgent.Logger

	// Note: human_feedback tool is already registered via WorkspaceTools (which includes human tools)
	// No need to register it manually here

	// Register workflow-specific variable tools with "workflow" category
	if err := mcpAgent.RegisterCustomTool(
		"update_variable",
		"Update, add, or delete variables in variables.json. Provide action (required: 'update', 'add', or 'delete'), existing_variable_name (required for update/delete), and fields to update. The variables.json file is updated immediately when this tool is called.",
		updateVariableParams,
		createUpdateVariableExecutor(workspacePath, logger, readFile, writeFile),
		"workflow",
	); err != nil {
		logger.Errorf("❌ Failed to register update_variable tool: %v", err)
		return nil, nil, fmt.Errorf("failed to register update_variable tool: %w", err)
	}

	if err := mcpAgent.RegisterCustomTool(
		"update_objective",
		"Update the templated objective in variables.json. Provide the updated objective with {{VARIABLE}} placeholders. The variables.json file is updated immediately when this tool is called.",
		updateObjectiveParams,
		createUpdateObjectiveExecutor(workspacePath, logger, readFile, writeFile),
		"workflow",
	); err != nil {
		logger.Errorf("❌ Failed to register update_objective tool: %v", err)
		return nil, nil, fmt.Errorf("failed to register update_objective tool: %w", err)
	}

	// Generate system prompt for update mode
	systemPrompt := variableExtractionSystemPromptProcessorForUpdate(templateVars)

	// Execute the agent with normal Execute (not StructuredOutputViaTool)
	_, updatedHistory, err := baseAgent.Execute(ctx, userMessage, conversationHistory, systemPrompt, false)
	if err != nil {
		return nil, updatedHistory, fmt.Errorf("agent execution failed: %w", err)
	}

	// Check if any of our custom tools were called
	toolCalls := extractToolCallsFromMessages(updatedHistory)
	variableUpdateToolCalled := false
	for _, toolName := range toolCalls {
		if toolName == "update_variable" || toolName == "update_objective" {
			variableUpdateToolCalled = true
		}
	}

	// Read the current variables.json (whether tools were called or not)
	// In UPDATE mode, conversational responses are normal - not an error
	// If tools were called, variables.json was updated. If not, we return the current variables unchanged.
	currentVariables, err := readVariablesFromFile(ctx, workspacePath, readFile)
	if err != nil {
		return nil, updatedHistory, fmt.Errorf("failed to read variables: %w", err)
	}

	if !variableUpdateToolCalled {
		// No tools called - this is a normal conversational response, not an error
		// Return the current variables (unchanged) so conversation can continue
		logger.Infof("📝 Variable extraction agent in UPDATE mode: Conversational response (no variable changes). Returning current variables.")
		return currentVariables, updatedHistory, nil
	}

	// Tools were called - variables.json was updated
	logger.Infof("✅ Variables updated via tools (%d variables)", len(currentVariables.Variables))
	return currentVariables, updatedHistory, nil
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

// variableExtractionSystemPromptProcessorForUpdate generates system prompt for updating existing variables
func variableExtractionSystemPromptProcessorForUpdate(templateVars map[string]string) string {
	// Get current date and time
	now := time.Now()
	currentDate := now.Format("2006-01-02")
	currentTime := now.Format("15:04:05")

	templateData := struct {
		Objective             string
		WorkspacePath         string
		ExistingVariablesJSON string
		CurrentDate           string
		CurrentTime           string
	}{
		Objective:             templateVars["Objective"],
		WorkspacePath:         templateVars["WorkspacePath"],
		ExistingVariablesJSON: templateVars["ExistingVariablesJSON"],
		CurrentDate:           currentDate,
		CurrentTime:           currentTime,
	}

	templateStr := `## 📅 **CURRENT SESSION INFORMATION**
**Date**: {{.CurrentDate}}
**Time**: {{.CurrentTime}}

## 🤖 AGENT IDENTITY
- **Role**: Variable Extraction Agent (Update Mode)
- **Task**: Update existing variables based on human feedback
- **Tools**: Use human_feedback tool to confirm changes, then use update_variable and update_objective tools to modify variables.json. These tools update variables.json immediately when called.

## 🎯 OBJECTIVE

**PRIMARY OBJECTIVE**: {{.Objective}}

**WORKSPACE**: {{.WorkspacePath}}

## 📄 EXISTING VARIABLES

Update these variables based on human feedback. Use judgment to determine what changes address the feedback.

{{.ExistingVariablesJSON}}

## 🎯 UPDATE GUIDELINES

**Principles**:
- Interpret feedback and make logical changes (minor = targeted, substantial = comprehensive)
- Update related parts to maintain consistency
- Preserve variable placeholders ({{"{{"}}VARIABLE_NAME{{"}}"}}) exactly as-is in objective
- Keep same detail level in all variables

**Available Tools**:
- **human_feedback**: **REQUIRED BEFORE MAKING ANY VARIABLE CHANGES**. Use this tool to ask the user for confirmation before modifying variables. Provide a clear message describing the proposed changes (what variables will be updated/added/deleted and why). Wait for user approval before proceeding with variable modification tools. Generate a unique UUID for the unique_id parameter.
- **update_variable**: Update, add, or delete variables. Provide action (required: 'update', 'add', or 'delete'), existing_variable_name (required for update/delete), and fields to update. The variables.json file is updated immediately when this tool is called.
- **update_objective**: Update the templated objective. Provide the updated objective with {{"{{"}}VARIABLE{{"}}"}} placeholders. The variables.json file is updated immediately when this tool is called.

**CRITICAL WORKFLOW - HUMAN CONFIRMATION REQUIRED**:
1. **ALWAYS use human_feedback tool FIRST** before making any variable changes (update/add/delete variables or update objective)
2. In the human_feedback message, clearly describe:
   - What changes you plan to make (which variables to update/add/delete, or objective changes)
   - Why these changes address the user's feedback
   - The impact of these changes
3. The human_feedback tool will automatically return the user's response. **After receiving the response**:
   - If user approved: Immediately proceed with update_variable or update_objective tools in the same conversation turn
   - If user asked questions or needs clarification: Respond conversationally without calling variable update tools
   - If user rejected or requested changes: Adjust your approach and either ask again with human_feedback or respond conversationally
4. You can call multiple variable modification tools in the same turn after getting approval

**Guidelines**:
- You can call multiple variable modification tools in one turn after getting approval
- Tools update variables.json immediately - no merging needed
- Unchanged variables are preserved automatically
- A variable cannot be both updated and deleted

## 🔑 CRITICAL RULES

1. **Preserve Unchanged Variables**: Keep all existing variables that are not mentioned in feedback exactly as they are
2. **EXACT TEXT PRESERVATION**: The objective field MUST be the EXACT original text word-for-word, with ONLY hard-coded values replaced by {{"{{"}}VARIABLE_NAME{{"}}"}} placeholders
3. **Variable Name Consistency**: Preserve existing variable names unless feedback explicitly requests changes
4. **Use descriptive variable names** - UPPER_SNAKE_CASE, descriptive (or user-provided names)
5. **Provide clear descriptions** - what does this variable represent?

## 📤 OUTPUT REQUIREMENTS

**Workflow for variable changes**:
1. **First**: Use human_feedback tool to describe proposed changes and get user confirmation
2. **After human_feedback returns**: The tool automatically provides the user's response. Based on that response:
   - **If approved**: Immediately call update_variable or update_objective tools in the same conversation turn
   - **If questions/clarification needed**: Respond conversationally without calling variable update tools
   - **If rejected**: Adjust your approach and either ask again with human_feedback or respond conversationally
3. You can call multiple variable modification tools in the same turn after getting approval

**Respond conversationally when**: User asks questions, seeks clarification, or provides feedback that doesn't require variable changes. In this case, don't call any tools - just respond with text.

**IMPORTANT**: Never call update_variable or update_objective without first getting user confirmation via human_feedback tool. After human_feedback returns, you will automatically continue in the same turn and can make the variable changes.
`

	// Parse and execute the template
	tmpl, err := template.New("variable_extraction_system_prompt_update").Parse(templateStr)
	if err != nil {
		return fmt.Sprintf("Error parsing variable extraction UPDATE system prompt template: %v", err)
	}

	var result strings.Builder
	if err := tmpl.Execute(&result, templateData); err != nil {
		return fmt.Sprintf("Error executing variable extraction UPDATE system prompt template: %v", err)
	}

	return result.String()
}
