package virtual_tools

import (
	"encoding/json"
	"fmt"
)

type ReadLargeOutputParams struct {
	// Ending character position (inclusive)
	End int `json:"end"`
	// Name of the tool output file (e.g., tool_20250721_091511_tavily-search.json)
	Filename string `json:"filename"`
	// Starting character position (1-based)
	Start int `json:"start"`
}

// Read specific characters from a large tool output file
//
// Usage: Import package and call with typed struct
//       Panics on API errors - check output string for tool execution errors
// Example: output := ReadLargeOutput(ReadLargeOutputParams{
//     End: "value",
//     // ... other parameters
// })
// // Check output for errors (e.g., strings.HasPrefix(output, "Error:"))
// // Handle tool execution error if detected
//
func ReadLargeOutput(params ReadLargeOutputParams) string {
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
		"tool": "read_large_output",
		"args": paramsMap,
	}
	return callAPI("/api/virtual/execute", payload)
}

