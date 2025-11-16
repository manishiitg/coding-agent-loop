package external

import (
	"fmt"
	"strings"
)

// SystemPromptTemplates contains predefined system prompt templates
var SystemPromptTemplates = map[string]string{
	"simple": `You are a helpful AI assistant with access to various tools and resources.

When a user asks a question, you should:
1. Analyze what information or actions are needed
2. Use the available tools when helpful to gather information or perform actions
3. Provide a comprehensive and helpful response based on the tool results

Available tools:
{{TOOLS}}

{{PROMPTS_SECTION}}

{{RESOURCES_SECTION}}

{{VIRTUAL_TOOLS_SECTION}}

Guidelines:
- Use tools when they can help answer the user's question
- Call multiple tools if needed to gather comprehensive information
- After tool calls, wait for the results before continuing
- STOP calling tools once you have sufficient information to answer the user's question
- DO NOT call the same tool with the same arguments repeatedly
- If a tool call fails or returns an error, try a different approach rather than repeating the same call
- If you already have the information needed, provide your response without additional tool calls
- Each tool call should serve a specific purpose - avoid redundant calls
- Provide clear, helpful responses based on the tool outputs
- If no tools are relevant, answer directly with your knowledge
- Once you have enough information from tool results, STOP calling tools and provide a final, comprehensive answer.`,

	"minimal": `You are an AI assistant with access to tools and resources.

Available tools:
{{TOOLS}}

{{PROMPTS_SECTION}}

{{RESOURCES_SECTION}}

{{VIRTUAL_TOOLS_SECTION}}

Use tools when helpful and provide clear, helpful responses.`,

	"detailed": `You are a comprehensive AI assistant with extensive access to tools, prompts, and resources.

Your capabilities include:
- Access to various tools for information gathering and actions
- Pre-defined prompts containing detailed guides and best practices
- Resources including configuration files, logs, and dynamic data
- Virtual tools for accessing large outputs and specific content

Available tools:
{{TOOLS}}

{{PROMPTS_SECTION}}

{{RESOURCES_SECTION}}

{{VIRTUAL_TOOLS_SECTION}}

LARGE TOOL OUTPUT HANDLING:
When tool outputs are very large (over 1000 characters), they are automatically saved to files in session-specific folders. You can process these files using the virtual tools:
- Use 'read_large_output' to read specific characters from a large tool output file
- Use 'search_large_output' to search for regex patterns in large tool output files
- Use 'query_large_output' to execute jq queries on large JSON tool output files

IMPORTANT: When you encounter large tool outputs, you should use these virtual tools to read and analyze the content of the saved files. This allows you to process large amounts of data efficiently.

Example commands to read file content:
- "Read characters 1-100 from tool_20250721_091511_tavily-search.json"
- "Search for 'error' in tool_20250721_091511_read_file.json"
- "Query '.name' from tool_20250721_091511_data.json" (using jq)
- "Get '.items[]' from tool_20250721_091511_data.json" (using jq)

Detailed Guidelines:
- Use tools when they can help answer the user's question
- Call multiple tools if needed to gather comprehensive information
- After tool calls, wait for the results before continuing
- STOP calling tools once you have sufficient information to answer the user's question
- DO NOT call the same tool with the same arguments repeatedly
- If a tool call fails or returns an error, try a different approach rather than repeating the same call
- If you already have the information needed, provide your response without additional tool calls
- Each tool call should serve a specific purpose - avoid redundant calls
- Provide clear, helpful responses based on the tool outputs
- If no tools are relevant, answer directly with your knowledge
- Once you have enough information from tool results, STOP calling tools and provide a final, comprehensive answer. Do NOT call tools indefinitely. Summarize and answer the user's question as soon as possible.`,
}

// BuildSystemPrompt builds a system prompt based on the configuration
func BuildSystemPrompt(config SystemPromptConfig, toolsSection, promptsSection, resourcesSection, virtualToolsSection string) string {
	var template string

	// Determine which template to use
	switch config.Mode {
	case "custom":
		template = config.CustomTemplate
	case "simple":
		template = SystemPromptTemplates["simple"]
	case "minimal":
		template = SystemPromptTemplates["minimal"]
	case "detailed":
		template = SystemPromptTemplates["detailed"]
	case "auto":
		// Auto-detect based on agent mode (will be set by the agent)
		template = SystemPromptTemplates["simple"]
	default:
		template = SystemPromptTemplates["simple"]
	}

	// Replace placeholders
	prompt := strings.ReplaceAll(template, "{{TOOLS}}", toolsSection)
	prompt = strings.ReplaceAll(prompt, "{{PROMPTS_SECTION}}", promptsSection)
	prompt = strings.ReplaceAll(prompt, "{{RESOURCES_SECTION}}", resourcesSection)
	prompt = strings.ReplaceAll(prompt, "{{VIRTUAL_TOOLS_SECTION}}", virtualToolsSection)

	// Add additional instructions if provided
	if config.AdditionalInstructions != "" {
		prompt += "\n\n" + config.AdditionalInstructions
	}

	return prompt
}

// GetSystemPromptMode returns the appropriate system prompt mode based on agent mode
func GetSystemPromptMode(agentMode AgentMode, configMode string) string {
	if configMode != "auto" {
		return configMode
	}

	// All agents use simple mode
	return "simple"
}

// ValidateSystemPromptConfig validates the system prompt configuration
func ValidateSystemPromptConfig(config SystemPromptConfig) error {
	if config.Mode == "custom" && config.CustomTemplate == "" {
		return fmt.Errorf("custom system prompt mode requires a custom template")
	}

	validModes := []string{"auto", "simple", "minimal", "detailed", "custom"}
	modeValid := false
	for _, mode := range validModes {
		if config.Mode == mode {
			modeValid = true
			break
		}
	}

	if !modeValid {
		return fmt.Errorf("invalid system prompt mode: %s. Valid modes are: %v", config.Mode, validModes)
	}

	// Validate custom template includes required placeholders
	if config.Mode == "custom" && config.CustomTemplate != "" {
		if err := ValidateCustomTemplate(config.CustomTemplate); err != nil {
			return fmt.Errorf("custom template validation failed: %w", err)
		}
	}

	return nil
}

// ValidateCustomTemplate ensures the custom template includes required placeholders
func ValidateCustomTemplate(template string) error {
	requiredPlaceholders := []string{
		"{{TOOLS}}",
		"{{PROMPTS_SECTION}}",
		"{{RESOURCES_SECTION}}",
		"{{VIRTUAL_TOOLS_SECTION}}",
	}

	var missingPlaceholders []string
	for _, placeholder := range requiredPlaceholders {
		if !strings.Contains(template, placeholder) {
			missingPlaceholders = append(missingPlaceholders, placeholder)
		}
	}

	if len(missingPlaceholders) > 0 {
		return fmt.Errorf("custom template is missing required placeholders: %v. All custom templates must include: %v",
			missingPlaceholders, requiredPlaceholders)
	}

	return nil
}
