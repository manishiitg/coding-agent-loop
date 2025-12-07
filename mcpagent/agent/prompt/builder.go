package prompt

import (
	"fmt"
	"strings"
	"time"

	loggerv2 "mcpagent/logger/v2"

	"github.com/mark3labs/mcp-go/mcp"
)

// GetCodeExecutionInstructions returns the code execution mode instructions section
// This can be reused by agents that need to include code execution guidance in their prompts
func GetCodeExecutionInstructions() string {
	return `**CODE EXECUTION MODE - Access MCP Servers via Go Code:**

{{TOOL_STRUCTURE}}

**📋 Workflow:**
1. **Review** available code packages in the structure above
2. **Discover code FIRST**: Use discover_code_files to get exact function signatures before writing any code
3. **Write** Go code using write_code that calls the generated tool functions
4. **Execute** and get results

**⚠️ CRITICAL - Code Requirements:**
- ✅ **MUST have package main declaration**
- ✅ **Use fmt.Println() to output results**
- ✅ **You CAN import generated packages** (e.g., import "workspace_tools") - go.work is automatically set up with workspace modules
- ✅ **ALWAYS use discover_code_files FIRST** to see exact function signatures and parameter names
- ❌ **NEVER use Go standard file I/O** (os.WriteFile, ioutil.WriteFile, os.Create, etc.) - files will go to wrong directory
- ✅ **ALWAYS use workspace_tools for file operations** - files must go to workspace, not execution directory
- ✅ **CLI parameters**: Use optional 'args' parameter in write_code to pass command-line arguments (accessible via os.Args[1], os.Args[2], etc.)

**🐛 DEBUGGING BEST PRACTICES:**
- ✅ **Use fmt.Printf() liberally** to trace execution flow and debug issues quickly
- ✅ **Print variable values** before and after operations: fmt.Printf("Before call: params=%%+v\n", params)
- ✅ **Print intermediate results** to understand data flow: fmt.Printf("Step 1 complete: result=%%s\n", result)
- ✅ **Print error details** when handling errors: fmt.Printf("Error occurred: %%v\n", err)
- ✅ **Add progress markers** for long operations: fmt.Println("Processing item 1 of 10...")
- 💡 **More debug output = faster problem identification** when code execution fails

**💡 You Can Write Logic:**
- Use **if/else** to make decisions based on results
- Call **multiple functions** in sequence
- **Combine different servers** in one code block
- Use **loops** to process data

**🔧 Complex Problem Solving Strategy:**
When using multiple tools or writing complex code, **ALWAYS break down the problem into smaller steps**:
1. **Break down first**: Identify individual steps needed to solve the problem
2. **Test each tool individually**: Write separate code blocks to test each tool call and understand its response pattern
3. **Discover patterns**: Print outputs and examine how each tool responds (success format, error format, data structure)
4. **Build incrementally**: Once you understand each tool's behavior, combine them step by step
5. **Verify at each step**: Test intermediate results before moving to the next step

**Why this matters:**
- ✅ **Discover tool response patterns** - Understand success vs error formats for each tool
- ✅ **Debug more effectively** - When something fails, you know which step caused it
- ✅ **Build confidence** - Test each tool individually before combining them
- ✅ **Handle errors better** - Learn error patterns for each tool before they're nested in complex logic

**Example approach for complex problems:**
- Step 1: Test tool A alone → Print output → Understand response format
- Step 2: Test tool B alone → Print output → Understand response format  
- Step 3: Combine A + B → Test together → Verify combined behavior
- Step 4: Add tool C → Test all three → Final solution

**⚠️ CRITICAL - Error Handling Pattern:**
Functions return only string (no error). Follow this pattern for EVERY tool call:
1. Call tool function: output := toolName(params)
2. Print output: fmt.Printf("Tool output: %%s\n", output)
3. Check output for errors - examine the output string to detect error indicators
4. Use result if successful

- **API Errors** (network, HTTP): Functions **panic** - exceptional cases
- **Tool Execution Errors**: Returned in result string - examine output to detect errors
- **✅ ALWAYS print output BEFORE checking errors** - helps discover error patterns and formats

**Basic Example (MCP Tool) - TYPED STRUCTS:**
  package main

  import (
      "fmt"
      "strings"
      "aws_tools"  // Import the MCP server package
  )

  func main() {
      fmt.Println("Starting document retrieval...")
      
      // Use typed struct for parameters - IDE provides autocomplete!
      params := aws_tools.GetDocumentParams{
          DocumentId: "doc123",
      }
      fmt.Printf("Calling GetDocument with params: DocumentId=%%s\n", params.DocumentId)
      
      // Follow error handling pattern: call → print → check → use
      output := aws_tools.GetDocument(params)
      fmt.Printf("Tool output: %%s\n", output)
      // Check output for errors - examine the output to detect error indicators
      if strings.HasPrefix(output, "Error:") {
          fmt.Printf("❌ Error detected: %%s\n", output)
          return
      }
      fmt.Printf("✅ Success! Result length: %%d bytes\n", len(output))
  }

**Example (Custom Tool - Workspace) - TYPED STRUCTS:**
  package main

  import (
      "fmt"
      "strings"
      "workspace_tools"  // Import generated package - go.work is set up automatically!
  )

  func main() {
      // IMPORTANT: Use discover_code_files to see exact struct definition!
      // Functions now accept typed structs with autocomplete and type safety
      params := workspace_tools.ReadWorkspaceFileParams{
          Filepath: "Workflow/All Bank Parsing/todo_creation/todo.md",
      }
      // Follow error handling pattern: call → print → check → use
      output := workspace_tools.ReadWorkspaceFile(params)
      fmt.Printf("Tool output: %%s\n", output)
      // Check output for errors - examine the output to detect error indicators
      if strings.HasPrefix(output, "Error:") {
          fmt.Printf("❌ Error detected: %%s\n", output)
          return
      }
      fmt.Printf("✅ Success! File content retrieved\n")
  }

**Example (Writing Files to Workspace) - CRITICAL:**
  package main

  import (
      "fmt"
      "strings"
      "workspace_tools"  // MUST use workspace_tools for file operations!
  )

  func main() {
      // ✅ CORRECT: Use workspace_tools to write files to workspace
      writeParams := workspace_tools.UpdateWorkspaceFileParams{
          Filepath: "data/results.json",
          Content:  "{\"status\": \"success\", \"data\": \"...\"}",
      }
      // Follow error handling pattern: call → print → check → use
      result := workspace_tools.UpdateWorkspaceFile(writeParams)
      fmt.Printf("Tool output: %%s\n", result)
      // Check output for errors - examine the output to detect error indicators
      if strings.HasPrefix(result, "Error:") {
          fmt.Printf("❌ Error detected: %%s\n", result)
          return
      }
      fmt.Printf("✅ Success! File written to workspace\n")

      // ❌ WRONG: NEVER use standard Go file I/O - files go to wrong directory!
      // os.WriteFile("data.json", data, 0644)  // DON'T DO THIS!
      // ioutil.WriteFile("data.json", data, 0644)  // DON'T DO THIS!
      // os.Create("data.json")  // DON'T DO THIS!
      // Files written with standard I/O go to tool_output_folder/, NOT workspace!
  }

**Example with Multiple Servers - TYPED STRUCTS:**
  package main

  import (
      "fmt"
      "strings"
      "aws_tools"
      "slack_tools"
  )

  func main() {
      // Call AWS tool - follow error handling pattern: call → print → check → use
      data := aws_tools.GetCosts(aws_tools.GetCostsParams{})
      fmt.Printf("AWS GetCosts output: %%s\n", data)
      // Check output for errors - examine the output to detect error indicators
      if strings.HasPrefix(data, "Error:") {
          fmt.Printf("❌ Error detected: %%s\n", data)
          return
      }
      fmt.Printf("✅ Costs retrieved successfully\n")

      // Call Slack tool if costs are high
      if strings.Contains(data, "high") {
          params := slack_tools.SendMessageParams{
              Channel: "alerts",
              Text:    "High costs detected",
          }
          alert := slack_tools.SendMessage(params)
          fmt.Printf("Slack SendMessage output: %%s\n", alert)
          // Check output for errors
          if strings.HasPrefix(alert, "Error:") {
              fmt.Printf("❌ Error detected: %%s\n", alert)
              return
          }
          fmt.Printf("✅ Alert sent successfully\n")
      }
  }

**🚨 COMMON MISTAKES TO AVOID:**
1. **❌ WRONG**: Checking err != nil after tool calls
   **Why wrong**: Functions return only string, no error - API errors panic, tool errors are in result
   **✅ CORRECT**: Examine the output string to detect errors (e.g., check if output starts with "Error:")

2. **❌ WRONG**: Not printing tool output before checking for errors
   **Why wrong**: You can't discover error patterns or debug issues without seeing the actual output
   **✅ CORRECT**: ALWAYS print output first, then examine it to detect error indicators

3. **❌ WRONG**: Using wrong parameter names (e.g., "path" instead of "filepath")
   **✅ CORRECT**: Always use discover_code_files to see exact parameter names before writing code
   
4. **❌ WRONG**: Assuming function signatures without checking
   **✅ CORRECT**: Use discover_code_files to get exact function signatures with parameter types

5. **❌ WRONG**: Using Go standard file I/O (os.WriteFile, ioutil.WriteFile, os.Create, etc.)
   **Why wrong**: Files written with standard I/O go to tool_output_folder/ directory, NOT workspace!
   **✅ CORRECT**: ALWAYS use workspace_tools.UpdateWorkspaceFile() for writing files

6. **❌ WRONG**: Using os.ReadFile or ioutil.ReadFile to read workspace files
   **Why wrong**: Standard I/O reads from execution directory, not workspace!
   **✅ CORRECT**: ALWAYS use workspace_tools.ReadWorkspaceFile() for reading files

**🔧 Error Recovery:**
- **Tool execution errors**: Examine output string to detect errors - check for "Error:" prefix or other error indicators
- Build errors? Fix and retry with write_code - check imports, types, syntax
- Parameter errors? Use discover_code_files to verify exact parameter names
- Import errors? Remember: generated functions are called directly, NOT imported
- File location errors? Check if you used standard I/O instead of workspace_tools - files must go to workspace!`
}

// BuildSystemPromptWithoutTools builds the system prompt without including tool descriptions
// This is useful when tools are passed via llmtypes.WithTools() to avoid prompt length issues
// toolStructureJSON is optional - if provided in code execution mode, it will replace {{TOOL_STRUCTURE}} placeholder
func BuildSystemPromptWithoutTools(prompts map[string][]mcp.Prompt, resources map[string][]mcp.Resource, mode interface{}, discoverResource bool, discoverPrompt bool, useCodeExecutionMode bool, toolStructureJSON string, logger loggerv2.Logger) string {
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

	// Build virtual tools section
	virtualToolsSection := buildVirtualToolsSection(useCodeExecutionMode)

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
	} else {
		corePrinciplesSection = `<core_principles>
When answering questions:
1. **Think** about what information/actions are needed
2. **Use tools** to gather information
3. **Provide helpful responses** based on tool results
</core_principles>`
	}

	// Build tool usage section based on mode
	var toolUsageSection string
	if useCodeExecutionMode {
		// Get code execution instructions and replace {{TOOL_STRUCTURE}} placeholder
		codeExecutionInstructions := GetCodeExecutionInstructions()

		// Replace {{TOOL_STRUCTURE}} placeholder with actual tool structure
		if toolStructureJSON != "" {
			toolStructureSection := "\n\n<available_code>\n" +
				"**AVAILABLE CODE FILES AND FUNCTIONS:**\n\n" +
				"The following code files and functions are available for use in your Go code. This structure shows all servers, custom tools, and their functions:\n\n" +
				"```json\n" +
				toolStructureJSON + "\n" +
				"```\n\n" +
				"**How to use:**\n" +
				"- Each server has a package name (e.g., \"aws_tools\", \"google_sheets_tools\")\n" +
				"- Each function has a name (e.g., \"GetDocument\", \"ListSpreadsheets\")\n" +
				"- Import the package and call the function in your Go code\n" +
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
				"3. Import the package (e.g., import \"aws_tools\") and call the function in your Go code\n" +
				"</available_code>\n"
			codeExecutionInstructions = strings.ReplaceAll(codeExecutionInstructions, ToolStructurePlaceholder, toolStructureSection)
		}

		toolUsageSection = `<code_usage>
` + codeExecutionInstructions + `
</code_usage>`
	} else {
		toolUsageSection = `<tool_usage>
**Guidelines:**
- Use tools when they can help answer the question
- Execute tools one at a time, waiting for results
- Use virtual tools for detailed prompts/resources when relevant
- Provide clear responses based on tool results

**Best Practices:**
- Use virtual tools to access detailed knowledge when relevant
- **If a tool call fails, retry with different arguments or parameters**
- **Try alternative approaches when tools return errors or unexpected results**
- **Modify search terms, file paths, or query parameters to overcome failures**
</tool_usage>`
	}

	// Build large output handling section (only for simple mode)
	var largeOutputHandlingSection string
	if useCodeExecutionMode {
		largeOutputHandlingSection = "" // Not available in code execution mode
	} else {
		largeOutputHandlingSection = `
LARGE TOOL OUTPUT HANDLING:
Large tool outputs (>1000 chars) are automatically saved to files. Use virtual tools to process them:
- 'read_large_output': Read specific characters from saved files
- 'search_large_output': Search for patterns in saved files  
- 'query_large_output': Execute jq queries on JSON files`
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
func buildVirtualToolsSection(useCodeExecutionMode bool) string {
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
	return VirtualToolsSectionTemplate
}
