package mcpagent

import (
	"context"
	"fmt"
	"strings"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"

	"github.com/mark3labs/mcp-go/mcp"

	loggerv2 "mcpagent/logger/v2"
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

	// Only add get_prompt and get_resource if MCP servers exist
	if hasMCPServers {
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

	// Add large output virtual tools if enabled
	// In code execution mode, we don't support large output tools (they don't work in subprocess)
	// We handle large outputs via truncation in write_code instead
	if !a.UseCodeExecutionMode {
		largeOutputTools := a.CreateLargeOutputVirtualTools()
		virtualTools = append(virtualTools, largeOutputTools...)
	}

	// Add discover_code_files tool (requires both server_name and tool_name - returns actual Go code)
	// Note: discover_code_structure has been removed - tool structure is now automatically included in system prompt
	discoverCodeFilesTool := llmtypes.Tool{
		Type: "function",
		Function: &llmtypes.FunctionDefinition{
			Name:        "discover_code_files",
			Description: "Get Go source code for a specific tool from a specific server. Both server_name and tool_name are required.",
			Parameters: llmtypes.NewParameters(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"server_name": map[string]interface{}{
						"type":        "string",
						"description": "MCP server name (e.g., 'aws', 'gdrive', 'google_sheets', 'virtual_tools', 'custom_tools').",
					},
					"tool_name": map[string]interface{}{
						"type":        "string",
						"description": "Tool name (e.g., 'GetDocument', 'resolve-library-id'). The tool name will be converted to snake_case filename.",
					},
				},
				"required": []string{"server_name", "tool_name"},
			}),
		},
	}
	virtualTools = append(virtualTools, discoverCodeFilesTool)

	// Add write_code tool
	writeCodeTool := llmtypes.Tool{
		Type: "function",
		Function: &llmtypes.FunctionDefinition{
			Name:        "write_code",
			Description: "Write Go code to workspace. Code can import generated tool packages from 'generated/' directory. Filename is automatically generated. Optional CLI arguments can be passed to the program via os.Args.",
			Parameters: llmtypes.NewParameters(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"code": map[string]interface{}{
						"type":        "string",
						"description": "Go source code to write",
					},
					"args": map[string]interface{}{
						"type":        "array",
						"items":       map[string]interface{}{"type": "string"},
						"description": "Optional array of command-line arguments to pass to the Go program. Accessible via os.Args[1], os.Args[2], etc. (os.Args[0] is the program name).",
					},
				},
				"required": []string{"code"},
			}),
		},
	}
	virtualTools = append(virtualTools, writeCodeTool)

	return virtualTools
}

// HandleVirtualTool handles virtual tool execution
func (a *Agent) HandleVirtualTool(ctx context.Context, toolName string, args map[string]interface{}) (string, error) {
	switch toolName {
	case "get_prompt":
		return a.handleGetPrompt(ctx, args)
	case "get_resource":
		return a.handleGetResource(ctx, args)
	case "discover_code_files":
		return a.handleDiscoverCodeFiles(ctx, args)
	case "write_code":
		return a.handleWriteCode(ctx, args)
	default:
		// Check if it's a large output virtual tool
		if a.EnableLargeOutputVirtualTools {
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
