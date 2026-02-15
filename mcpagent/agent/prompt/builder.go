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

// GetCodeExecutionInstructions returns the code execution mode instructions section
// This can be reused by agents that need to include code execution guidance in their prompts
// workspacePath: the actual workspace path to substitute in examples (e.g., "Workflow/runs/iteration-1/execution")
// If workspacePath is empty (chat mode), workspace-related instructions are excluded
func GetCodeExecutionInstructions(workspacePath string) string {
	// Base instructions (always included)
	baseInstructions := `**CODE EXECUTION MODE - Access MCP Servers via Go Code:**

{{TOOL_STRUCTURE}}

**📋 Workflow:**
1. Use discover_code_files to get exact function signatures before writing code
2. Write Go code using write_code that calls generated tool functions
3. Execute and get results
4. Recognize completion: If output shows completion, move to next step or provide final answer

**⚠️ CRITICAL Requirements:**
- ✅ **MUST have package main declaration**
- ✅ **Use fmt.Println()/fmt.Printf() to output results**
- ✅ **Import generated packages** (e.g., import "workspace_tools", import "aws") - go.work is set up automatically
- ✅ **ALWAYS use discover_code_files FIRST** to see exact function signatures and parameter names`

	// Workspace-specific instructions (only for workflow mode with workspace path)
	workspaceInstructions := `
- ❌ **NEVER use Go standard file I/O** (os.WriteFile, os.ReadFile, etc.) - files go to wrong directory
- ✅ **ALWAYS use workspace_tools package** for file operations (ReadWorkspaceFile, UpdateWorkspaceFile)
- ✅ **ALWAYS pass WorkspacePath as first arg**: write_code args=["` + workspacePath + `"] → access via os.Args[1] in Go
- ✅ **ALWAYS use filepath.Join()**: Never hardcode paths. Use filepath.Join(os.Args[1], "step-N/file.json")`

	// Error handling (always included)
	errorHandling := `

**⚠️ Error Handling Pattern:**
Functions return only string (no error). Follow this pattern for EVERY tool call:
1. Call: output := toolName(params)
2. Print: fmt.Printf("Tool output: %%s\n", output)
3. Check: Examine output string for error indicators (e.g., strings.HasPrefix(output, "Error:"))
4. Use: Process result if successful

- **API Errors** (network, HTTP): Functions panic - exceptional cases
- **Tool Errors**: Returned in result string - examine output to detect errors
- **✅ ALWAYS print output BEFORE checking errors** - helps discover error patterns`

	// Workspace-specific error note
	workspaceErrorNote := `
- **⚠️ ReadWorkspaceFile PANICS if file missing** - Use ListWorkspaceFiles to verify existence first`

	// Best practices (always included)
	bestPractices := `

**🔧 Best Practices:**
- **Debugging**: Use fmt.Printf() liberally to trace execution and print variable values
- **Complex problems**: Break down into steps, test each tool individually, build incrementally
- **Multiple tools**: Test tools separately first, then combine once you understand their response patterns
- **Error recovery**: Use discover_code_files to verify parameter names, check imports/types for build errors`

	// Workspace example (only for workflow mode)
	workspaceExample := `

**Example - Tool Call with WorkspacePath (CRITICAL):**
  package main
  import ("fmt"; "os"; "path/filepath"; "strings"; "workspace_tools")

  func main() {
      // ✅ CRITICAL: Get workspace path from CLI args (passed via write_code args parameter)
      if len(os.Args) < 2 {
          fmt.Println("❌ Error: WorkspacePath required as first argument")
          os.Exit(1)
      }
      basePath := os.Args[1]  // e.g., "` + workspacePath + `"

      // ✅ CORRECT: Use filepath.Join for all paths (NEVER hardcode full paths)
      inputPath := filepath.Join(basePath, "step-1/credentials.json")

      // Use discover_code_files to see exact struct definition first!
      output := workspace_tools.ReadWorkspaceFile(workspace_tools.ReadWorkspaceFileParams{
          Filepath: inputPath,
      })
      fmt.Printf("Tool output: %%s\n", output)
      if strings.HasPrefix(output, "Error:") {
          fmt.Printf("❌ Error: %%s\n", output)
          return
      }
      fmt.Printf("✅ Success!\n")
  }

  // ⚠️ Call this with: write_code(code="...", args=["` + workspacePath + `"])

**Example - File Operations (CRITICAL):**
  package main
  import ("fmt"; "os"; "path/filepath"; "strings"; "workspace_tools")

  func main() {
      basePath := os.Args[1]  // ✅ ALWAYS get from CLI args
      outputPath := filepath.Join(basePath, "step-2/results.json")  // ✅ ALWAYS use filepath.Join

      result := workspace_tools.UpdateWorkspaceFile(workspace_tools.UpdateWorkspaceFileParams{
          Filepath: outputPath,
          Content:  "{\"status\": \"success\"}",
      })
      fmt.Printf("Tool output: %%s\n", result)
      if strings.HasPrefix(result, "Error:") {
          fmt.Printf("❌ Error: %%s\n", result)
          return
      }

      // ❌ WRONG: NEVER hardcode paths like "` + workspacePath + `/step-2/file.json"
      // ❌ WRONG: NEVER use os.WriteFile, os.ReadFile - files go to wrong directory!
  }`

	// Simple example for chat mode (no workspace)
	simpleExample := `

**Example - Basic Tool Call:**
  package main
  import ("fmt"; "strings"; "your_server_tools")

  func main() {
      // Use discover_code_files to see exact struct definition first!
      output := your_server_tools.YourToolFunction(your_server_tools.YourToolParams{
          ParamName: "value",
      })
      fmt.Printf("Tool output: %%s\n", output)
      if strings.HasPrefix(output, "Error:") {
          fmt.Printf("❌ Error: %%s\n", output)
          return
      }
      fmt.Printf("✅ Success!\n")
  }`

	// Common mistakes base (always included)
	commonMistakesBase := `

**🚨 Common Mistakes:**
- ❌ Checking err != nil (functions return string, no error)
- ❌ Not printing output before checking errors
- ❌ Using wrong parameter names - always use discover_code_files first
- ❌ Writing placeholder code - always implement actual logic
- ❌ Looping on completion messages - recognize completion and move on
- ❌ Wrong imports - use "workspace_tools" NOT "generated/workspace_tools"`

	// Workspace-specific mistakes
	workspaceMistakes := `
- ❌ Using standard Go file I/O - must use workspace_tools package
- ❌ Missing args in write_code - MUST pass args=["` + workspacePath + `"]
- ❌ Hardcoding paths - NEVER use "Workflow/runs/iteration-X/..." in Go code`

	// Build instructions based on mode
	var instructions string
	if workspacePath != "" {
		// Workflow mode with workspace path
		instructions = baseInstructions + workspaceInstructions + errorHandling + workspaceErrorNote + bestPractices + workspaceExample + commonMistakesBase + workspaceMistakes
	} else {
		// Chat mode without workspace path
		instructions = baseInstructions + errorHandling + bestPractices + simpleExample + commonMistakesBase
	}

	return instructions
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
</core_principles>`
	}

	// Build tool usage section based on mode
	var toolUsageSection string
	if useCodeExecutionMode {
		// Get code execution instructions and replace {{TOOL_STRUCTURE}} placeholder
		// Note: workspace path is passed as empty here - it will be substituted by workflow agents
		// that have access to the actual workspace path in their template processing
		codeExecutionInstructions := GetCodeExecutionInstructions("")

		// Replace {{TOOL_STRUCTURE}} placeholder with actual tool structure
		if toolStructureJSON != "" {
			toolStructureSection := "\n\n<available_code>\n" +
				"**AVAILABLE CODE FILES AND FUNCTIONS:**\n\n" +
				"The following code files and functions are available for use in your Go code. This structure shows all servers, custom tools, and their functions:\n\n" +
				"```json\n" +
				toolStructureJSON + "\n" +
				"```\n\n" +
				"**How to use:**\n" +
				"- The JSON structure shows package names as keys (e.g., \"google_sheets_tools\", \"workspace_tools\")\n" +
				"- Each package contains a \"tools\" array with available function names (e.g., \"GetDocument\", \"ListSpreadsheets\")\n" +
				"- Use the package name as \"server_name\" in discover_code_files (e.g., discover_code_files(server_name=\"google_sheets_tools\", tool_names=[\"GetDocument\"]))\n" +
				"- Import the package and call the function in your Go code (e.g., import \"google_sheets_tools\")\n" +
				"- Use 'discover_code_files' tool to get exact function signatures before writing code\n" +
				"</available_code>\n"
			codeExecutionInstructions = strings.ReplaceAll(codeExecutionInstructions, ToolStructurePlaceholder, toolStructureSection)
		} else {
			// Provide helpful message when tool structure is not available
			toolStructureSection := "\n\n<available_code>\n" +
				"**AVAILABLE CODE FILES AND FUNCTIONS:**\n\n" +
				"Tool structure discovery is in progress or unavailable. Use the 'discover_code_files' tool to explore available code:\n\n" +
				"- **discover_code_files(server_name, tool_name)**: Get exact function signatures for any tool\n" +
				"- Example: discover_code_files(server_name=\"aws\", tool_name=\"GetDocument\")\n" +
				"- This will show you the exact Go function signature, parameters, and usage\n\n" +
				"**How to discover tools:**\n" +
				"1. Use discover_code_files to find available servers and tools\n" +
				"2. Get the exact function signature for the tool you want to use\n" +
				"3. Import the package (e.g., import \"aws\") and call the function in your Go code\n" +
				"</available_code>\n"
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
		// Code execution mode: Show simplified virtual tools section
		return `🔧 AVAILABLE FUNCTIONS:

- **discover_code_files** - Get Go source code for a specific function
  Usage: discover_code_files(server_name="aws", tool_name="GetDocument")

- **write_code** - Write and execute Go code
  Code runs as separate process via 'go run'
  Use fmt.Println() to output results
  Optional 'args' parameter: Array of strings passed as CLI arguments (accessible via os.Args[1], os.Args[2], etc.)`
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
