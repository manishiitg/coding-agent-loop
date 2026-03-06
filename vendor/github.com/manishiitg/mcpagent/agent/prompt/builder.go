package prompt

import (
	"fmt"
	"strings"
	"time"

	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"

	"github.com/mark3labs/mcp-go/mcp"
)

// GetToolSearchInstructions returns the tool search mode instructions section
// This provides guidance on how to use the search_tools virtual tool to discover tools
func GetToolSearchInstructions() string {
	return `**TOOL SEARCH MODE - Dynamic Tool Discovery:**

You have access to a large catalog of tools, but they are not loaded by default to optimize performance.
Use the **search_tools** function to discover tools, and then **add_tool** to load them.

**📋 How to Search & Add:**
1. Call search_tools with a search pattern (regex or keywords)
2. Review the returned tools with their descriptions
3. Call add_tool with the names of the tools you want to use
4. Use the discovered tools to complete your task

**🔍 Search Examples:**
- search_tools(query="weather") - Find weather-related tools
- search_tools(query="database.*query") - Regex for database query tools
- search_tools(query="(?i)slack") - Case-insensitive search for Slack tools
- search_tools(query="file") - Find file manipulation tools

**⚠️ Search Behavior:**
- Regex patterns are tried first for precise matching
- If no regex matches found, fuzzy search is automatically applied
- Fuzzy search considers tool names and descriptions
- Top 5 fuzzy matches are returned when exact matches fail

**📝 Workflow:**
1. **Understand** the user's request
2. **Search** for relevant tools using search_tools
3. **Add** the tools you need using add_tool(tool_names=["tool1", "tool2"])
4. **Use** the discovered tools to complete the task
5. **Search again** if needed for additional capabilities

**💡 Tips:**
- Start with broad searches, then narrow down
- Discovered tools must be explicitly added with add_tool
- Once added, tools remain available for the entire conversation
- You can search multiple times to find different tools
- Check tool descriptions to understand parameters`
}

// GetCodeExecutionInstructions returns the code execution mode instructions section.
// workspacePath: the actual workspace path to substitute in examples.
// If workspacePath is empty (chat mode), workspace-related instructions are excluded.
func GetCodeExecutionInstructions(workspacePath string) string {
	return `**CODE EXECUTION MODE — Access MCP Tools via HTTP API:**

{{TOOL_STRUCTURE}}

**Workflow:**
1. See available servers and tools in the index above
2. Call get_api_spec(server_name="...", tool_name="...") to get the full API spec for the tool(s) you need
   - tool_name accepts a single string or an array of strings for multiple tools
3. Use execute_shell_command to write and run code — prefer Python for reliability and readability
4. MCP_API_URL and MCP_API_TOKEN env vars are available in the execution environment
5. MCP and custom tools are accessed via HTTP POST to per-tool endpoints documented in the OpenAPI spec
6. Session tracking is automatic — MCP_API_URL already includes the session context, so you do NOT need to add session_id to request bodies

**How to call tools from code:**
- MCP tools: POST {MCP_API_URL}/tools/mcp/{server}/{tool}
- Custom tools: POST {MCP_API_URL}/tools/custom/{tool}
- Send tool arguments as JSON body (do NOT include session_id — it is handled automatically via the URL)
- Include Authorization: Bearer $MCP_API_TOKEN header
- Response: {"success": true/false, "result": "...", "error": "..."}

**Example (Python):**
` + "```" + `python
import requests, os
url = os.environ["MCP_API_URL"] + "/tools/mcp/google_sheets/get_document"
headers = {"Authorization": f"Bearer {os.environ.get('MCP_API_TOKEN', '')}", "Content-Type": "application/json"}
payload = {"spreadsheet_id": "abc123"}
resp = requests.post(url, json=payload, headers=headers)
print(resp.json())
` + "```" + `

**Example (curl):**
` + "```" + `bash
curl -X POST "$MCP_API_URL/tools/mcp/google_sheets/get_document" \
  -H "Authorization: Bearer $MCP_API_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"spreadsheet_id": "abc123"}'
` + "```" + `

**Best Practices:**
- Use get_api_spec(server_name="...", tool_name="...") to get specs before calling any tool
- Prefer Python for writing code — it handles JSON, HTTP requests, and error handling cleanly
- Break complex tasks into steps, test each tool call individually
- Always check the "success" field in responses
- MCP_API_URL and MCP_API_TOKEN are pre-set in the shell environment — always use them
- **IMPORTANT**: When custom tools are available as direct calls (via mcp__api-bridge__*), ALWAYS prefer using them over writing equivalent shell commands. Custom tools provide structured input validation and atomic operations.`
}

// BuildSystemPromptWithoutTools builds the system prompt without including tool descriptions
// This is useful when tools are passed via llmtypes.WithTools() to avoid prompt length issues
// toolStructureJSON is optional - if provided in code execution mode, it will replace {{TOOL_STRUCTURE}} placeholder
// useToolSearchMode enables tool search mode instructions when true
// toolCategories is optional list of tool categories for tool search mode
func BuildSystemPromptWithoutTools(prompts map[string][]mcp.Prompt, resources map[string][]mcp.Resource, mode interface{}, discoverResource bool, discoverPrompt bool, useCodeExecutionMode bool, toolStructureJSON string, useToolSearchMode bool, toolCategories []string, logger loggerv2.Logger, enableParallelToolExecution bool) string {
	// Build prompts section with previews (only if discoverPrompt is true and NOT in code execution mode)
	// In code execution mode, prompts/resources are not accessible via get_prompt/get_resource
	var promptsSection string
	if discoverPrompt && !useCodeExecutionMode {
		promptsSection = buildPromptsSectionWithPreviews(prompts, logger)
	} else {
		promptsSection = "" // Empty prompts section when discovery is disabled or in code execution mode
	}

	// Build resources section (only if discoverResource is true and NOT in code execution mode)
	// In code execution mode, resources are not accessible via get_resource
	var resourcesSection string
	if discoverResource && !useCodeExecutionMode {
		resourcesSection = buildResourcesSection(resources)
	} else {
		resourcesSection = "" // Empty resources section when discovery is disabled or in code execution mode
	}

	// Build virtual tools section (only mention tools that are actually available)
	virtualToolsSection := buildVirtualToolsSection(useCodeExecutionMode, useToolSearchMode, prompts, resources)

	// Get current date and time
	now := time.Now()
	currentDate := now.Format("2006-01-02")
	currentTime := now.Format("15:04:05")

	// Build core principles section based on mode
	var corePrinciplesSection string
	if useCodeExecutionMode {
		corePrinciplesSection = `<core_principles>
When answering questions:
1. **Think** about what information/actions are needed
2. **Write code** to gather information and perform actions
3. **Provide helpful responses** based on execution results
</core_principles>`
	} else if useToolSearchMode {
		corePrinciplesSection = `<core_principles>
**Your Goal:** Complete the user's request autonomously using discovered tools.

**Operating Rules:**
1. **Search First:** Use search_tools to find relevant tools before attempting to use them.
2. **Be Proactive:** Once tools are discovered, use them without asking for permission.
3. **Chain Actions:** If a tool output leads to a next step, take it immediately.
4. **Search Again:** If you need additional capabilities, search for more tools.
</core_principles>`
	} else {
		corePrinciplesSection = `<core_principles>
**Your Goal:** Complete the user's request autonomously.

**Operating Rules:**
1. **Be Proactive:** Do not ask for permission to use tools. Just use them.
2. **Chain Actions:** If a tool output leads to a next step, take it immediately. Do not stop to report intermediate progress unless asked.
3. **Solve Fully:** strive to reach the final answer or state before returning control to the user.
4. **Conversational Messages:** For greetings, small talk, or simple questions that don't require any action, respond with a brief, friendly message naturally.
</core_principles>`
	}

	// Build tool usage section based on mode
	var toolUsageSection string
	if useCodeExecutionMode {
		codeExecutionInstructions := GetCodeExecutionInstructions("")

		// Replace {{TOOL_STRUCTURE}} placeholder with the tool index
		if toolStructureJSON != "" {
			toolStructureSection := "\n\n<available_tools>\n" +
				"**AVAILABLE SERVERS AND TOOLS:**\n\n" +
				"The following MCP servers and their tools are accessible via HTTP API.\n" +
				"Call get_api_spec(server_name=\"...\", tool_name=\"...\") to get the full API spec for specific tools.\n\n" +
				"```json\n" +
				toolStructureJSON + "\n" +
				"```\n\n" +
				"Domain tools (MCP and custom) are accessible via HTTP API endpoints documented in the OpenAPI spec.\n" +
				"System tools (e.g. execute_shell_command) are available as direct LLM calls.\n" +
				"</available_tools>\n"
			codeExecutionInstructions = strings.ReplaceAll(codeExecutionInstructions, ToolStructurePlaceholder, toolStructureSection)
		} else {
			toolStructureSection := "\n\n<available_tools>\n" +
				"**AVAILABLE SERVERS AND TOOLS:**\n\n" +
				"Tool index is being built. Use get_api_spec(server_name=\"...\", tool_name=\"...\") to discover endpoints.\n" +
				"</available_tools>\n"
			codeExecutionInstructions = strings.ReplaceAll(codeExecutionInstructions, ToolStructurePlaceholder, toolStructureSection)
		}

		toolUsageSection = `<code_usage>
` + codeExecutionInstructions + `
</code_usage>`
	} else if useToolSearchMode {
		// Get tool search instructions
		toolSearchInstructions := GetToolSearchInstructions()

		toolUsageSection = `<tool_search>
` + toolSearchInstructions + `
</tool_search>`
	} else {
		var parallelToolHint string
		if enableParallelToolExecution {
			parallelToolHint = `

**Parallel Execution:**
- You can call multiple tools in a single response — they will execute concurrently
- Use this to speed up independent operations (e.g., reading multiple files, querying multiple APIs)
- Only parallelize independent calls — if one tool's output is needed as input for another, call them sequentially`
		}
		toolUsageSection = `<tool_usage>
**Guidelines:**
- Use tools when they can help answer the question
- Use virtual tools for detailed prompts/resources when relevant
- Provide clear responses based on tool results` + parallelToolHint + `

**Best Practices:**
- Use virtual tools to access detailed knowledge when relevant
- **If a tool call fails, retry with different arguments or parameters**
- **Try alternative approaches when tools return errors or unexpected results**
- **Modify search terms, file paths, or query parameters to overcome failures**
</tool_usage>`
	}

	// Build context offloading section (only for simple mode)
	var largeOutputHandlingSection string
	if useCodeExecutionMode {
		largeOutputHandlingSection = "" // Not available in code execution mode
	} else {
		largeOutputHandlingSection = `
CONTEXT OFFLOADING:
Large tool outputs (>1000 chars) are automatically offloaded to filesystem (offload context pattern). Use virtual tools to access them:
- 'read_large_output': Read specific characters from offloaded files
- 'search_large_output': Search for patterns in offloaded files  
- 'query_large_output': Execute jq queries on offloaded JSON files`
	}

	// Always use Simple system prompt template
	prompt := SystemPromptTemplate

	// Replace all placeholders
	prompt = strings.ReplaceAll(prompt, CorePrinciplesPlaceholder, corePrinciplesSection)
	prompt = strings.ReplaceAll(prompt, ToolUsagePlaceholder, toolUsageSection)
	prompt = strings.ReplaceAll(prompt, PromptsSectionPlaceholder, promptsSection)
	prompt = strings.ReplaceAll(prompt, ResourcesSectionPlaceholder, resourcesSection)
	prompt = strings.ReplaceAll(prompt, VirtualToolsSectionPlaceholder, virtualToolsSection)
	prompt = strings.ReplaceAll(prompt, LargeOutputHandlingPlaceholder, largeOutputHandlingSection)
	prompt = strings.ReplaceAll(prompt, CurrentDatePlaceholder, currentDate)
	prompt = strings.ReplaceAll(prompt, CurrentTimePlaceholder, currentTime)

	return prompt
}

// buildPromptsSectionWithPreviews builds the prompts section with previews
func buildPromptsSectionWithPreviews(prompts map[string][]mcp.Prompt, logger loggerv2.Logger) string {

	// Count total prompts across all servers
	totalPrompts := 0
	for _, serverPrompts := range prompts {
		totalPrompts += len(serverPrompts)
	}

	if totalPrompts == 0 {
		logger.Debug("No prompts found for preview generation - skipping prompts section")
		return ""
	}

	logger.Debug("Building prompts section with previews",
		loggerv2.Int("server_count", len(prompts)),
		loggerv2.Int("total_prompts", totalPrompts))

	var promptsList []string
	for serverName, serverPrompts := range prompts {
		if len(serverPrompts) == 0 {
			// Skip servers with no prompts
			continue
		}

		logger.Debug("Processing server prompts",
			loggerv2.String("server_name", serverName),
			loggerv2.Int("prompt_count", len(serverPrompts)))

		promptsList = append(promptsList, fmt.Sprintf("%s:", serverName))
		for _, prompt_item := range serverPrompts {
			name := prompt_item.Name
			description := prompt_item.Description

			logger.Debug("Processing prompt",
				loggerv2.String("server_name", serverName),
				loggerv2.String("prompt_name", name),
				loggerv2.Int("description_length", len(description)))

			// Extract preview (first 10 lines) from the description
			preview := extractPromptPreview(description)

			// Format as preview with name and first few lines
			promptsList = append(promptsList, fmt.Sprintf("  - %s: %s", name, preview))
		}
	}

	// Double-check: if no prompts were actually added, return empty
	if len(promptsList) == 0 {
		logger.Debug("No actual prompts found after processing - skipping prompts section")
		return ""
	}

	promptsText := strings.Join(promptsList, "\n")
	logger.Debug("Prompts section built",
		loggerv2.Int("total_length", len(promptsText)),
		loggerv2.Int("prompt_lines", len(promptsList)))

	return strings.ReplaceAll(PromptsSectionTemplate, PromptsListPlaceholder, promptsText)
}

// extractPromptPreview extracts the first 10 lines from prompt content
func extractPromptPreview(description string) string {
	// If description contains "Content:", extract the content part (legacy format)
	if strings.Contains(description, "\n\nContent:\n") {
		parts := strings.Split(description, "\n\nContent:\n")
		if len(parts) > 1 {
			content := parts[1]

			// Split into lines and take first 10 lines
			lines := strings.Split(content, "\n")
			previewLines := lines
			if len(lines) > 10 {
				previewLines = lines[:10]
			}

			preview := strings.Join(previewLines, "\n")
			if len(lines) > 10 {
				preview += "\n... (use 'get_prompt' tool for full content)"
			}

			return preview
		}
	}

	// If description contains full content (new format), extract preview
	if len(description) > 100 && !strings.Contains(description, "Prompt loaded from") {
		// Split into lines and take first 10 lines
		lines := strings.Split(description, "\n")
		previewLines := lines
		if len(lines) > 10 {
			previewLines = lines[:10]
		}

		preview := strings.Join(previewLines, "\n")
		if len(lines) > 10 {
			preview += "\n... (use 'get_prompt' tool for full content)"
		}

		return preview
	}

	// If no content section or short description, return the description as is
	return description
}

// buildResourcesSection builds the resources section
func buildResourcesSection(resources map[string][]mcp.Resource) string {
	if len(resources) == 0 {
		return ""
	}

	var resourcesList []string
	for serverName, serverResources := range resources {
		resourcesList = append(resourcesList, fmt.Sprintf("%s:", serverName))
		for _, resource := range serverResources {
			name := resource.Name
			uri := resource.URI
			description := resource.Description
			resourcesList = append(resourcesList, fmt.Sprintf("  - %s (%s): %s", name, uri, description))
		}
	}

	resourcesText := strings.Join(resourcesList, "\n")
	return strings.ReplaceAll(ResourcesSectionTemplate, ResourcesListPlaceholder, resourcesText)
}

// buildVirtualToolsSection builds the virtual tools section
// Only mentions tools that are actually available (prompts/resources must exist)
func buildVirtualToolsSection(useCodeExecutionMode bool, useToolSearchMode bool, prompts map[string][]mcp.Prompt, resources map[string][]mcp.Resource) string {
	if useCodeExecutionMode {
		return `AVAILABLE FUNCTIONS:

- **get_api_spec** - Get the full OpenAPI spec for specific tool(s)
  Usage: get_api_spec(server_name="google_sheets", tool_name="get_document")
  Multiple tools: get_api_spec(server_name="google_sheets", tool_name=["create_spreadsheet", "update_values"])
  All servers and tool names are listed in the tool index above — use get_api_spec to get endpoint details and request schemas.

Domain tools (MCP and custom) are accessed via HTTP endpoints documented in the OpenAPI spec.
System tools (e.g. execute_shell_command) are available as direct LLM function calls.
Custom domain tools use: POST /tools/custom/{tool}
MCP tools use: POST /tools/mcp/{server}/{tool}`
	}

	if useToolSearchMode {
		// Tool search mode: Show search_tools as the primary discovery mechanism
		return `🔧 TOOL DISCOVERY:

- **search_tools** - Search for available tools by name or description
  Usage: search_tools(query="regex_pattern")
  Returns matching tools (you must use add_tool to load them)
  Supports regex patterns and fuzzy matching

- **add_tool** - Add one or more tools to your toolkit
  Usage: add_tool(tool_names=["name1", "name2"])
  
Once you discover tools using search_tools, add them using add_tool, then they will be available for you to call directly.`
	}

	// Check if prompts actually exist
	hasPrompts := false
	if prompts != nil {
		totalPrompts := 0
		for _, serverPrompts := range prompts {
			totalPrompts += len(serverPrompts)
		}
		hasPrompts = totalPrompts > 0
	}

	// Check if resources actually exist
	hasResources := false
	if resources != nil {
		totalResources := 0
		for _, serverResources := range resources {
			totalResources += len(serverResources)
		}
		hasResources = totalResources > 0
	}

	// Build virtual tools list based on what's actually available
	var toolsList []string
	if hasPrompts {
		toolsList = append(toolsList, "- **get_prompt**: Fetch full prompt content (server + name) from an mcp server")
	}
	if hasResources {
		toolsList = append(toolsList, "- **get_resource**: Fetch resource content (server + uri) from an mcp server")
	}

	// If no tools are available, return empty string (section will be empty)
	if len(toolsList) == 0 {
		return ""
	}

	// Build the section with only available tools
	toolsText := strings.Join(toolsList, "\n")
	return `🔧 VIRTUAL TOOLS:

` + toolsText + `

These are internal tools - just specify server and identifier.`
}
