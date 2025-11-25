package prompt

import (
	"fmt"
	"strings"
	"time"

	"mcp-agent/agent_go/internal/utils"

	"github.com/mark3labs/mcp-go/mcp"
)

// BuildSystemPromptWithoutTools builds the system prompt without including tool descriptions
// This is useful when tools are passed via llmtypes.WithTools() to avoid prompt length issues
// toolStructureJSON is optional - if provided in code execution mode, it will replace {{TOOL_STRUCTURE}} placeholder
func BuildSystemPromptWithoutTools(prompts map[string][]mcp.Prompt, resources map[string][]mcp.Resource, mode interface{}, discoverResource bool, discoverPrompt bool, useCodeExecutionMode bool, toolStructureJSON string, logger utils.ExtendedLogger) string {
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

	// Always use Simple system prompt template
	prompt := SystemPromptTemplate

	// Replace placeholders (tools are passed via llmtypes.WithTools())
	// prompt = strings.ReplaceAll(prompt, "{{TOOLS_SECTION}}", "Tools are available via llmtypes.WithTools() - see available tools in the tools array")
	prompt = strings.ReplaceAll(prompt, PromptsSectionPlaceholder, promptsSection)
	prompt = strings.ReplaceAll(prompt, ResourcesSectionPlaceholder, resourcesSection)
	prompt = strings.ReplaceAll(prompt, VirtualToolsSectionPlaceholder, virtualToolsSection)
	prompt = strings.ReplaceAll(prompt, CurrentDatePlaceholder, currentDate)
	prompt = strings.ReplaceAll(prompt, CurrentTimePlaceholder, currentTime)

	// Note: {{TOOL_STRUCTURE}} placeholder will be replaced later in code execution mode
	// after the tool_usage section is replaced, so it appears in the right location

	// In code execution mode, update core_principles and tool_usage section and remove large output handling
	if useCodeExecutionMode {
		// Replace core_principles section for code execution mode
		codeExecutionCorePrinciples := `<core_principles>
When answering questions:
1. **Think** about what information/actions are needed
2. **Write code** to gather information and perform actions
3. **Provide helpful responses** based on execution results
</core_principles>`
		prompt = strings.ReplaceAll(prompt, `<core_principles>
When answering questions:
1. **Think** about what information/actions are needed
2. **Use tools** to gather information
3. **Provide helpful responses** based on tool results
</core_principles>`, codeExecutionCorePrinciples)

		// Replace tool_usage section with code execution mode guidance
		// The {{TOOL_STRUCTURE}} placeholder will be replaced after this
		codeExecutionToolUsage := `<code_usage>
**CODE EXECUTION MODE - Access MCP Servers via Go Code:**

{{TOOL_STRUCTURE}}

**📋 Workflow:**
1. **Review** available code packages in the structure above
2. **Discover code FIRST**: Use discover_code_files to get exact function signatures before writing any code
3. **Write** Go code using write_code that calls the generated tool functions
4. **Execute** and get results

**⚠️ CRITICAL - Code Requirements:**
- ✅ **MUST have package main declaration**
- ✅ **Code runs as separate process via 'go run'**
- ✅ **Generated tool functions make HTTP calls to MCP API**
- ✅ **Use fmt.Println() to output results**
- ❌ **DO NOT try to import mcp-agent internal packages** - not accessible

**🌐 MCP API Architecture:**
Your code runs independently and calls MCP tools via HTTP API:
- API Endpoint: http://localhost:8000/api/mcp/execute
- Generated functions automatically make POST requests
- Environment variables: MCP_API_URL, MCP_SERVER_NAME

**Generated Function Pattern:**
Each MCP tool is generated as a Go function that makes HTTP calls:

  // Generated function example (in aws_tools package)
  func GetDocument(params map[string]interface{}) (string, error) {
      apiURL := os.Getenv("MCP_API_URL")
      if apiURL == "" {
          apiURL = "http://localhost:8000"
      }

      reqBody, _ := json.Marshal(map[string]interface{}{
          "server": os.Getenv("MCP_SERVER_NAME"),
          "tool":   "get_document",
          "args":   params,
      })

      resp, err := http.Post(apiURL+"/api/mcp/execute", "application/json", bytes.NewBuffer(reqBody))
      // ... parse response ...
      return result.Result, nil
  }

**💡 You Can Write Logic:**
- Use **if/else** to make decisions based on results
- Call **multiple functions** in sequence
- **Combine different servers** in one code block
- Use **loops** to process data

**Basic Example:**
  package main

  import (
      "fmt"
      "os"
  )

  func main() {
      // Set environment for generated functions
      os.Setenv("MCP_SERVER_NAME", "aws")

      // Call generated function
      params := map[string]interface{}{
          "document_id": "doc123",
      }
      output, err := GetDocument(params)
      if err != nil {
          fmt.Printf("Error: %%v\n", err)
          return
      }
      fmt.Printf("Result: %%s\n", output)
  }

**Example with Multiple Servers:**
  package main

  import (
      "fmt"
      "os"
      "strings"
  )

  func main() {
      // Call AWS tool
      os.Setenv("MCP_SERVER_NAME", "aws")
      data, err := GetCosts(map[string]interface{}{})
      if err != nil {
          fmt.Printf("Error: %%v\n", err)
          return
      }
      fmt.Printf("Costs: %%s\n", data)

      // Call Slack tool if costs are high
      if strings.Contains(data, "high") {
          os.Setenv("MCP_SERVER_NAME", "slack")
          params := map[string]interface{}{
              "channel": "alerts",
              "text": "High costs detected",
          }
          alert, _ := SendMessage(params)
          fmt.Printf("Alert: %%s\n", alert)
      }
  }

**HTTP API Direct Usage (if needed):**
You can also make direct HTTP calls to the MCP API:

  import (
      "bytes"
      "encoding/json"
      "net/http"
  )

  func callMCPTool(server, tool string, args map[string]interface{}) (string, error) {
      reqBody, _ := json.Marshal(map[string]interface{}{
          "server": server,
          "tool":   tool,
          "args":   args,
      })

      resp, err := http.Post("http://localhost:8000/api/mcp/execute",
          "application/json", bytes.NewBuffer(reqBody))
      if err != nil {
          return "", err
      }
      defer resp.Body.Close()

      var result struct {
          Success bool
          Result  string
          Error   string
      }
      json.NewDecoder(resp.Body).Decode(&result)

      if !result.Success {
          return "", fmt.Errorf(result.Error)
      }
      return result.Result, nil
  }

**🔧 Error Recovery:**
Build errors? Fix and retry with write_code - check imports, types, syntax
</code_usage>`
		prompt = strings.ReplaceAll(prompt, `<tool_usage>
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
</tool_usage>`, codeExecutionToolUsage)

		// Replace {{TOOL_STRUCTURE}} placeholder in the code execution tool usage section
		// This happens after the tool_usage replacement so the placeholder is in the right place
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
				"</available_code>\n"
			prompt = strings.ReplaceAll(prompt, ToolStructurePlaceholder, toolStructureSection)
		} else {
			// Remove placeholder if no structure available
			prompt = strings.ReplaceAll(prompt, ToolStructurePlaceholder, "")
		}

		// Remove large output handling section (not available in code execution mode)
		prompt = strings.ReplaceAll(prompt, `
LARGE TOOL OUTPUT HANDLING:
Large tool outputs (>1000 chars) are automatically saved to files. Use virtual tools to process them:
- 'read_large_output': Read specific characters from saved files
- 'search_large_output': Search for patterns in saved files  
- 'query_large_output': Execute jq queries on JSON files
`, "")

		// Also remove the <virtual_tools> wrapper tags in code execution mode
		prompt = strings.ReplaceAll(prompt, "<virtual_tools>", "")
		prompt = strings.ReplaceAll(prompt, "</virtual_tools>", "")
	}

	return prompt
}

// buildPromptsSectionWithPreviews builds the prompts section with previews
func buildPromptsSectionWithPreviews(prompts map[string][]mcp.Prompt, logger utils.ExtendedLogger) string {
	// Count total prompts across all servers
	totalPrompts := 0
	for _, serverPrompts := range prompts {
		totalPrompts += len(serverPrompts)
	}

	if totalPrompts == 0 {
		logger.Info("🔍 No prompts found for preview generation - skipping prompts section")
		return ""
	}

	logger.Info("🔍 Building prompts section with previews", map[string]interface{}{
		"server_count":  len(prompts),
		"total_prompts": totalPrompts,
	})

	var promptsList []string
	for serverName, serverPrompts := range prompts {
		if len(serverPrompts) == 0 {
			// Skip servers with no prompts
			continue
		}

		logger.Info("📝 Processing server prompts", map[string]interface{}{
			"server_name":  serverName,
			"prompt_count": len(serverPrompts),
		})

		promptsList = append(promptsList, fmt.Sprintf("%s:", serverName))
		for _, prompt_item := range serverPrompts {
			name := prompt_item.Name
			description := prompt_item.Description

			logger.Debug("📄 Processing prompt", map[string]interface{}{
				"server_name":        serverName,
				"prompt_name":        name,
				"description_length": len(description),
			})

			// Extract preview (first 10 lines) from the description
			preview := extractPromptPreview(description)

			// Format as preview with name and first few lines
			promptsList = append(promptsList, fmt.Sprintf("  - %s: %s", name, preview))
		}
	}

	// Double-check: if no prompts were actually added, return empty
	if len(promptsList) == 0 {
		logger.Info("🔍 No actual prompts found after processing - skipping prompts section")
		return ""
	}

	promptsText := strings.Join(promptsList, "\n")
	logger.Info("✅ Prompts section built", map[string]interface{}{
		"total_length": len(promptsText),
		"prompt_lines": len(promptsList),
	})

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
  Imports: Use "mcp-agent/agent_go/generated/{package}_tools"
  Must return a string (not print to stdout)`
	}
	return VirtualToolsSectionTemplate
}
