package openapi

import (
	"fmt"
	"sort"
	"strings"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
	"gopkg.in/yaml.v3"
)

// CustomToolForOpenAPI represents a custom tool for OpenAPI spec generation.
type CustomToolForOpenAPI struct {
	Definition llmtypes.Tool
	Category   string
}

// GenerateServerOpenAPISpec generates an OpenAPI 3.0 YAML spec for a single MCP server's tools.
// The spec documents per-tool POST endpoints at /tools/mcp/{server}/{tool} with request schemas
// derived from the MCP tool's JSON Schema parameters.
func GenerateServerOpenAPISpec(
	serverName string,
	tools []llmtypes.Tool,
	baseURL string,
) ([]byte, error) {
	sanitizedServer := SanitizePathSegment(serverName)

	spec := buildBaseSpec(
		fmt.Sprintf("%s Tools API", serverName),
		fmt.Sprintf("API for interacting with %s MCP server tools via HTTP", serverName),
		baseURL,
	)

	paths := make(map[string]interface{})
	schemas := make(map[string]interface{})

	for _, tool := range tools {
		if tool.Function == nil {
			continue
		}

		toolName := tool.Function.Name
		sanitizedTool := SanitizePathSegment(toolName)
		path := fmt.Sprintf("/tools/mcp/%s/%s", sanitizedServer, sanitizedTool)
		operationID := ToolNameToOperationID(sanitizedServer, sanitizedTool)
		schemaName := SchemaNameFromToolName(toolName)

		// Convert tool parameters to OpenAPI schema
		requestSchema := JSONSchemaToOpenAPISchema(tool.Function.Parameters)
		schemas[schemaName] = requestSchema

		// Build path item
		paths[path] = map[string]interface{}{
			"post": buildOperation(
				tool.Function.Description,
				operationID,
				schemaName,
			),
		}
	}

	spec["paths"] = paths

	// Add schemas to components
	components := spec["components"].(map[string]interface{})
	components["schemas"] = schemas

	return yaml.Marshal(spec)
}

// GenerateCustomToolsOpenAPISpec generates an OpenAPI 3.0 YAML spec for custom tools in a category.
func GenerateCustomToolsOpenAPISpec(
	category string,
	tools map[string]CustomToolForOpenAPI,
	baseURL string,
) ([]byte, error) {
	spec := buildBaseSpec(
		fmt.Sprintf("%s Custom Tools API", category),
		fmt.Sprintf("API for interacting with %s custom tools via HTTP", category),
		baseURL,
	)

	paths := make(map[string]interface{})
	schemas := make(map[string]interface{})

	// Sort tool names for deterministic output
	toolNames := make([]string, 0, len(tools))
	for name := range tools {
		toolNames = append(toolNames, name)
	}
	sort.Strings(toolNames)

	for _, toolName := range toolNames {
		customTool := tools[toolName]
		if customTool.Definition.Function == nil {
			continue
		}

		sanitizedTool := SanitizePathSegment(toolName)
		path := fmt.Sprintf("/tools/custom/%s", sanitizedTool)
		operationID := ToolNameToOperationID("custom", sanitizedTool)
		schemaName := SchemaNameFromToolName(toolName)

		requestSchema := JSONSchemaToOpenAPISchema(customTool.Definition.Function.Parameters)
		schemas[schemaName] = requestSchema

		paths[path] = map[string]interface{}{
			"post": buildOperation(
				customTool.Definition.Function.Description,
				operationID,
				schemaName,
			),
		}
	}

	spec["paths"] = paths

	components := spec["components"].(map[string]interface{})
	components["schemas"] = schemas

	return yaml.Marshal(spec)
}

// buildBaseSpec creates the base OpenAPI 3.0 spec structure with security scheme.
func buildBaseSpec(title, description, baseURL string) map[string]interface{} {
	return map[string]interface{}{
		"openapi": "3.0.3",
		"info": map[string]interface{}{
			"title":       title,
			"description": description,
			"version":     "1.0",
		},
		"servers": []map[string]interface{}{
			{"url": baseURL},
		},
		"security": []map[string]interface{}{
			{"bearerAuth": []string{}},
		},
		"components": map[string]interface{}{
			"securitySchemes": map[string]interface{}{
				"bearerAuth": map[string]interface{}{
					"type":   "http",
					"scheme": "bearer",
				},
			},
			"responses": map[string]interface{}{
				"ToolResponse": map[string]interface{}{
					"description": "Tool execution result",
					"content": map[string]interface{}{
						"application/json": map[string]interface{}{
							"schema": map[string]interface{}{
								"type": "object",
								"properties": map[string]interface{}{
									"success": map[string]interface{}{"type": "boolean"},
									"result":  map[string]interface{}{"type": "string"},
									"error":   map[string]interface{}{"type": "string"},
								},
							},
						},
					},
				},
			},
		},
	}
}

// buildOperation creates an OpenAPI path operation for a tool endpoint.
func buildOperation(description, operationID, schemaName string) map[string]interface{} {
	op := map[string]interface{}{
		"operationId": operationID,
		"requestBody": map[string]interface{}{
			"required": true,
			"content": map[string]interface{}{
				"application/json": map[string]interface{}{
					"schema": map[string]interface{}{
						"$ref": fmt.Sprintf("#/components/schemas/%s", schemaName),
					},
				},
			},
		},
		"responses": map[string]interface{}{
			"200": map[string]interface{}{
				"$ref": "#/components/responses/ToolResponse",
			},
		},
	}

	// Only add summary if description is non-empty; keep it short
	if description != "" {
		// Truncate long descriptions for the summary field
		summary := description
		if idx := strings.Index(summary, "\n"); idx > 0 {
			summary = summary[:idx]
		}
		if len(summary) > 120 {
			summary = summary[:117] + "..."
		}
		op["summary"] = summary
	}

	return op
}
