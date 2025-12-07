package virtual_tools

import (
	"encoding/json"
	"fmt"
)

type QueryLargeOutputParams struct {
	// Output compact JSON format
	Compact *bool `json:"compact,omitempty"`
	// Name of the JSON tool output file
	Filename string `json:"filename"`
	// jq query to execute (e.g., '.name', '.items[]')
	Query string `json:"query"`
	// Output raw string values
	Raw *bool `json:"raw,omitempty"`
}

// Execute jq queries on large JSON tool output files
//
// Usage: Import package and call with typed struct
//       Panics on API errors - check output string for tool execution errors
// Example: output := QueryLargeOutput(QueryLargeOutputParams{
//     Compact: "value",
//     // ... other parameters
// })
// // Check output for errors (e.g., strings.HasPrefix(output, "Error:"))
// // Handle tool execution error if detected
//
func QueryLargeOutput(params QueryLargeOutputParams) string {
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
		"tool": "query_large_output",
		"args": paramsMap,
	}
	return callAPI("/api/virtual/execute", payload)
}

