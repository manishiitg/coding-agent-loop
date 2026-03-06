package mcpagent

import (
	"context"
	"fmt"
	"strings"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"

	"github.com/mark3labs/mcp-go/mcp"

	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"
)

// VirtualTool represents a virtual tool that can be called by the LLM
type VirtualTool struct {
	Name        string
	Description string
	Parameters  map[string]interface{}
	Handler     func(ctx context.Context, args map[string]interface{}) (string, error)
}

// CreateVirtualTools creates virtual tools for prompt and resource access
func (a *Agent) CreateVirtualTools() []llmtypes.Tool {
	var virtualTools []llmtypes.Tool

	// Check if MCP servers exist - get_prompt and get_resource require MCP servers
	hasMCPServers := len(a.Clients) > 0
	// Also check if NO_SERVERS is explicitly selected (overrides client count)
	if len(a.selectedServers) > 0 {
		// If selectedServers contains only "NO_SERVERS", then no MCP servers
		hasMCPServers = false
		for _, server := range a.selectedServers {
			if server != "NO_SERVERS" {
				hasMCPServers = true
				break
			}
		}
	}

	// Check if prompts actually exist (not just if servers exist)
	hasPrompts := false
	if hasMCPServers && a.prompts != nil {
		totalPrompts := 0
		for _, serverPrompts := range a.prompts {
			totalPrompts += len(serverPrompts)
		}
		hasPrompts = totalPrompts > 0
	}

	// Check if resources actually exist (not just if servers exist)
	hasResources := false
	if hasMCPServers && a.resources != nil {
		totalResources := 0
		for _, serverResources := range a.resources {
			totalResources += len(serverResources)
		}
		hasResources = totalResources > 0
	}

	// Only add get_prompt if prompts actually exist
	if hasPrompts {
		// Add get_prompt tool
		getPromptTool := llmtypes.Tool{
			Type: "function",
			Function: &llmtypes.FunctionDefinition{
				Name:        "get_prompt",
				Description: "Fetch the full content of a specific prompt by name and server",
				Parameters: llmtypes.NewParameters(map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"server": map[string]interface{}{
							"type":        "string",
							"description": "Server name",
						},
						"name": map[string]interface{}{
							"type":        "string",
							"description": "Prompt name (e.g., aws-msk, how-it-works)",
						},
					},
					"required": []string{"server", "name"},
				}),
			},
		}
		virtualTools = append(virtualTools, getPromptTool)
	}

	// Only add get_resource if resources actually exist
	if hasResources {
		// Add get_resource tool
		getResourceTool := llmtypes.Tool{
			Type: "function",
			Function: &llmtypes.FunctionDefinition{
				Name:        "get_resource",
				Description: "Fetch the content of a specific resource by URI and server. Only use URIs that are listed in the system prompt's 'AVAILABLE RESOURCES' section.",
				Parameters: llmtypes.NewParameters(map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"server": map[string]interface{}{
							"type":        "string",
							"description": "Server name",
						},
						"uri": map[string]interface{}{
							"type":        "string",
							"description": "Resource URI",
						},
					},
					"required": []string{"server", "uri"},
				}),
			},
		}
		virtualTools = append(virtualTools, getResourceTool)
	}

	// Add context offloading virtual tools if enabled
	// In code execution mode, context offloading tools are not needed
	// (the LLM writes code that calls HTTP endpoints directly)
	if !a.UseCodeExecutionMode {
		largeOutputTools := a.CreateLargeOutputVirtualTools()
		virtualTools = append(virtualTools, largeOutputTools...)
	}

	// Add get_api_spec tool — returns OpenAPI spec for specific tool(s)
	getAPISpecTool := llmtypes.Tool{
		Type: "function",
		Function: &llmtypes.FunctionDefinition{
			Name:        "get_api_spec",
			Description: "Get the OpenAPI specification for specific tool(s) on a server. The system prompt lists all available servers and tool names — use this to get the full API spec (endpoint, request schema, auth) for the tools you want to call.",
			Parameters: llmtypes.NewParameters(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"server_name": map[string]interface{}{
						"type":        "string",
						"description": "Server name from the available tools index (e.g., 'google_sheets', 'playwright').",
					},
					"tool_name": map[string]interface{}{
						"description": "Tool name(s) to get the API spec for. Pass a single string (e.g., 'search_issues') or an array of strings (e.g., ['create_spreadsheet', 'update_values']).",
					},
				},
				"required": []string{"server_name", "tool_name"},
			}),
		},
	}
	virtualTools = append(virtualTools, getAPISpecTool)

	return virtualTools
}

// HandleVirtualTool handles virtual tool execution
func (a *Agent) HandleVirtualTool(ctx context.Context, toolName string, args map[string]interface{}) (string, error) {
	switch toolName {
	case "get_prompt":
		return a.handleGetPrompt(ctx, args)
	case "get_resource":
		return a.handleGetResource(ctx, args)
	case "get_api_spec":
		return a.handleGetAPISpec(ctx, args)
	case "search_tools":
		return a.handleSearchTools(ctx, args)
	case "add_tool":
		return a.handleAddTool(ctx, args)
	case "show_all_tools":
		return a.handleShowAllTools(ctx, args)
	default:
		// Check if it's a context offloading virtual tool
		if a.EnableContextOffloading {
			return a.HandleLargeOutputVirtualTool(ctx, toolName, args)
		}
		return "", fmt.Errorf("unknown virtual tool: %s", toolName)
	}
}

// handleGetPrompt handles the get_prompt virtual tool
func (a *Agent) handleGetPrompt(ctx context.Context, args map[string]interface{}) (string, error) {
	server, ok := args["server"].(string)
	if !ok {
		return "", fmt.Errorf("server parameter is required")
	}

	name, ok := args["name"].(string)
	if !ok {
		return "", fmt.Errorf("name parameter is required")
	}

	// First, try to fetch from server (prioritize fresh data)
	if a.Clients != nil {
		if client, exists := a.Clients[server]; exists {
			promptResult, err := client.GetPrompt(ctx, name)
			if err == nil && promptResult != nil {
				// Extract content from messages
				if len(promptResult.Messages) > 0 {
					var contentParts []string
					for _, msg := range promptResult.Messages {
						if textContent, ok := msg.Content.(*mcp.TextContent); ok {
							contentParts = append(contentParts, textContent.Text)
						} else if textContent, ok := msg.Content.(mcp.TextContent); ok {
							contentParts = append(contentParts, textContent.Text)
						}
					}
					if len(contentParts) > 0 {
						content := strings.Join(contentParts, "\n")
						// Only return if we got actual content (not just metadata)
						if !strings.Contains(content, "Prompt loaded from") {
							return content, nil
						}
					}
				}
			}
		}
	}

	// If server fetch failed or returned metadata only, try cached data
	if a.prompts != nil {
		if serverPrompts, exists := a.prompts[server]; exists {
			for _, prompt := range serverPrompts {
				if prompt.Name == name {
					// Return the full content
					if strings.Contains(prompt.Description, "\n\nContent:\n") {
						parts := strings.Split(prompt.Description, "\n\nContent:\n")
						if len(parts) > 1 {
							return parts[1], nil
						}
					}
					return prompt.Description, nil
				}
			}
		}
	}

	return "", fmt.Errorf("prompt %s not found in server %s", name, server)
}

// handleGetResource handles the get_resource virtual tool
func (a *Agent) handleGetResource(ctx context.Context, args map[string]interface{}) (string, error) {
	server, ok := args["server"].(string)
	if !ok {
		return "", fmt.Errorf("server parameter is required")
	}

	uri, ok := args["uri"].(string)
	if !ok {
		return "", fmt.Errorf("uri parameter is required")
	}

	// First, try to fetch from server (prioritize fresh data)
	if a.Clients != nil {
		if client, exists := a.Clients[server]; exists {

			resourceResult, err := client.GetResource(ctx, uri)
			if err == nil && resourceResult != nil {
				// Extract content from resource using the same approach as existing code
				if len(resourceResult.Contents) > 0 {
					var contentParts []string
					for _, content := range resourceResult.Contents {
						contentStr := formatResourceContents(content)
						contentParts = append(contentParts, contentStr)
					}
					if len(contentParts) > 0 {
						content := strings.Join(contentParts, "\n")
						// Only return if we got actual content (not just metadata)
						if !strings.Contains(content, "Resource loaded from") && len(content) > 0 {
							return content, nil
						}
					}
				}
			}
		}
	}

	// If server fetch failed or returned metadata only, try cached data
	if a.resources != nil {
		if serverResources, exists := a.resources[server]; exists {

			for _, resource := range serverResources {
				if resource.URI == uri {

					// For cached resources, we need to fetch the actual content
					// Since we only have the resource metadata, we'll need to try fetching again
					// or return the description if it contains the content
					if resource.Description != "" {
						// Check if description contains actual content (not just metadata)
						if !strings.Contains(resource.Description, "Resource loaded from") && len(resource.Description) > 0 {
							return resource.Description, nil
						}
					}

					// If we have cached resource metadata but no content, try to fetch from server again
					// This handles cases where the resource exists but wasn't fetched during initialization
					if a.Clients != nil {
						if client, exists := a.Clients[server]; exists {
							resourceResult, err := client.GetResource(ctx, uri)
							if err == nil && resourceResult != nil && resourceResult.Contents != nil {
								var contentParts []string
								for _, content := range resourceResult.Contents {
									contentStr := formatResourceContents(content)
									contentParts = append(contentParts, contentStr)
								}
								if len(contentParts) > 0 {
									content := strings.Join(contentParts, "\n")
									return content, nil
								}
							}
						}
					}

					// If we still can't get content, return the resource description as fallback
					if resource.Description != "" {
						return resource.Description, nil
					}
				}
			}
		}
	}

	// If all attempts failed, provide a helpful error message
	errorMsg := fmt.Sprintf("resource %s not found in server %s. Available resources can be found in the system prompt's 'AVAILABLE RESOURCES' section", uri, server)
	if a.Logger != nil {
		a.Logger.Error("🔧 [get_resource] Resource not found", fmt.Errorf("%s", errorMsg), loggerv2.String("server", server), loggerv2.String("uri", uri))
	}
	return "", fmt.Errorf("resource %s not found in server %s. Available resources can be found in the system prompt's 'AVAILABLE RESOURCES' section", uri, server)
}

// formatResourceContents formats resource contents for display (copied from existing code)
func formatResourceContents(resource mcp.ResourceContents) string {
	switch r := resource.(type) {
	case *mcp.TextResourceContents:
		return r.Text
	case *mcp.BlobResourceContents:
		return fmt.Sprintf("[Binary data: %s]", r.MIMEType)
	default:
		return fmt.Sprintf("[Unknown resource type: %T]", resource)
	}
}
