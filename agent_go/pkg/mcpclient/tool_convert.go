package mcpclient

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/tmc/langchaingo/llms"

	"mcp-agent/agent_go/internal/utils"
)

// normalizeArrayParameters recursively normalizes JSON Schema properties to ensure
// all array types have an 'items' field (required by Gemini and some other LLM providers).
// This function fixes array parameters that are missing the items field by defaulting to string type.
func normalizeArrayParameters(schema map[string]interface{}) {
	if schema == nil {
		return
	}

	// Process properties if they exist
	if properties, ok := schema["properties"].(map[string]interface{}); ok {
		for _, propValue := range properties {
			if propMap, ok := propValue.(map[string]interface{}); ok {
				// Check if this property is an array type
				if propType, typeExists := propMap["type"].(string); typeExists && propType == "array" {
					// If items field is missing, add default string type
					if _, itemsExists := propMap["items"]; !itemsExists {
						propMap["items"] = map[string]interface{}{
							"type": "string",
						}
					} else {
						// If items exists, recursively normalize nested objects
						if itemsMap, ok := propMap["items"].(map[string]interface{}); ok {
							normalizeArrayParameters(itemsMap)
						}
					}
				} else if propType == "object" {
					// Recursively normalize nested objects
					normalizeArrayParameters(propMap)
				}
			}
		}
	}
}

// NormalizeLLMTools normalizes array parameters in llms.Tool objects to ensure
// all arrays have an 'items' field (required by Gemini and some other LLM providers).
// This function normalizes tools in-place, modifying their Parameters schema.
// Uses JSON round-trip to ensure structure preservation for langchaingo's Gemini conversion.
func NormalizeLLMTools(tools []llms.Tool) {
	fixedCount := 0
	totalMissing := 0
	totalFixed := 0

	for i := range tools {
		if tools[i].Function != nil && tools[i].Function.Parameters != nil {
			toolName := tools[i].Function.Name
			// Debug: log the actual type and structure
			paramsBytes, _ := json.Marshal(tools[i].Function.Parameters)
			if len(paramsBytes) > 200 {
				paramsBytes = paramsBytes[:200]
			}

			// Handle different parameter types
			switch params := tools[i].Function.Parameters.(type) {
			case map[string]interface{}:
				// CRITICAL: Use JSON round-trip to ensure structure is preserved for langchaingo
				// langchaingo's Gemini conversion may lose nested map structure during processing
				paramsBytes, err := json.Marshal(params)
				if err == nil {
					var normalizedParams map[string]interface{}
					if err := json.Unmarshal(paramsBytes, &normalizedParams); err == nil {
						// Debug: Check for specific problematic properties
						if props, ok := normalizedParams["properties"].(map[string]interface{}); ok {
							for propName := range props {
								if propName == "assignees" || propName == "labels" || propName == "files" || propName == "reviewers" {
									prop := props[propName]
									if propMap, ok := prop.(map[string]interface{}); ok {
										propType := propMap["type"]
										hasItems := propMap["items"] != nil
										fmt.Printf("[TOOL_NORMALIZE] Tool %s.%s: type=%v, hasItems=%v\n", toolName, propName, propType, hasItems)
									}
								}
							}
						}
						beforeFix := countMissingItems(normalizedParams)
						totalMissing += beforeFix
						if beforeFix > 0 {
							fmt.Printf("[TOOL_NORMALIZE] Tool %s has %d missing items fields\n", toolName, beforeFix)
						}
						normalizeArrayParameters(normalizedParams)
						afterFix := countMissingItems(normalizedParams)
						totalFixed += (beforeFix - afterFix)
						if afterFix < beforeFix {
							fixedCount++
							fmt.Printf("[TOOL_NORMALIZE] Fixed tool %s: %d -> %d missing items\n", toolName, beforeFix, afterFix)
						}
						// CRITICAL: Replace Parameters with normalized version from JSON round-trip
						// This ensures the structure is preserved when langchaingo processes it
						tools[i].Function.Parameters = normalizedParams
					}
				}
			default:
				// If Parameters is not a map, try to convert it
				// This can happen when Parameters is stored in different formats
				fmt.Printf("[TOOL_NORMALIZE] Tool %s has Parameters type: %T (not map[string]interface{})\n", toolName, params)
				paramsBytes, err := json.Marshal(params)
				if err == nil {
					var paramsMap map[string]interface{}
					if err := json.Unmarshal(paramsBytes, &paramsMap); err == nil {
						beforeFix := countMissingItems(paramsMap)
						totalMissing += beforeFix
						normalizeArrayParameters(paramsMap)
						afterFix := countMissingItems(paramsMap)
						totalFixed += (beforeFix - afterFix)
						if afterFix < beforeFix {
							fixedCount++
							fmt.Printf("[TOOL_NORMALIZE] Fixed tool %s (converted): %d -> %d missing items\n", toolName, beforeFix, afterFix)
						}
						// Update the Parameters with normalized version
						tools[i].Function.Parameters = paramsMap
					} else {
						fmt.Printf("[TOOL_NORMALIZE] Failed to unmarshal Parameters for tool %s: %v\n", toolName, err)
					}
				} else {
					fmt.Printf("[TOOL_NORMALIZE] Failed to marshal Parameters for tool %s: %v\n", toolName, err)
				}
			}
		}
	}
	if totalMissing > 0 {
		fmt.Printf("[TOOL_NORMALIZE] Summary: Found %d missing items fields, fixed %d, %d tools affected\n", totalMissing, totalFixed, fixedCount)
	} else {
		fmt.Printf("[TOOL_NORMALIZE] Summary: All tools already have items fields\n")
	}
}

// countMissingItems counts how many array properties are missing items field
func countMissingItems(schema map[string]interface{}) int {
	count := 0
	if schema == nil {
		return 0
	}
	if properties, ok := schema["properties"].(map[string]interface{}); ok {
		for _, propValue := range properties {
			if propMap, ok := propValue.(map[string]interface{}); ok {
				if propType, typeExists := propMap["type"].(string); typeExists && propType == "array" {
					if _, itemsExists := propMap["items"]; !itemsExists {
						count++
					}
				} else if propType == "object" {
					count += countMissingItems(propMap)
				}
			}
		}
	}
	return count
}

// ToolsAsLLM converts MCP tools to langchaingo llms.Tool format for Bedrock
func ToolsAsLLM(mcpTools []mcp.Tool) ([]llms.Tool, error) {
	llmTools := make([]llms.Tool, len(mcpTools))

	for i, tool := range mcpTools {
		// Convert ToolArgumentsSchema to proper JSON Schema
		schema := map[string]interface{}{
			"type": tool.InputSchema.Type,
		}

		// Only add properties if they exist and are not empty
		if tool.InputSchema.Properties != nil && len(tool.InputSchema.Properties) > 0 {
			schema["properties"] = tool.InputSchema.Properties
		} else {
			schema["properties"] = map[string]interface{}{}
		}

		// Only add required if they exist and are not empty
		if tool.InputSchema.Required != nil && len(tool.InputSchema.Required) > 0 {
			schema["required"] = tool.InputSchema.Required
		} else {
			schema["required"] = []string{}
		}

		// Add additional properties restriction for better validation
		schema["additionalProperties"] = false

		// Normalize array parameters to ensure all arrays have items field (required by Gemini)
		normalizeArrayParameters(schema)

		llmTools[i] = llms.Tool{
			Type: "function",
			Function: &llms.FunctionDefinition{
				Name:        tool.Name,
				Description: tool.Description,
				Parameters:  schema, // Now properly formatted JSON Schema
			},
		}
	}

	return llmTools, nil
}

// ToolDetailsAsLLM converts ToolDetail structs to langchaingo llms.Tool format
// This is used when we have ToolDetail objects (e.g., from cache) that need to be converted to LLM tools
func ToolDetailsAsLLM(toolDetails []ToolDetail) ([]llms.Tool, error) {
	llmTools := make([]llms.Tool, len(toolDetails))

	for i, toolDetail := range toolDetails {
		// Convert ToolDetail to proper JSON Schema format
		schema := map[string]interface{}{
			"type": "object",
		}

		// Only add properties if they exist and are not empty
		if toolDetail.Parameters != nil && len(toolDetail.Parameters) > 0 {
			schema["properties"] = toolDetail.Parameters
		} else {
			schema["properties"] = map[string]interface{}{}
		}

		// Only add required if they exist and are not empty
		if len(toolDetail.Required) > 0 {
			schema["required"] = toolDetail.Required
		} else {
			schema["required"] = []string{}
		}

		// Add additional properties restriction for better validation
		schema["additionalProperties"] = false

		// Normalize array parameters to ensure all arrays have items field (required by Gemini)
		normalizeArrayParameters(schema)

		llmTools[i] = llms.Tool{
			Type: "function",
			Function: &llms.FunctionDefinition{
				Name:        toolDetail.Name,
				Description: toolDetail.Description,
				Parameters:  schema, // Now properly formatted JSON Schema
			},
		}
	}

	return llmTools, nil
}

// ToolDetail represents detailed information about a single tool
type ToolDetail struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
	Required    []string               `json:"required,omitempty"`
}

// ToolResultAsString converts a tool result to a string representation
func ToolResultAsString(result *mcp.CallToolResult, logger utils.ExtendedLogger) string {
	if result == nil {
		return "Tool execution completed but no result returned"
	}

	// Join all content parts
	var parts []string
	for _, content := range result.Content {
		switch c := content.(type) {
		case *mcp.TextContent:
			// Try to parse JSON format {"type":"text","text":"..."}
			text := c.Text
			if strings.HasPrefix(strings.TrimSpace(text), "{") && strings.HasSuffix(strings.TrimSpace(text), "}") {
				var jsonResponse map[string]interface{}
				if err := json.Unmarshal([]byte(text), &jsonResponse); err == nil {
					// Check if it's a {"type":"text","text":"..."} format
					if responseType, ok := jsonResponse["type"].(string); ok && responseType == "text" {
						if responseText, ok := jsonResponse["text"].(string); ok {
							parts = append(parts, responseText)
							logger.Infof("[DEBUG] ToolResultAsString - Successfully parsed JSON response, extracted text: %s", responseText[:min(len(responseText), 100)])
							continue
						}
					}
				}
			}
			// If not JSON or not the expected format, use the text as-is
			parts = append(parts, text)
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

	// Debug logging
	logger.Infof("[DEBUG] ToolResultAsString - IsError: %v, Content: %s", result.IsError, joined)

	// If it's already marked as an error, return the error message
	if result.IsError {
		return fmt.Sprintf("Tool call failed with error: %s", joined)
	}

	// Check for implicit errors in the content (even when IsError is false)
	if strings.Contains(joined, "exit status") ||
		strings.Contains(joined, "Invalid choice") ||
		strings.Contains(joined, "usage:") ||
		strings.Contains(joined, "Error: Access denied") {
		logger.Infof("[DEBUG] ToolResultAsString - Detected implicit error: %s", joined)
		return fmt.Sprintf("Tool call failed with error: %s", joined)
	}

	return joined
}

// extractErrorMessage extracts error message from content array
func extractErrorMessage(content []mcp.Content) string {
	var errorParts []string
	for _, c := range content {
		if textContent, ok := c.(*mcp.TextContent); ok {
			errorParts = append(errorParts, textContent.Text)
		}
	}
	if len(errorParts) > 0 {
		// Join all error parts and preserve the detailed error message
		detailedError := strings.Join(errorParts, " ")

		// Check if this is a command execution error with exit status
		if strings.Contains(detailedError, "exit status") {
			return detailedError
		}

		// Check if this contains usage/help information
		if strings.Contains(detailedError, "usage:") || strings.Contains(detailedError, "Invalid choice") {
			return detailedError
		}

		// For other errors, preserve the full message
		return detailedError
	}
	return "Unknown error"
}

// formatResourceContents formats resource contents for display
func formatResourceContents(resource mcp.ResourceContents) string {
	switch r := resource.(type) {
	case *mcp.TextResourceContents:
		return r.Text
	case *mcp.BlobResourceContents:
		return fmt.Sprintf("[Binary data: %s]", r.MIMEType)
	default:
		if jsonBytes, err := json.Marshal(resource); err == nil {
			return string(jsonBytes)
		}
		return fmt.Sprintf("[Unknown resource type: %T]", resource)
	}
}

// ParseToolArguments parses JSON string arguments into a map for MCP tool calls
func ParseToolArguments(argsJSON string) (map[string]interface{}, error) {
	if argsJSON == "" {
		return make(map[string]interface{}), nil
	}

	var args map[string]interface{}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return nil, fmt.Errorf("failed to parse tool arguments: %w", err)
	}

	return args, nil
}

// extractTextFromJSON extracts the actual text content from JSON structure
func extractTextFromJSON(text string) string {
	// Check if the text starts with JSON structure like {"type":"text","text":"content"}
	if strings.HasPrefix(text, `{"type":"text","text":"`) {
		// Find the closing quote and brace
		startIndex := len(`{"type":"text","text":"`)
		endIndex := strings.LastIndex(text, `"}`)
		if endIndex > startIndex {
			// Extract the content between the quotes
			content := text[startIndex:endIndex]
			// Unescape the content
			content = strings.ReplaceAll(content, `\"`, `"`)
			content = strings.ReplaceAll(content, `\n`, "\n")
			content = strings.ReplaceAll(content, `\t`, "\t")
			return content
		}
	}
	// If not JSON structure, return the original text
	return text
}

// min returns the minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
