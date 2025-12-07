package virtual_tools

import (
	"encoding/json"
	"fmt"
)

type DiscoverCodeFilesParams struct {
	// MCP server name (e.g., 'aws', 'gdrive', 'google_sheets', 'virtual_tools', 'custom_tools').
	Server_name string `json:"server_name"`
	// Tool name (e.g., 'GetDocument', 'resolve-library-id'). The tool name will be converted to snake_case filename.
	Tool_name string `json:"tool_name"`
}

// Get Go source code for a specific tool from a specific server. Both server_name and tool_name are required.
//
// Usage: Import package and call with typed struct
//       Panics on API errors - check output string for tool execution errors
// Example: output := DiscoverCodeFiles(DiscoverCodeFilesParams{
//     Server_name: "value",
//     // ... other parameters
// })
// // Check output for errors (e.g., strings.HasPrefix(output, "Error:"))
// // Handle tool execution error if detected
//
func DiscoverCodeFiles(params DiscoverCodeFilesParams) string {
	// Convert params struct to map for API call
	paramsBytes, err := json.Marshal(params)
	if err != nil {
		panic(fmt.Sprintf("failed to marshal parameters: %%v", err))
	}
	var paramsMap map[string]interface{}
	if err := json.Unmarshal(paramsBytes, &paramsMap); err != nil {
		panic(fmt.Sprintf("failed to unmarshal parameters: %%v", err))
	}

	// Build request payload and call common API client
	payload := map[string]interface{}{
		"tool": "discover_code_files",
		"args": paramsMap,
	}
	return callAPI("/api/virtual/execute", payload)
}

