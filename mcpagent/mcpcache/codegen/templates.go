package codegen

import (
	"fmt"
	"strings"
	"time"
)

// GeneratePackageHeader generates the package header with imports
// Updated to include context, io, strings, and time for proper error handling, broken pipe detection, and timeouts
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
	"strings"
	"time"
)
`, packageName)
}

// GenerateToolPackageHeader generates a minimal package header for tool files
// Tool files need encoding/json and fmt
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
// API errors (network, HTTP) panic - tool execution errors are returned in result string
// Includes broken pipe retry logic for connection recovery
func GenerateAPIClient(timeout time.Duration) string {
	timeoutSeconds := int(timeout.Seconds())
	return fmt.Sprintf(`// isBrokenPipeError checks if an error is a broken pipe error
// This is used to detect connection issues that require retry
func isBrokenPipeError(err error) bool {
	if err == nil {
		return false
	}
	errorMessage := err.Error()
	return strings.Contains(errorMessage, "Broken pipe") ||
		strings.Contains(errorMessage, "broken pipe") ||
		strings.Contains(errorMessage, "[Errno 32]") ||
		strings.Contains(errorMessage, "EOF") ||
		strings.Contains(errorMessage, "connection reset")
}

// isBrokenPipeInContent checks if a string contains broken pipe error indicators
// This is used when the error is embedded in tool result content rather than returned as an error
func isBrokenPipeInContent(content string) bool {
	return strings.Contains(content, "Broken pipe") ||
		strings.Contains(content, "[Errno 32]")
}

// callAPI makes an HTTP POST request to the specified endpoint with the given payload
// This is a common function used by all tool functions to reduce code duplication
// Panics on API errors (network, HTTP failures) - tool execution errors are in result string
// Includes broken pipe retry logic: retries once with 100ms delay if broken pipe detected
// Automatically includes session_id in payload if MCP_SESSION_ID env var is set
func callAPI(endpoint string, payload map[string]interface{}) string {
	apiURL := os.Getenv("MCP_API_URL")
	if apiURL == "" {
		apiURL = "http://localhost:8000"
	}

	// Add session_id to payload for MCP connection reuse (e.g., Playwright browser sharing)
	// When set, the executor will use session registry instead of creating new connections
	sessionID := os.Getenv("MCP_SESSION_ID")
	if sessionID != "" {
		payload["session_id"] = sessionID
	}

	reqBody, err := json.Marshal(payload)
	if err != nil {
		panic(fmt.Sprintf("failed to marshal request: %%v", err))
	}

	// Retry logic for broken pipe errors
	maxRetries := 2
	for attempt := 0; attempt < maxRetries; attempt++ {
		// Create request with %d second timeout (from agent ToolTimeout)
		ctx, cancel := context.WithTimeout(context.Background(), %d*time.Second)
		
		req, err := http.NewRequestWithContext(ctx, "POST", apiURL+endpoint, bytes.NewBuffer(reqBody))
		if err != nil {
			cancel()
			panic(fmt.Sprintf("failed to create request: %%v", err))
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := http.DefaultClient.Do(req)
		
		// Check for broken pipe error in HTTP request
		if err != nil {
			cancel()
			if isBrokenPipeError(err) && attempt < maxRetries-1 {
				// Broken pipe detected - retry with short delay
				time.Sleep(100 * time.Millisecond)
				continue
			}
			panic(fmt.Sprintf("HTTP request failed: %%v", err))
		}

		// Read response body
		bodyBytes, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		cancel()

		if readErr != nil {
			if isBrokenPipeError(readErr) && attempt < maxRetries-1 {
				// Broken pipe detected while reading response - retry
				time.Sleep(100 * time.Millisecond)
				continue
			}
			panic(fmt.Sprintf("failed to read response body: %%v", readErr))
		}

		if resp.StatusCode != http.StatusOK {
			bodyStr := string(bodyBytes)
			if isBrokenPipeInContent(bodyStr) && attempt < maxRetries-1 {
				// Broken pipe detected in error response - retry
				time.Sleep(100 * time.Millisecond)
				continue
			}
			panic(fmt.Sprintf("HTTP %%d: %%s", resp.StatusCode, bodyStr))
		}

		var result struct {
			Success bool   `+"`json:\"success\"`"+`
			Result  string `+"`json:\"result\"`"+`
			Error   string `+"`json:\"error\"`"+`
		}
		if err := json.Unmarshal(bodyBytes, &result); err != nil {
			panic(fmt.Sprintf("failed to decode response: %%v", err))
		}

		if !result.Success {
			// Check for broken pipe in error message or result
			if (isBrokenPipeInContent(result.Error) || isBrokenPipeInContent(result.Result)) && attempt < maxRetries-1 {
				// Broken pipe detected in error/result - retry
				time.Sleep(100 * time.Millisecond)
				continue
			}
			panic(fmt.Sprintf("API error: %%s", result.Error))
		}

		// Check for broken pipe in successful result content
		if isBrokenPipeInContent(result.Result) && attempt < maxRetries-1 {
			// Broken pipe detected in result content - retry
			time.Sleep(100 * time.Millisecond)
			continue
		}

		// Return result string - tool execution errors will be in result (check output for error indicators)
		return result.Result
	}

	// Should never reach here, but panic if we do
	panic("callAPI: max retries exceeded without returning")
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
	builder.WriteString("//       Panics on API errors - check output string for tool execution errors\n")
	if goStruct != nil && len(goStruct.Fields) > 0 {
		builder.WriteString("//          output := " + funcName + "(" + goStruct.Name + "{\n")
		// Add example with first field
		firstField := goStruct.Fields[0]
		builder.WriteString(fmt.Sprintf("//              %s: \"value\",\n", firstField.Name))
		if len(goStruct.Fields) > 1 {
			builder.WriteString("//              // ... other parameters\n")
		}
		builder.WriteString("//          })\n")
		builder.WriteString("//          // Check output for errors (e.g., strings.HasPrefix(output, \"Error:\"))\n")
		builder.WriteString("//          // Handle tool execution error if detected\n")
	} else {
		builder.WriteString("//          output := " + funcName + "(" + goStruct.Name + "{})\n")
		builder.WriteString("//          // Check output for errors (e.g., strings.HasPrefix(output, \"Error:\"))\n")
		builder.WriteString("//          // Handle tool execution error if detected\n")
	}
	builder.WriteString("//\n")

	// Function signature - typed struct parameter, returns only string (no error)
	// Use struct name for parameter type (empty struct if no fields)
	paramType := goStruct.Name
	builder.WriteString(fmt.Sprintf("func %s(params %s) string {\n", funcName, paramType))

	// Convert struct to map for API call (panic on errors)
	builder.WriteString("\t// Convert params struct to map for API call\n")
	builder.WriteString("\tparamsBytes, err := json.Marshal(params)\n")
	builder.WriteString("\tif err != nil {\n")
	builder.WriteString("\t\tpanic(fmt.Sprintf(\"failed to marshal parameters: %v\", err))\n")
	builder.WriteString("\t}\n")
	builder.WriteString("\tvar paramsMap map[string]interface{}\n")
	builder.WriteString("\tif err := json.Unmarshal(paramsBytes, &paramsMap); err != nil {\n")
	builder.WriteString("\t\tpanic(fmt.Sprintf(\"failed to unmarshal parameters: %v\", err))\n")
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
	builder.WriteString("//       Panics on API errors - check output string for tool execution errors\n")
	if goStruct != nil && len(goStruct.Fields) > 0 {
		builder.WriteString("// Example: output := " + funcName + "(" + goStruct.Name + "{\n")
		// Add example with first field
		firstField := goStruct.Fields[0]
		builder.WriteString(fmt.Sprintf("//     %s: \"value\",\n", firstField.Name))
		if len(goStruct.Fields) > 1 {
			builder.WriteString("//     // ... other parameters\n")
		}
		builder.WriteString("// })\n")
		builder.WriteString("// // Check output for errors (e.g., strings.HasPrefix(output, \"Error:\"))\n")
		builder.WriteString("// // Handle tool execution error if detected\n")
	} else {
		builder.WriteString("// Example: output := " + funcName + "(" + goStruct.Name + "{})\n")
		builder.WriteString("// // Check output for errors (e.g., strings.HasPrefix(output, \"Error:\"))\n")
		builder.WriteString("// // Handle tool execution error if detected\n")
	}
	builder.WriteString("//\n")

	// Function signature - typed struct parameter, returns only string (no error)
	// Use struct name for parameter type (empty struct if no fields)
	paramType := goStruct.Name
	builder.WriteString(fmt.Sprintf("func %s(params %s) string {\n", funcName, paramType))

	// Convert struct to map for API call (panic on errors)
	builder.WriteString("\t// Convert params struct to map for API call\n")
	builder.WriteString("\tparamsBytes, err := json.Marshal(params)\n")
	builder.WriteString("\tif err != nil {\n")
	builder.WriteString("\t\tpanic(fmt.Sprintf(\"failed to marshal parameters: %v\", err))\n")
	builder.WriteString("\t}\n")
	builder.WriteString("\tvar paramsMap map[string]interface{}\n")
	builder.WriteString("\tif err := json.Unmarshal(paramsBytes, &paramsMap); err != nil {\n")
	builder.WriteString("\t\tpanic(fmt.Sprintf(\"failed to unmarshal parameters: %v\", err))\n")
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
	builder.WriteString("//       Panics on API errors - check output string for tool execution errors\n")
	if goStruct != nil && len(goStruct.Fields) > 0 {
		builder.WriteString("// Example: output := " + funcName + "(" + goStruct.Name + "{\n")
		// Add example with first field
		firstField := goStruct.Fields[0]
		builder.WriteString(fmt.Sprintf("//     %s: \"value\",\n", firstField.Name))
		if len(goStruct.Fields) > 1 {
			builder.WriteString("//     // ... other parameters\n")
		}
		builder.WriteString("// })\n")
		builder.WriteString("// // Check output for errors (e.g., strings.HasPrefix(output, \"Error:\"))\n")
		builder.WriteString("// // Handle tool execution error if detected\n")
	} else {
		builder.WriteString("// Example: output := " + funcName + "(" + goStruct.Name + "{})\n")
		builder.WriteString("// // Check output for errors (e.g., strings.HasPrefix(output, \"Error:\"))\n")
		builder.WriteString("// // Handle tool execution error if detected\n")
	}
	builder.WriteString("//\n")

	// Function signature - typed struct parameter, returns only string (no error)
	// Use struct name for parameter type (empty struct if no fields)
	paramType := goStruct.Name
	builder.WriteString(fmt.Sprintf("func %s(params %s) string {\n", funcName, paramType))

	// Convert struct to map for API call (panic on errors)
	builder.WriteString("\t// Convert params struct to map for API call\n")
	builder.WriteString("\tparamsBytes, err := json.Marshal(params)\n")
	builder.WriteString("\tif err != nil {\n")
	builder.WriteString("\t\tpanic(fmt.Sprintf(\"failed to marshal parameters: %v\", err))\n")
	builder.WriteString("\t}\n")
	builder.WriteString("\tvar paramsMap map[string]interface{}\n")
	builder.WriteString("\tif err := json.Unmarshal(paramsBytes, &paramsMap); err != nil {\n")
	builder.WriteString("\t\tpanic(fmt.Sprintf(\"failed to unmarshal parameters: %v\", err))\n")
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
