// Package external provides prompt building utilities for MCP agent system prompts.
//
// This package contains functions that help construct formatted sections for system prompts,
// including tools, prompts, resources, and virtual tools. These functions are designed
// to work with the MCP agent architecture and provide human-readable markdown formatting
// for better LLM comprehension.
//
// The functions in this package are primarily used internally by the external agent
// implementation to build comprehensive system prompts that include:
//   - Available tools organized by MCP server
//   - Available prompts with descriptions
//   - Available resources with URIs and descriptions
//   - Virtual tools for on-demand content access
//
// All functions return formatted strings that can be directly included in system prompts
// or used for debugging and documentation purposes.
package external

import (
	"fmt"
	"strings"

	mcpagent "mcpagent/agent"

	"github.com/mark3labs/mcp-go/mcp"
)

// buildToolsSectionFromAgent builds a formatted tools section string from the agent's available tools.
//
// This function organizes tools by their MCP server and creates a human-readable markdown format.
// It uses the agent's tool-to-server mapping to group tools correctly.
//
// Parameters:
//   - agent: The MCP agent containing tools and server mappings
//
// Returns:
//   - A formatted string showing tools grouped by server, or "No tools available." if none exist
//
// Example output:
//
//	**aws-server**:
//	- aws_cli_query
//	- aws_cloudwatch
//	**github-server**:
//	- github_create_issue
func buildToolsSectionFromAgent(agent *mcpagent.Agent) string {
	if len(agent.Tools) == 0 {
		return "No tools available."
	}

	var sections []string
	toolsByServer := make(map[string][]string)

	// Use the actual MCP server names from toolToServer mapping
	toolToServer := agent.GetToolToServer()

	for _, tool := range agent.Tools {
		if tool.Function == nil {
			continue
		}

		// Get the actual server name from the toolToServer mapping
		serverName := "unknown"
		if mappedServer, exists := toolToServer[tool.Function.Name]; exists {
			serverName = mappedServer
		}

		toolsByServer[serverName] = append(toolsByServer[serverName], tool.Function.Name)
	}

	// Build sections for each server
	for serverName, toolNames := range toolsByServer {
		section := fmt.Sprintf("**%s**:\n", serverName)
		for _, toolName := range toolNames {
			section += fmt.Sprintf("- %s\n", toolName)
		}
		sections = append(sections, section)
	}

	return strings.Join(sections, "\n")
}

// buildPromptsSection builds a formatted prompts section string for inclusion in system prompts.
//
// This function creates a human-readable markdown format showing available prompts organized by server.
// It filters out empty server prompt lists and formats each prompt with its name and description.
//
// Parameters:
//   - prompts: A map where keys are server names and values are arrays of MCP prompts
//
// Returns:
//   - A formatted string showing prompts grouped by server, or empty string if no prompts exist
//
// Example output:
//
//	**aws-server**:
//	- cost_analysis: Analyze AWS costs across services
//	- resource_optimization: Find cost optimization opportunities
//	**github-server**:
//	- issue_template: Standard issue creation template
func buildPromptsSection(prompts map[string][]mcp.Prompt) string {
	if len(prompts) == 0 {
		return ""
	}

	var sections []string
	for serverName, serverPrompts := range prompts {
		if len(serverPrompts) == 0 {
			continue
		}
		section := fmt.Sprintf("**%s**:\n", serverName)
		for _, prompt := range serverPrompts {
			section += fmt.Sprintf("- %s: %s\n", prompt.Name, prompt.Description)
		}
		sections = append(sections, section)
	}

	if len(sections) == 0 {
		return ""
	}

	return strings.Join(sections, "\n")
}

// buildResourcesSection builds a formatted resources section string for inclusion in system prompts.
//
// This function creates a human-readable markdown format showing available resources organized by server.
// It filters out empty server resource lists and formats each resource with its URI and description.
//
// Parameters:
//   - resources: A map where keys are server names and values are arrays of MCP resources
//
// Returns:
//   - A formatted string showing resources grouped by server, or empty string if no resources exist
//
// Example output:
//
//	**fileserver**:
//	- file://config.json: Application configuration file
//	- file://logs.txt: System log files
//	**database**:
//	- db://users: User account information
func buildResourcesSection(resources map[string][]mcp.Resource) string {
	if len(resources) == 0 {
		return ""
	}

	var sections []string
	for serverName, serverResources := range resources {
		if len(serverResources) == 0 {
			continue
		}
		section := fmt.Sprintf("**%s**:\n", serverName)
		for _, resource := range serverResources {
			section += fmt.Sprintf("- %s: %s\n", resource.URI, resource.Description)
		}
		sections = append(sections, section)
	}

	if len(sections) == 0 {
		return ""
	}

	return strings.Join(sections, "\n")
}

// buildVirtualToolsSection builds a formatted virtual tools section string for inclusion in system prompts.
//
// This function returns a static description of the virtual tools available to the agent.
// Virtual tools provide on-demand access to prompts, resources, and large output processing.
//
// Returns:
//   - A formatted string describing all available virtual tools with their purposes
//
// Virtual tools include:
//   - get_prompt: Fetch full content of specific prompts by server and name
//   - get_resource: Fetch content of specific resources by server and URI
//   - read_large_output: Read specific character ranges from large tool output files
//   - search_large_output: Search for patterns in large tool output files
//   - query_large_output: Execute jq queries on large JSON tool output files
//
// Example output:
//
//	**Virtual Tools:**
//	- get_prompt: Fetch full content of a specific prompt
//	- get_resource: Fetch content of a specific resource
//	- read_large_output: Read specific characters from large tool output files
//	- search_large_output: Search for patterns in large tool output files
//	- query_large_output: Execute jq queries on large JSON tool output files
func buildVirtualToolsSection() string {
	return `**Virtual Tools:**
- get_prompt: Fetch full content of a specific prompt
- get_resource: Fetch content of a specific resource
- read_large_output: Read specific characters from large tool output files
- search_large_output: Search for patterns in large tool output files
- query_large_output: Execute jq queries on large JSON tool output files`
}
