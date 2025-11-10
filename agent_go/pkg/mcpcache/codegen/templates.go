package codegen

import (
	"fmt"
	"strings"
)

// GeneratePackageHeader generates the package header with imports
func GeneratePackageHeader(packageName string) string {
	return fmt.Sprintf(`package %s

import (
	"context"
	"mcp-agent/agent_go/pkg/mcpagent/codeexec"
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

// GenerateFunctionWithParams generates a Go function that accepts map[string]interface{} directly
// This is simpler and works natively with yaegi interpreter
func GenerateFunctionWithParams(toolName string, goStruct *GoStruct, actualToolName string, toolDescription string) string {
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
	builder.WriteString(fmt.Sprintf("\treturn codeexec.CallMCPTool(ctx, \"%s\", params)\n", actualToolName))
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
