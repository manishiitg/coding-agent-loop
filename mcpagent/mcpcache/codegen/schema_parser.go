package codegen

import (
	"strings"
)

// GoStruct represents a generated Go struct
type GoStruct struct {
	Name   string
	Fields []GoField
}

// GoField represents a field in a Go struct
type GoField struct {
	Name        string
	Type        string
	JSONTag     string
	Required    bool
	Description string // Description from JSON schema
}

// ParseJSONSchemaToGoStruct converts JSON schema properties to Go struct fields
func ParseJSONSchemaToGoStruct(toolName string, schema map[string]interface{}) (*GoStruct, error) {
	// Generate struct name: {ToolName}Params (capitalized for export)
	// Use sanitizeFunctionName to get PascalCase, then add Params
	baseName := sanitizeFunctionName(toolName)
	structName := baseName + "Params"

	// Get properties
	properties, ok := schema["properties"].(map[string]interface{})
	if !ok {
		// No properties, return empty struct
		return &GoStruct{
			Name:   structName,
			Fields: []GoField{},
		}, nil
	}

	// Get required fields
	requiredMap := make(map[string]bool)
	if required, ok := schema["required"].([]interface{}); ok {
		for _, req := range required {
			if reqStr, ok := req.(string); ok {
				requiredMap[reqStr] = true
			}
		}
	}

	// Convert properties to Go fields
	fields := make([]GoField, 0, len(properties))
	for propName, propValue := range properties {
		propMap, ok := propValue.(map[string]interface{})
		if !ok {
			continue
		}

		// Get type
		goType := getGoType(propMap, requiredMap[propName])

		// Get description
		description := ""
		if desc, ok := propMap["description"].(string); ok {
			description = desc
		}

		// Sanitize field name
		fieldName := sanitizeIdentifier(propName)
		// Capitalize first letter for exported field
		if len(fieldName) > 0 {
			fieldName = strings.ToUpper(fieldName[:1]) + fieldName[1:]
		}

		fields = append(fields, GoField{
			Name:        fieldName,
			Type:        goType,
			JSONTag:     propName,
			Required:    requiredMap[propName],
			Description: description,
		})
	}

	return &GoStruct{
		Name:   structName,
		Fields: fields,
	}, nil
}

// getGoType converts JSON schema type to Go type
func getGoType(propMap map[string]interface{}, required bool) string {
	// Get type from schema
	typeVal, ok := propMap["type"].(string)
	if !ok {
		// Default to interface{} if type not specified
		return "interface{}"
	}

	var goType string
	switch typeVal {
	case "string":
		goType = "string"
	case "integer":
		goType = "int"
	case "number":
		goType = "float64"
	case "boolean":
		goType = "bool"
	case "array":
		// Handle array types
		items, ok := propMap["items"].(map[string]interface{})
		if ok {
			itemType := getGoType(items, true)
			goType = "[]" + itemType
		} else {
			goType = "[]interface{}"
		}
	case "object":
		// For objects, use map[string]interface{} or interface{}
		goType = "map[string]interface{}"
	default:
		goType = "interface{}"
	}

	// If field is optional (not required), make it a pointer
	if !required {
		return "*" + goType
	}

	return goType
}

// sanitizeIdentifier converts a string to a valid Go identifier
func sanitizeIdentifier(name string) string {
	// Replace invalid characters with underscores
	result := strings.Builder{}
	for i, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			result.WriteRune(r)
		} else {
			result.WriteRune('_')
		}
		// Don't start with a number
		if i == 0 && r >= '0' && r <= '9' {
			result.Reset()
			result.WriteString("_")
			result.WriteRune(r)
		}
	}

	identifier := result.String()

	// Handle empty identifier
	if identifier == "" {
		identifier = "_"
	}

	// Check for Go reserved words
	reservedWords := map[string]bool{
		"break": true, "case": true, "chan": true, "const": true, "continue": true,
		"default": true, "defer": true, "else": true, "fallthrough": true, "for": true,
		"func": true, "go": true, "goto": true, "if": true, "import": true,
		"interface": true, "map": true, "package": true, "range": true, "return": true,
		"select": true, "struct": true, "switch": true, "type": true, "var": true,
	}

	if reservedWords[identifier] {
		identifier = identifier + "_"
	}

	return identifier
}

// sanitizeFunctionName converts tool name to valid Go function name
func sanitizeFunctionName(toolName string) string {
	// Replace hyphens and underscores with spaces, then split
	// This handles both "resolve-library-id" and "aws_get_document"
	normalized := strings.ReplaceAll(toolName, "-", "_")
	parts := strings.Split(normalized, "_")
	result := strings.Builder{}
	for _, part := range parts {
		if len(part) > 0 {
			result.WriteString(strings.ToUpper(part[:1]) + part[1:])
		}
	}
	return result.String()
}

// GetPackageName converts server name to package name
func GetPackageName(serverName string) string {
	// Sanitize server name and add _tools suffix
	return sanitizeIdentifier(serverName) + "_tools"
}

// ToolNameToSnakeCase converts a tool name to snake_case for file names
// Handles both kebab-case (resolve-library-id) and camelCase (getDocument)
func ToolNameToSnakeCase(toolName string) string {
	// First normalize: replace hyphens with underscores
	normalized := strings.ReplaceAll(toolName, "-", "_")

	// If already in snake_case, return as is
	if strings.Contains(normalized, "_") {
		return strings.ToLower(normalized)
	}

	// Convert camelCase to snake_case
	var result strings.Builder
	for i, r := range normalized {
		if i > 0 && r >= 'A' && r <= 'Z' {
			result.WriteRune('_')
		}
		result.WriteRune(r)
	}
	return strings.ToLower(result.String())
}
