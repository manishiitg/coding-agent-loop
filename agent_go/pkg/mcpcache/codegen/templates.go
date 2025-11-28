package codegen

import (
	"fmt"
	"strings"
	"time"
)

// GeneratePackageHeader generates the package header with imports
// Updated to include context, io, and time for proper error handling and timeouts
func GeneratePackageHeader(packageName string) string {
	return fmt.Sprintf(`package %s

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)
`, packageName)
}

// GenerateToolPackageHeader generates a minimal package header for tool files
// Tool files only need encoding/json and fmt since they use the common callAPI function
func GenerateToolPackageHeader(packageName string) string {
	return fmt.Sprintf(`package %s

import (
	"encoding/json"
	"fmt"
)
`, packageName)
}

// GenerateAPIClient generates a common API client function for all tools in a package
// This reduces code duplication across generated tool functions
func GenerateAPIClient(timeout time.Duration) string {
	timeoutSeconds := int(timeout.Seconds())
	return fmt.Sprintf(`// callAPI makes an HTTP POST request to the specified endpoint with the given payload
// This is a common function used by all tool functions to reduce code duplication
func callAPI(endpoint string, payload map[string]interface{}) (string, error) {
	apiURL := os.Getenv("MCP_API_URL")
	if apiURL == "" {
		apiURL = "http://localhost:8000"
	}

	reqBody, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %%w", err)
	}

	// Create request with %d second timeout (from agent ToolTimeout)
	ctx, cancel := context.WithTimeout(context.Background(), %d*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "POST", apiURL+endpoint, bytes.NewBuffer(reqBody))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %%w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("HTTP request failed: %%w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("HTTP %%d: %%s", resp.StatusCode, string(body))
	}

	var result struct {
		Success bool   `+"`json:\"success\"`"+`
		Result  string `+"`json:\"result\"`"+`
		Error   string `+"`json:\"error\"`"+`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("failed to decode response: %%w", err)
	}

	if !result.Success {
		return "", fmt.Errorf("%%s", result.Error)
	}
	return result.Result, nil
}

`, timeoutSeconds, timeoutSeconds)
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

// GenerateFunction generates a Go function that calls MCP tool via HTTP API
// Note: This function is kept for backward compatibility but uses HTTP API now
func GenerateFunction(toolName string, structName string, actualToolName string) string {
	funcName := sanitizeFunctionName(toolName)

	var builder strings.Builder

	// Function signature - simple parameters map (no context needed for HTTP calls)
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

// GenerateFunctionWithParams generates a Go function that accepts typed struct and calls MCP API via HTTP
func GenerateFunctionWithParams(toolName string, goStruct *GoStruct, actualToolName string, toolDescription string, serverName string, timeout time.Duration) string {
	funcName := sanitizeFunctionName(toolName)

	var builder strings.Builder

	// First, generate the struct definition (always generate, even if empty)
	if goStruct != nil {
		builder.WriteString(GenerateStruct(goStruct))
		builder.WriteString("\n")
	}

	// Add function comment with tool description (Go doc comment format)
	if toolDescription != "" {
		lines := strings.Split(toolDescription, "\n")
		for _, line := range lines {
			builder.WriteString("// ")
			builder.WriteString(line)
			builder.WriteString("\n")
		}
	}

	// Add usage comment with parameter information
	builder.WriteString("//\n")
	builder.WriteString("// Usage: Import package and call with typed struct\n")
	builder.WriteString(fmt.Sprintf("// Note: This function connects to MCP server '%s'\n", serverName))
	if goStruct != nil && len(goStruct.Fields) > 0 {
		builder.WriteString("//          output, err := " + funcName + "(" + goStruct.Name + "{\n")
		// Add example with first field
		firstField := goStruct.Fields[0]
		builder.WriteString(fmt.Sprintf("//              %s: \"value\",\n", firstField.Name))
		if len(goStruct.Fields) > 1 {
			builder.WriteString("//              // ... other parameters\n")
		}
		builder.WriteString("//          })\n")
	} else {
		builder.WriteString("//          output, err := " + funcName + "(" + goStruct.Name + "{})\n")
	}
	builder.WriteString("//\n")

	// Function signature - typed struct parameter
	var paramType string
	if goStruct != nil && len(goStruct.Fields) > 0 {
		paramType = goStruct.Name
	} else {
		// For tools with no parameters, still use a struct but it will be empty
		paramType = goStruct.Name
	}
	builder.WriteString(fmt.Sprintf("func %s(params %s) (string, error) {\n", funcName, paramType))

	// Convert struct to map for API call with proper error handling
	builder.WriteString("\t// Convert params struct to map for API call\n")
	builder.WriteString("\tparamsBytes, err := json.Marshal(params)\n")
	builder.WriteString("\tif err != nil {\n")
	builder.WriteString("\t\treturn \"\", fmt.Errorf(\"failed to marshal parameters: %w\", err)\n")
	builder.WriteString("\t}\n")
	builder.WriteString("\tvar paramsMap map[string]interface{}\n")
	builder.WriteString("\tif err := json.Unmarshal(paramsBytes, &paramsMap); err != nil {\n")
	builder.WriteString("\t\treturn \"\", fmt.Errorf(\"failed to unmarshal parameters: %w\", err)\n")
	builder.WriteString("\t}\n\n")

	// Build request payload and call common API client
	builder.WriteString("\t// Build request payload and call common API client\n")
	builder.WriteString("\tpayload := map[string]interface{}{\n")
	builder.WriteString(fmt.Sprintf("\t\t\"server\": \"%s\",\n", serverName))
	builder.WriteString(fmt.Sprintf("\t\t\"tool\":   \"%s\",\n", actualToolName))
	builder.WriteString("\t\t\"args\":   paramsMap,\n")
	builder.WriteString("\t}\n")
	builder.WriteString("\treturn callAPI(\"/api/mcp/execute\", payload)\n")
	builder.WriteString("}\n\n")

	return builder.String()
}

// GenerateCustomToolFunction generates a Go function for custom tools
// Updated to call HTTP API instead of using codeexec registry
func GenerateCustomToolFunction(toolName string, goStruct *GoStruct, actualToolName string, toolDescription string, timeout time.Duration) string {
	funcName := sanitizeFunctionName(toolName)

	var builder strings.Builder

	// First, generate the struct definition (always generate, even if empty)
	if goStruct != nil {
		builder.WriteString(GenerateStruct(goStruct))
		builder.WriteString("\n")
	}

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

	// Add usage comment with parameter information
	builder.WriteString("//\n")
	builder.WriteString("// Usage: Import package and call with typed struct\n")
	if goStruct != nil && len(goStruct.Fields) > 0 {
		builder.WriteString("// Example: output, err := " + funcName + "(" + goStruct.Name + "{\n")
		// Add example with first field
		firstField := goStruct.Fields[0]
		builder.WriteString(fmt.Sprintf("//     %s: \"value\",\n", firstField.Name))
		if len(goStruct.Fields) > 1 {
			builder.WriteString("//     // ... other parameters\n")
		}
		builder.WriteString("// })\n")
	} else {
		builder.WriteString("// Example: output, err := " + funcName + "(" + goStruct.Name + "{})\n")
	}
	builder.WriteString("//\n")

	// Function signature - typed struct parameter
	var paramType string
	if goStruct != nil && len(goStruct.Fields) > 0 {
		paramType = goStruct.Name
	} else {
		paramType = goStruct.Name
	}
	builder.WriteString(fmt.Sprintf("func %s(params %s) (string, error) {\n", funcName, paramType))

	// Convert struct to map for API call with proper error handling
	builder.WriteString("\t// Convert params struct to map for API call\n")
	builder.WriteString("\tparamsBytes, err := json.Marshal(params)\n")
	builder.WriteString("\tif err != nil {\n")
	builder.WriteString("\t\treturn \"\", fmt.Errorf(\"failed to marshal parameters: %w\", err)\n")
	builder.WriteString("\t}\n")
	builder.WriteString("\tvar paramsMap map[string]interface{}\n")
	builder.WriteString("\tif err := json.Unmarshal(paramsBytes, &paramsMap); err != nil {\n")
	builder.WriteString("\t\treturn \"\", fmt.Errorf(\"failed to unmarshal parameters: %w\", err)\n")
	builder.WriteString("\t}\n\n")

	// Build request payload and call common API client
	builder.WriteString("\t// Build request payload and call common API client\n")
	builder.WriteString("\tpayload := map[string]interface{}{\n")
	builder.WriteString(fmt.Sprintf("\t\t\"tool\": \"%s\",\n", actualToolName))
	builder.WriteString("\t\t\"args\": paramsMap,\n")
	builder.WriteString("\t}\n")
	builder.WriteString("\treturn callAPI(\"/api/custom/execute\", payload)\n")
	builder.WriteString("}\n\n")

	return builder.String()
}

// GenerateVirtualToolFunction generates a Go function for virtual tools
// Updated to call HTTP API instead of using codeexec registry
func GenerateVirtualToolFunction(toolName string, goStruct *GoStruct, actualToolName string, toolDescription string, timeout time.Duration) string {
	funcName := sanitizeFunctionName(toolName)

	var builder strings.Builder

	// First, generate the struct definition (always generate, even if empty)
	if goStruct != nil {
		builder.WriteString(GenerateStruct(goStruct))
		builder.WriteString("\n")
	}

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

	// Add usage comment with parameter information
	builder.WriteString("//\n")
	builder.WriteString("// Usage: Import package and call with typed struct\n")
	if goStruct != nil && len(goStruct.Fields) > 0 {
		builder.WriteString("// Example: output, err := " + funcName + "(" + goStruct.Name + "{\n")
		// Add example with first field
		firstField := goStruct.Fields[0]
		builder.WriteString(fmt.Sprintf("//     %s: \"value\",\n", firstField.Name))
		if len(goStruct.Fields) > 1 {
			builder.WriteString("//     // ... other parameters\n")
		}
		builder.WriteString("// })\n")
	} else {
		builder.WriteString("// Example: output, err := " + funcName + "(" + goStruct.Name + "{})\n")
	}
	builder.WriteString("//\n")

	// Function signature - typed struct parameter
	var paramType string
	if goStruct != nil && len(goStruct.Fields) > 0 {
		paramType = goStruct.Name
	} else {
		paramType = goStruct.Name
	}
	builder.WriteString(fmt.Sprintf("func %s(params %s) (string, error) {\n", funcName, paramType))

	// Convert struct to map for API call with error handling
	builder.WriteString("\t// Convert params struct to map for API call\n")
	builder.WriteString("\tparamsBytes, err := json.Marshal(params)\n")
	builder.WriteString("\tif err != nil {\n")
	builder.WriteString("\t\treturn \"\", fmt.Errorf(\"failed to marshal parameters: %w\", err)\n")
	builder.WriteString("\t}\n")
	builder.WriteString("\tvar paramsMap map[string]interface{}\n")
	builder.WriteString("\tif err := json.Unmarshal(paramsBytes, &paramsMap); err != nil {\n")
	builder.WriteString("\t\treturn \"\", fmt.Errorf(\"failed to unmarshal parameters: %w\", err)\n")
	builder.WriteString("\t}\n\n")

	// Build request payload and call common API client
	builder.WriteString("\t// Build request payload and call common API client\n")
	builder.WriteString("\tpayload := map[string]interface{}{\n")
	builder.WriteString(fmt.Sprintf("\t\t\"tool\": \"%s\",\n", actualToolName))
	builder.WriteString("\t\t\"args\": paramsMap,\n")
	builder.WriteString("\t}\n")
	builder.WriteString("\treturn callAPI(\"/api/virtual/execute\", payload)\n")
	builder.WriteString("}\n\n")

	return builder.String()
}
