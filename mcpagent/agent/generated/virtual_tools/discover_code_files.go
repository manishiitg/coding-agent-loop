package virtual_tools

import (
	"encoding/json"
	"fmt"
)

type DiscoverCodeFilesParams struct {
	// Package name from the JSON structure (e.g., 'google_sheets', 'workspace', 'virtual_tools'). Use the exact package name as shown in the JSON structure, not the MCP server name.
	Server_name string `json:"server_name"`
	// Array of tool names to discover (e.g., ['GetDocument'] for single tool, or ['GetDocument', 'ListDocuments'] for multiple tools). The tool names will be converted to snake_case filenames.
	Tool_names []string `json:"tool_names"`
}

// Get Go source code for one or more tools from a specific server. Requires server_name and tool_names (array). For a single tool, pass an array with one element.
//
// Usage: Import package and call with typed struct
//
//	Panics on API errors - check output string for tool execution errors
//
//	Example: output := DiscoverCodeFiles(DiscoverCodeFilesParams{
//	    Server_name: "value",
//	    // ... other parameters
//	})
//
// // Check output for errors (e.g., strings.HasPrefix(output, "Error:"))
// // Handle tool execution error if detected
func DiscoverCodeFiles(params DiscoverCodeFilesParams) string {
	// Convert params struct to map for API call
	paramsBytes, err := json.Marshal(params)
	if err != nil {
		panic(fmt.Sprintf("failed to marshal parameters: %v", err))
	}
	var paramsMap map[string]interface{}
	if err := json.Unmarshal(paramsBytes, &paramsMap); err != nil {
		panic(fmt.Sprintf("failed to unmarshal parameters: %v", err))
	}

	// Build request payload and call common API client
	payload := map[string]interface{}{
		"tool": "discover_code_files",
		"args": paramsMap,
	}
	return callAPI("/api/virtual/execute", payload)
}
