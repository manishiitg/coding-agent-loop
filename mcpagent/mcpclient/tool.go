package mcpclient

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"

	"mcp-agent/agent_go/internal/utils"
)

// PrintTools displays tools in a detailed, human-readable format
func PrintTools(tools []mcp.Tool, logger utils.ExtendedLogger) {
	logger.Infof("Available tools - count: %d", len(tools))
	for i, tool := range tools {
		logger.Infof("Tool %d: %s - %s", i+1, tool.Name, tool.Description)

		// Extract parameter information from schema
		schemaBytes, err := json.Marshal(tool.InputSchema)
		if err == nil {
			var schemaMap map[string]any
			if err := json.Unmarshal(schemaBytes, &schemaMap); err == nil {
				// Gather required fields
				requiredFields := map[string]bool{}
				if req, ok := schemaMap["required"].([]any); ok {
					for _, r := range req {
						if s, ok := r.(string); ok {
							requiredFields[s] = true
						}
					}
				}

				// Walk through properties
				if props, ok := schemaMap["properties"].(map[string]any); ok {
					for propName, propDetails := range props {
						if propMap, ok := propDetails.(map[string]any); ok {
							required := requiredFields[propName]
							description := ""
							if desc, ok := propMap["description"].(string); ok {
								description = desc
							}
							logger.Infof("Tool parameter - Tool: %s, Param: %s, Required: %v, Description: %s",
								tool.Name, propName, required, description)
						}
					}
				}
			}
		}
	}
}

// PrintResources displays resources in a detailed, human-readable format
func PrintResources(resources []mcp.Resource, logger utils.ExtendedLogger) {
	logger.Infof("Available resources - count: %d", len(resources))
	for i, resource := range resources {
		logger.Infof("Resource %d: %s (%s) - %s", i+1, resource.Name, resource.URI, resource.Description)
	}
}

// PrintPrompts displays prompts in a detailed, human-readable format
func PrintPrompts(prompts []mcp.Prompt, logger utils.ExtendedLogger) {
	logger.Infof("Available prompts - count: %d", len(prompts))
	for i, prompt := range prompts {
		logger.Infof("Prompt %d: %s - %s", i+1, prompt.Name, prompt.Description)

		// Print prompt arguments if available
		for j, arg := range prompt.Arguments {
			logger.Infof("Prompt argument - Prompt: %s, Arg %d: %s - %s",
				prompt.Name, j+1, arg.Name, arg.Description)
		}
	}
}

// PrintToolResult displays a tool result in a human-readable format
func PrintToolResult(result *mcp.CallToolResult, logger utils.ExtendedLogger) {
	if result == nil {
		logger.Infof("Tool execution completed but no result returned")
		return
	}

	logger.Infof("Tool Result (IsError: %v):", result.IsError)

	// Join all content parts
	var parts []string
	for _, content := range result.Content {
		switch c := content.(type) {
		case *mcp.TextContent:
			parts = append(parts, c.Text)
		case *mcp.ImageContent:
			parts = append(parts, fmt.Sprintf("[Image: %s]", c.Data))
		case *mcp.EmbeddedResource:
			parts = append(parts, fmt.Sprintf("[Resource: %s]", formatResourceContents(c.Resource)))
		default:
			// For any other content type, try to marshal to JSON
			if jsonBytes, err := json.Marshal(content); err == nil {
				parts = append(parts, string(jsonBytes))
			} else {
				parts = append(parts, fmt.Sprintf("[Unknown content type: %T]", content))
			}
		}
	}

	joined := strings.Join(parts, "\n")
	logger.Infof("%s", joined)
}

// PrintResourceResult displays a resource result in a human-readable format
func PrintResourceResult(result *mcp.ReadResourceResult, logger utils.ExtendedLogger) {
	if result == nil {
		logger.Infof("Resource read completed but no result returned")
		return
	}

	logger.Infof("Resource Result:")
	logger.Infof("Contents (%d):", len(result.Contents))
	for i, content := range result.Contents {
		logger.Infof("  Content %d: %s", i+1, formatResourceContents(content))
	}
}

// PrintPromptResult displays a prompt result in a human-readable format
func PrintPromptResult(result *mcp.GetPromptResult, logger utils.ExtendedLogger) {
	if result == nil {
		logger.Infof("Prompt retrieval completed but no result returned")
		return
	}

	logger.Infof("Prompt Result:")
	logger.Infof("Description: %s", result.Description)
	logger.Infof("Messages (%d):", len(result.Messages))

	for i, msg := range result.Messages {
		logger.Infof("  Message %d:", i+1)
		logger.Infof("    Role: %s", msg.Role)
		logger.Infof("    Content: %s", formatContent(msg.Content))
	}
}

// formatContent formats content for display
func formatContent(content mcp.Content) string {
	switch c := content.(type) {
	case *mcp.TextContent:
		return c.Text
	case *mcp.ImageContent:
		return fmt.Sprintf("[Image: %s]", c.Data)
	case *mcp.EmbeddedResource:
		return fmt.Sprintf("[Resource: %s]", formatResourceContents(c.Resource))
	default:
		if jsonBytes, err := json.Marshal(content); err == nil {
			return string(jsonBytes)
		}
		return fmt.Sprintf("[Unknown content type: %T]", content)
	}
}
