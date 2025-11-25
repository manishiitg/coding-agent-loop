package codegen

import (
	"fmt"
	"strings"
)

// GeneratePackageHeader generates the package header with imports
func GeneratePackageHeader(packageName string) string {
	return fmt.Sprintf(`package %s

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
)
`, packageName)
}

// GenerateStruct generates Go struct code with field comments
func GenerateStruct(goStruct *GoStruct) string {
	if len(goStruct.Fields) == 0 {
		return fmt.Sprintf("type %s struct{}\n", goStruct.Name)
	}

	var builder strings.Builder
	builder.WriteString(fmt.Sprintf("type %s struct {\n", goStruct.Name))

	for _, field := range goStruct.Fields {
		// Add field comment if description exists
		if field.Description != "" {
			// Format description as comment (handle multi-line)
			lines := strings.Split(field.Description, "\n")
			for _, line := range lines {
				builder.WriteString(fmt.Sprintf("\t// %s\n", line))
			}
		}
		omitempty := ""
		if !field.Required {
			omitempty = ",omitempty"
		}
		builder.WriteString(fmt.Sprintf("\t%s %s `json:\"%s%s\"`\n",
			field.Name, field.Type, field.JSONTag, omitempty))
	}

	builder.WriteString("}\n")
	return builder.String()
}

// GenerateFunction generates a Go function that calls MCP tool
func GenerateFunction(toolName string, structName string, actualToolName string) string {
	funcName := sanitizeFunctionName(toolName)

	code := fmt.Sprintf(`func %s(ctx context.Context, params %s) (string, error) {
	args := make(map[string]interface{})
`, funcName, structName)

	// We'll need to add field assignments based on the struct
	// For now, generate a generic version that will be enhanced
	code += fmt.Sprintf(`	// Convert params to map[string]interface{}
	// Note: This is a simplified version - actual implementation should handle all fields
	return codeexec.CallMCPTool(ctx, "%s", args)
}
`, actualToolName)

	return code
}

// GenerateFunctionWithParams generates a Go function that accepts map[string]interface{} and calls MCP API via HTTP
// Now updated to call HTTP API instead of using yaegi interpreter and codeexec registry
func GenerateFunctionWithParams(toolName string, goStruct *GoStruct, actualToolName string, toolDescription string) string {
	funcName := sanitizeFunctionName(toolName)

	var builder strings.Builder

	// Add function comment with tool description (Go doc comment format)
	if toolDescription != "" {
		lines := strings.Split(toolDescription, "\n")
		for _, line := range lines {
			builder.WriteString("// ")
			builder.WriteString(line)
			builder.WriteString("\n")
		}
	}

	// Function signature - simple parameters map
	builder.WriteString(fmt.Sprintf("func %s(params map[string]interface{}) (string, error) {\n", funcName))

	// Get API URL from environment or use default
	builder.WriteString("\tapiURL := os.Getenv(\"MCP_API_URL\")\n")
	builder.WriteString("\tif apiURL == \"\" {\n")
	builder.WriteString("\t\tapiURL = \"http://localhost:8000\"\n")
	builder.WriteString("\t}\n\n")

	// Build request payload
	builder.WriteString("\treqBody, _ := json.Marshal(map[string]interface{}{\n")
	builder.WriteString("\t\t\"server\": os.Getenv(\"MCP_SERVER_NAME\"),\n")
	builder.WriteString(fmt.Sprintf("\t\t\"tool\":   \"%s\",\n", actualToolName))
	builder.WriteString("\t\t\"args\":   params,\n")
	builder.WriteString("\t})\n\n")

	// Make HTTP request
	builder.WriteString("\tresp, err := http.Post(apiURL+\"/api/mcp/execute\", \"application/json\", bytes.NewBuffer(reqBody))\n")
	builder.WriteString("\tif err != nil {\n")
	builder.WriteString("\t\treturn \"\", err\n")
	builder.WriteString("\t}\n")
	builder.WriteString("\tdefer resp.Body.Close()\n\n")

	// Parse response
	builder.WriteString("\tvar result struct {\n")
	builder.WriteString("\t\tSuccess bool   `json:\"success\"`\n")
	builder.WriteString("\t\tResult  string `json:\"result\"`\n")
	builder.WriteString("\t\tError   string `json:\"error\"`\n")
	builder.WriteString("\t}\n")
	builder.WriteString("\tjson.NewDecoder(resp.Body).Decode(&result)\n\n")

	// Handle response
	builder.WriteString("\tif !result.Success {\n")
	builder.WriteString("\t\treturn \"\", fmt.Errorf(result.Error)\n")
	builder.WriteString("\t}\n")
	builder.WriteString("\treturn result.Result, nil\n")
	builder.WriteString("}\n\n")

	return builder.String()
}

// GenerateCustomToolFunction generates a Go function for custom tools
// Uses map[string]interface{} directly for simplicity and yaegi compatibility
func GenerateCustomToolFunction(toolName string, goStruct *GoStruct, actualToolName string, toolDescription string) string {
	funcName := sanitizeFunctionName(toolName)

	var builder strings.Builder

	// Add function comment with tool description (Go doc comment format)
	if toolDescription != "" {
		// Format description as Go doc comment (handle multi-line)
		lines := strings.Split(toolDescription, "\n")
		for _, line := range lines {
			builder.WriteString("// ")
			builder.WriteString(line)
			builder.WriteString("\n")
		}
	}

	// Use map[string]interface{} directly - no struct conversion needed
	builder.WriteString(fmt.Sprintf("func %s(ctx context.Context, params map[string]interface{}) (string, error) {\n", funcName))
	builder.WriteString(fmt.Sprintf("\treturn codeexec.CallCustomTool(ctx, \"%s\", params)\n", actualToolName))
	builder.WriteString("}\n\n")

	return builder.String()
}

// GenerateVirtualToolFunction generates a Go function for virtual tools
// Uses map[string]interface{} directly for simplicity and yaegi compatibility
func GenerateVirtualToolFunction(toolName string, goStruct *GoStruct, actualToolName string, toolDescription string) string {
	funcName := sanitizeFunctionName(toolName)

	var builder strings.Builder

	// Add function comment with tool description (Go doc comment format)
	if toolDescription != "" {
		// Format description as Go doc comment (handle multi-line)
		lines := strings.Split(toolDescription, "\n")
		for _, line := range lines {
			builder.WriteString("// ")
			builder.WriteString(line)
			builder.WriteString("\n")
		}
	}

	// Use map[string]interface{} directly - no struct conversion needed
	builder.WriteString(fmt.Sprintf("func %s(ctx context.Context, params map[string]interface{}) (string, error) {\n", funcName))
	builder.WriteString(fmt.Sprintf("\treturn codeexec.CallVirtualTool(ctx, \"%s\", params)\n", actualToolName))
	builder.WriteString("}\n\n")

	return builder.String()
}
