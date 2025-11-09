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

// HumanControlledLearningIntegrationAgent reads plan.json and learnings files, then enhances plan with success/failure patterns
// This agent does NOT write files - it only returns enhanced JSON
type HumanControlledLearningIntegrationAgent struct {
	*agents.BaseOrchestratorAgent
}

// NewHumanControlledLearningIntegrationAgent creates a new learning integration agent
func NewHumanControlledLearningIntegrationAgent(config *agents.OrchestratorAgentConfig, logger utils.ExtendedLogger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) *HumanControlledLearningIntegrationAgent {
	baseAgent := agents.NewBaseOrchestratorAgentWithEventBridge(
		config,
		logger,
		tracer,
		agents.PlanReaderAgentType, // Reuse plan reader type for now
		eventBridge,
	)

	return &HumanControlledLearningIntegrationAgent{
		BaseOrchestratorAgent: baseAgent,
	}
}

// LearningPatternStep represents only the patterns for a step (title + patterns only)
type LearningPatternStep struct {
	Title           string   `json:"title"`
	SuccessPatterns []string `json:"success_patterns,omitempty"`
	FailurePatterns []string `json:"failure_patterns,omitempty"`
}

// LearningPatternResponse represents the response with only step titles and patterns
type LearningPatternResponse struct {
	Steps []LearningPatternStep `json:"steps"`
}

// ExecuteStructured executes the learning integration agent and returns only step titles and patterns
func (hclia *HumanControlledLearningIntegrationAgent) ExecuteStructured(ctx context.Context, templateVars map[string]string, conversationHistory []llmtypes.MessageContent) (*LearningPatternResponse, []llmtypes.MessageContent, error) {
	// Extract learning detail level to customize schema descriptions
	learningDetailLevel := templateVars["LearningDetailLevel"]
	if learningDetailLevel == "" {
		learningDetailLevel = "general"
	}

	// Define pattern descriptions based on learning detail level
	var successPatternDesc, failurePatternDesc string
	if learningDetailLevel == "exact" {
		successPatternDesc = `CRITICAL: In EXACT mode, extract COMPLETE patterns with ALL details from learning files. COMPREHENSIVE EXTRACTION REQUIRED - DO NOT MISS ANY PATTERNS. Each pattern string must include: (1) Narrative context describing what was accomplished, (2) Explanations for why each tool call was effective, (3) Complete tool calls with FULL argument JSON. DO NOT simplify or summarize - preserve the full detail from the learning file. Extract ALL success patterns mentioned in the learning file - do not skip any. Review the entire learning file content and extract every successful tool call, every strategy that worked, every best practice mentioned. Be exhaustive and thorough - completeness is critical. Example format: "When elements are nested inside an <iframe>, browser_evaluate provides a reliable way to gain access and control them. This is the key success pattern. Setting the input value: The browser_evaluate tool was used to access iframe content. tool_name: browser.browser_evaluate, arguments: {"function":"() => { const iframe = document.querySelector('frame[name=\\"login_page\\"]'); const input = iframe.contentDocument.querySelector('input[name=\\"fldLoginUserId\\"]'); input.value = '{{USER_ID}}'; }"}. Verifying state: The browser_snapshot tool confirmed the page state after navigation. tool_name: browser.browser_snapshot, arguments: {}"`
		failurePatternDesc = `CRITICAL: In EXACT mode, extract COMPLETE patterns with ALL details from learning files. COMPREHENSIVE EXTRACTION REQUIRED - DO NOT MISS ANY PATTERNS. Each pattern string must include: (1) Narrative context describing what went wrong, (2) Explanations for why each tool call failed, (3) Complete tool calls with FULL argument JSON, (4) Reason for failure. DO NOT simplify or summarize - preserve the full detail from the learning file. Extract ALL failure patterns mentioned in the learning file - do not skip any. Review the entire learning file content and extract every failed tool call, every anti-pattern, every warning mentioned. Be exhaustive and thorough - completeness is critical. Example format: "Using standard interaction tools on elements within an iframe is an anti-pattern. These tools will fail because the elements are not in the main document tree. Using browser_type on iframe element: The browser_type tool failed because it cannot access iframe content. tool_name: browser.browser_type, arguments: {"element":"User ID / Customer ID textbox","ref":"textbox_with_name_fldLoginUserId","text":"{{USER_ID}}"}. Reason: Elements inside iframe are not accessible via standard tools - use browser_evaluate instead"`
	} else {
		successPatternDesc = "List of high-level MCP server tool patterns that successfully achieved the step description. Each pattern should describe tool names (server_name.tool_name) without arguments and general strategies/principles that led to success. Format: server_name.tool_name - general approach or principle. Extract from step_X_learning.md files and match to appropriate steps based on which tools worked to accomplish the step goal."
		failurePatternDesc = "List of high-level MCP server tool patterns that failed to achieve the step description or should be avoided. Each pattern should describe tool names (server_name.tool_name) without arguments and anti-patterns/what to avoid. Format: server_name.tool_name - anti-pattern or mistake. Extract from step_X_learning.md files and match to appropriate steps based on which tools didn't work for the step goal."
	}

	// Define the JSON schema - only title and patterns (no other plan fields)
	schema := fmt.Sprintf(`{
		"type": "object",
		"properties": {
			"steps": {
				"type": "array",
				"items": {
					"type": "object",
					"properties": {
						"title": {
							"type": "string",
							"description": "Step title to match with existing plan steps"
						},
						"success_patterns": {
							"type": "array",
							"items": {
								"type": "string"
							},
							"description": %q
						},
						"failure_patterns": {
							"type": "array",
							"items": {
								"type": "string"
							},
							"description": %q
						}
					},
					"required": ["title"]
				}
			}
		},
		"required": ["steps"]
	}`, successPatternDesc, failurePatternDesc)

	// Generate system prompt and user message separately
	systemPrompt := hclia.learningIntegrationSystemPromptProcessor(templateVars)
	userMessage := hclia.learningIntegrationUserMessageProcessor(templateVars)

	// Create a simple input processor that returns the user message
	inputProcessor := func(map[string]string) string {
		return userMessage
	}

	// Use the base orchestrator agent's ExecuteStructured method with custom system prompt (overwrite=true)
	result, updatedHistory, err := agents.ExecuteStructuredWithInputProcessor[LearningPatternResponse](hclia.BaseOrchestratorAgent, ctx, templateVars, inputProcessor, conversationHistory, schema, systemPrompt, true)
	if err != nil {
		return nil, nil, err
	}

	return &result, updatedHistory, nil
}

// Execute implements the OrchestratorAgent interface
// NOTE: This method is NOT USED - use ExecuteStructured() instead
func (hclia *HumanControlledLearningIntegrationAgent) Execute(ctx context.Context, templateVars map[string]string, conversationHistory []llmtypes.MessageContent) (string, []llmtypes.MessageContent, error) {
	return "", nil, fmt.Errorf("Execute() is not used for learning integration agent - use ExecuteStructured() instead")
}

// learningIntegrationSystemPromptProcessor creates the system prompt for learning integration
func (hclia *HumanControlledLearningIntegrationAgent) learningIntegrationSystemPromptProcessor(templateVars map[string]string) string {
	// Extract learning detail level (default to "general" if not provided)
	learningDetailLevel := templateVars["LearningDetailLevel"]
	if learningDetailLevel == "" {
		learningDetailLevel = "general"
	}

	// Create template data
	templateData := map[string]string{
		"Objective":           templateVars["Objective"],
		"WorkspacePath":       templateVars["WorkspacePath"],
		"VariableNames":       templateVars["VariableNames"], // Optional - may be empty if no variables
		"LearningDetailLevel": learningDetailLevel,           // Learning detail level preference
	}

	// Define the system prompt template
	templateStr := `# Learning Integration Agent

## 🤖 AGENT IDENTITY
- **Role**: Learning Integration Agent
- **Responsibility**: Enhance existing plan.json with success/failure patterns from learnings files
- **Input**: plan.json + learnings/ folder files
- **Output**: Enhanced plan.json with patterns added
- **NO FILE WRITING**: This agent does NOT write files - only returns enhanced JSON
- **READ ONLY**: This agent reads files but does NOT write any files

## 🎚️ **LEARNING DETAIL LEVEL**

{{if eq .LearningDetailLevel "exact"}}
**MODE: EXACT MCP TOOLS**

You are operating in **EXACT MCP TOOLS** mode. This means:
- Extract **narrative context** describing what the execution accomplished (e.g., "The execution successfully entered the password and navigated to the OTP screen")
- Include **explanations** for why each tool call was effective or failed
- Extract **complete tool calls with full argument JSON** from learning files
- Capture **exact MCP tool invocations** with structured format showing tool_name and arguments
- Include **precise commands and arguments** that led to success or failure

**🚨 CRITICAL: COMPREHENSIVE EXTRACTION REQUIRED**

**DO NOT MISS ANY PATTERNS** - In EXACT mode, you MUST:
- **Extract ALL success patterns** from each learning file - do not skip any successful tool calls or strategies
- **Extract ALL failure patterns** from each learning file - do not skip any failed attempts or anti-patterns
- **Review the ENTIRE learning file content** - read through every section, every tool call, every explanation
- **Capture EVERY tool invocation** - include all tool calls mentioned in the learning file, not just a summary
- **Preserve ALL context** - include narrative descriptions, explanations, and reasoning for each pattern
- **Extract patterns from ALL sections** - success patterns, failure patterns, lessons learned, best practices, warnings, etc.
- **Do NOT summarize or condense** - extract the full detailed pattern as written in the learning file
- **Do NOT skip patterns** - if a learning file mentions 10 tool calls, extract all 10, not just 3-4
- **Be thorough and exhaustive** - completeness is more important than brevity in EXACT mode

**Example Pattern Format (Detailed with Context)**:
- Success: "The execution successfully entered the password and navigated to the OTP screen. The following tool calls were effective: Inspecting the page: The initial browser_snapshot call was crucial for identifying the correct elements for interaction. tool_name: browser_snapshot, arguments: {}. Typing the password: The browser_type tool was used with a specific element reference (e33) to accurately fill the password field. tool_name: browser_type, arguments: {\"element\":\"Enter Password\",\"ref\":\"e33\",\"text\":\"{{"{{"}}PASSWORD{{"}}"}}\"}. Clicking the login button: The browser_click tool successfully submitted the form. tool_name: browser_click, arguments: {\"element\":\"Login\",\"ref\":\"e42\"}"

- Failure: "The execution failed to authenticate due to incorrect API usage. The following tool calls caused issues: Using deprecated endpoint: The old_api_call tool failed because the endpoint was deprecated. tool_name: old_api_call, arguments: {\"endpoint\":\"/v1/auth\",\"method\":\"POST\"}. Reason: Deprecated API endpoint - should use /v2/auth instead"
{{else}}
**MODE: GENERAL PATTERNS**

You are operating in **GENERAL PATTERNS** mode. This means:
- Extract **high-level approaches, strategies, and workflow patterns** from learning files
- Capture **tool names only** (without arguments): server_name.tool_name
- Document **general principles and best practices** rather than exact commands
- Focus on **what worked** and **what to avoid** at a conceptual level

**Example Pattern Format**:
- Success: "aws.ec2_create_instance - verify instance type and security groups before creation"
- Failure: "aws.ec2_run_instances - deprecated API, use ec2_create_instance instead"
{{end}}

## 📋 TASK

**CRITICAL: FILE DISCOVERY IS REQUIRED**

1. **FIRST**: Use list_workspace_files tool to discover ALL learning files in {{.WorkspacePath}}/learnings/ directory
   - Call: list_workspace_files with folder: "{{.WorkspacePath}}/learnings" and max_depth: 1
   - This will return a list of all files matching step_*_learning.md pattern
   - **DO NOT skip this step** - you MUST discover files before reading them
   - **DO NOT guess file names** - always use list_workspace_files first

2. **THEN**: Read EACH discovered step_*_learning.md file using read_workspace_file tool
   - For each file found in step 1, call read_workspace_file with the full file path
   - Read ALL discovered files - do not skip any files
   - Process files in order: step_1_learning.md, step_2_learning.md, etc.

3. Extract success and failure patterns from each learning file (preserve full detail in EXACT mode, summarize in GENERAL mode)

4. Match patterns to steps by title (from plan provided in user message)

5. Return JSON with ONLY: title, success_patterns, failure_patterns

6. OMIT steps with no patterns (both arrays empty)

{{if .VariableNames}}
## 🔑 VARIABLE MASKING REQUIREMENT

**IMPORTANT**: plan.json and learning files may contain ACTUAL VALUES (like real account IDs, URLs, etc.)

Available variables to mask:
{{.VariableNames}}

**CRITICAL INSTRUCTIONS**:
- **REPLACE** any actual values from ALL sources (plan.json, learning files) with the corresponding {{"{{"}}VARIABLE{{"}}"}} placeholder
- **PRESERVE** any {{"{{"}}VARIABLE{{"}}"}} placeholders already in plan.json
- The final output must use ONLY placeholders like {{"{{"}}VARIABLE_NAME{{"}}"}}, NEVER actual values
- Match actual values to their variable names and replace with the appropriate {{"{{"}}VARIABLE_NAME{{"}}"}} placeholder
- Apply masking to all content: title, description, success_criteria, context_dependencies, context_output, loop_condition, loop_description, success_patterns, failure_patterns
- Use FUZZY MATCHING: If URL looks like https://github.com/user/repo → replace with {{"{{"}}GITHUB_REPO_URL{{"}}"}} even if not exact
- If ID looks like "account-123" → replace with {{"{{"}}AWS_ACCOUNT_ID{{"}}"}} even if slightly different
- If URL pattern similar to a variable → replace it (e.g., any github.com URL → {{"{{"}}GITHUB_REPO_URL{{"}}"}})
- Replace ANY value that matches a variable pattern, even if not 100% exact match

**Examples**:
- If plan.json has actual value "account-123456" and variable name is "AWS_ACCOUNT_ID" → output should have {{"{{"}}AWS_ACCOUNT_ID{{"}}"}}
- If learning files have actual URL "https://example.com/repo/abc123" and variable name is "REPO_URL" → output should have {{"{{"}}REPO_URL{{"}}"}}
- If source files have actual ID "xyz789" and variable name is "DEPLOYMENT_ID" → output should have {{"{{"}}DEPLOYMENT_ID{{"}}"}}

{{end}}

## 📚 EXTRACTION RULES

{{if eq .LearningDetailLevel "exact"}}
**EXACT MODE - Preserve ALL details - COMPREHENSIVE EXTRACTION REQUIRED**:

**CRITICAL INSTRUCTIONS FOR COMPREHENSIVE EXTRACTION**:
- **Extract COMPLETE patterns**: narrative context + explanations + full tool calls with complete JSON arguments
- **DO NOT simplify** - copy full pattern text from learning files
- **DO NOT skip patterns** - extract ALL patterns mentioned in the learning file
- **Review entire file** - read through ALL sections of each learning file before extracting patterns
- **Extract from all sections** - success patterns, failure patterns, lessons learned, best practices, warnings, notes, etc.
- **Capture every tool call** - if a learning file mentions 5 tool calls, extract all 5 with full details
- **Preserve all context** - include why each tool was used, what it accomplished, and any relevant details
- **Be exhaustive** - when in doubt, include the pattern rather than exclude it
- **Check for completeness** - after extraction, verify you haven't missed any patterns from the learning file

**Pattern Extraction Checklist** (verify you've extracted):
- [ ] All successful tool calls with full arguments
- [ ] All failed tool calls with full arguments and failure reasons
- [ ] All narrative context explaining what worked and why
- [ ] All explanations for why tools succeeded or failed
- [ ] All best practices mentioned
- [ ] All warnings or anti-patterns mentioned
- [ ] All lessons learned
- [ ] All workflow sequences that led to success

**Example**: "When elements are nested inside an <iframe>, browser_evaluate provides reliable access. Setting input: tool_name: browser.browser_evaluate, arguments: {\"function\":\"() => { ... }\"}"
{{else}}
**GENERAL MODE - High-level patterns**:
- Extract tool names (server_name.tool_name) and general strategies
- Example: "browser.browser_evaluate - Use for iframe interaction"
{{end}}

**Matching**: Match by step number (step_X_learning.md → step X) and step title/description relevance.

## 🎯 PATTERN MATCHING

- Match by step number (step_X_learning.md → step X) and step title/description
- Focus on MCP server tools (server_name.tool_name) that achieved or failed the step goal
- Ignore workspace management tools (write_workspace_file, read_workspace_file, list_workspace_files, etc.)

## 🚨 CRITICAL: FILE DISCOVERY WORKFLOW

**REQUIRED STEPS (DO NOT SKIP)**:

1. **MUST use list_workspace_files first**:
   - Tool: list_workspace_files
   - Arguments: {"folder": "{{.WorkspacePath}}/learnings", "max_depth": 1}

2. **THEN read each discovered file**:
   - Tool: read_workspace_file
   - Arguments: {"file_path": "{{.WorkspacePath}}/learnings/step_1_learning.md"}
   - Tool: read_workspace_file
   - Arguments: {"file_path": "{{.WorkspacePath}}/learnings/step_2_learning.md"}
   - ... (for each file found)

3. **Process ALL files** - do not skip any discovered learning files

## 📤 OUTPUT

Return JSON with ONLY: title, success_patterns, failure_patterns. OMIT steps with no patterns.

**Example**:
{{if eq .LearningDetailLevel "exact"}}
` + "```json" + `
{
  "steps": [
    {
      "title": "Deploy application",
      "success_patterns": [
        "The execution successfully created the EC2 instance. Creating instance: tool_name: aws.ec2_create_instance, arguments: {\"instance_type\":\"t3.medium\",\"image_id\":\"ami-123\"}"
      ],
      "failure_patterns": [
        "Using deprecated API: tool_name: aws.ec2_run_instances, arguments: {\"image_id\":\"ami-123\"}. Reason: Deprecated - use ec2_create_instance instead"
      ]
    }
  ]
}
` + "```" + `
{{else}}
` + "```json" + `
{
  "steps": [
    {
      "title": "Deploy application",
      "success_patterns": [
        "aws.ec2_create_instance - verify instance type before creation"
      ],
      "failure_patterns": [
        "aws.ec2_run_instances - deprecated API, use ec2_create_instance instead"
      ]
    }
  ]
}
` + "```" + `
{{end}}`

	// Parse and execute the template
	tmpl, err := template.New("learning_integration_system").Parse(templateStr)
	if err != nil {
		return fmt.Sprintf("Error parsing learning integration system prompt template: %v", err)
	}

	var result strings.Builder
	if err := tmpl.Execute(&result, templateData); err != nil {
		return fmt.Sprintf("Error executing learning integration system prompt template: %v", err)
	}

	return result.String()
}

// learningIntegrationUserMessageProcessor creates the user message for learning integration
func (hclia *HumanControlledLearningIntegrationAgent) learningIntegrationUserMessageProcessor(templateVars map[string]string) string {
	// Extract learning detail level (default to "general" if not provided)
	learningDetailLevel := templateVars["LearningDetailLevel"]
	if learningDetailLevel == "" {
		learningDetailLevel = "general"
	}

	// Create template data
	templateData := map[string]string{
		"Objective":           templateVars["Objective"],
		"WorkspacePath":       templateVars["WorkspacePath"],
		"ExistingPlanJSON":    templateVars["ExistingPlanJSON"], // Current plan.json content
		"VariableNames":       templateVars["VariableNames"],    // Optional - may be empty if no variables
		"LearningDetailLevel": learningDetailLevel,              // Learning detail level preference
	}

	// Define the user message template
	templateStr := `# Learning Integration Task

**OBJECTIVE**: {{.Objective}}
**WORKSPACE**: {{.WorkspacePath}}

## 📊 CURRENT PLAN JSON

{{.ExistingPlanJSON}}

## 📚 TASK

**CRITICAL: You MUST discover files before reading them**

1. **FIRST**: Use list_workspace_files tool to discover all learning files in {{.WorkspacePath}}/learnings/ directory
   - Call: list_workspace_files with folder: "{{.WorkspacePath}}/learnings" and max_depth: 1
   - This will show you which step_*_learning.md files actually exist
   - **DO NOT skip this step** - you MUST discover files before reading them

2. **THEN**: Read EACH discovered step_*_learning.md file using read_workspace_file tool
   - For each file found in step 1, call read_workspace_file with the full file path
   - Read ALL discovered files - do not skip any files

3. Extract patterns from each file and match to steps by title (from plan above)
   {{if eq .LearningDetailLevel "exact"}}
   - **CRITICAL FOR EXACT MODE**: Extract ALL patterns from each learning file - do not miss any
   - Review the ENTIRE content of each learning file before extracting patterns
   - Extract every successful tool call, every failed attempt, every best practice, every warning
   - Be exhaustive and comprehensive - completeness is more important than brevity
   - If a learning file mentions 10 patterns, extract all 10, not just a few
   {{end}}

4. Return JSON with ONLY: title, success_patterns, failure_patterns. OMIT steps with no patterns.

The plan above is for reference to match step titles.`

	// Parse and execute the template
	tmpl, err := template.New("learning_integration").Parse(templateStr)
	if err != nil {
		return fmt.Sprintf("Error parsing learning integration template: %v", err)
	}

	var result strings.Builder
	if err := tmpl.Execute(&result, templateData); err != nil {
		return fmt.Sprintf("Error executing learning integration template: %v", err)
	}

	return result.String()
}
