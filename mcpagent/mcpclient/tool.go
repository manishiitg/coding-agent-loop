package mcpclient

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"

	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"
)

// PrintTools displays tools in a detailed, human-readable format (Debug level only)
func PrintTools(tools []mcp.Tool, logger loggerv2.Logger) {
	logger.Debug("Available tools", loggerv2.Int("count", len(tools)))
	for i, tool := range tools {
		logger.Debug("Tool",
			loggerv2.Int("index", i+1),
			loggerv2.String("name", tool.Name),
			loggerv2.String("description", tool.Description))
	}
}

// PrintResources displays resources in a detailed, human-readable format (Debug level only)
func PrintResources(resources []mcp.Resource, logger loggerv2.Logger) {
	logger.Debug("Available resources", loggerv2.Int("count", len(resources)))
	for i, resource := range resources {
		logger.Debug("Resource",
			loggerv2.Int("index", i+1),
			loggerv2.String("name", resource.Name),
			loggerv2.String("uri", resource.URI),
			loggerv2.String("description", resource.Description))
	}
}

// PrintPrompts displays prompts in a detailed, human-readable format (Debug level only)
func PrintPrompts(prompts []mcp.Prompt, logger loggerv2.Logger) {
	logger.Debug("Available prompts", loggerv2.Int("count", len(prompts)))
	for i, prompt := range prompts {
		logger.Debug("Prompt",
			loggerv2.Int("index", i+1),
			loggerv2.String("name", prompt.Name),
			loggerv2.String("description", prompt.Description))
	}
}

// PrintToolResult displays a tool result in a human-readable format (Debug level only)
func PrintToolResult(result *mcp.CallToolResult, logger loggerv2.Logger) {
	if result == nil {
		logger.Debug("Tool execution completed but no result returned")
		return
	}

	logger.Debug("Tool Result", loggerv2.Any("is_error", result.IsError))

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
	logger.Debug("Tool result content", loggerv2.String("content", joined))
}

// PrintResourceResult displays a resource result in a human-readable format (Debug level only)
func PrintResourceResult(result *mcp.ReadResourceResult, logger loggerv2.Logger) {
	if result == nil {
		logger.Debug("Resource read completed but no result returned")
		return
	}

	logger.Debug("Resource Result", loggerv2.Int("contents_count", len(result.Contents)))
	for i, content := range result.Contents {
		logger.Debug("Resource content",
			loggerv2.Int("index", i+1),
			loggerv2.String("content", formatResourceContents(content)))
	}
}

// PrintPromptResult displays a prompt result in a human-readable format (Debug level only)
func PrintPromptResult(result *mcp.GetPromptResult, logger loggerv2.Logger) {
	if result == nil {
		logger.Debug("Prompt retrieval completed but no result returned")
		return
	}

	logger.Debug("Prompt Result",
		loggerv2.String("description", result.Description),
		loggerv2.Int("messages_count", len(result.Messages)))

	for i, msg := range result.Messages {
		logger.Debug("Prompt message",
			loggerv2.Int("index", i+1),
			loggerv2.String("role", string(msg.Role)),
			loggerv2.String("content", formatContent(msg.Content)))
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
