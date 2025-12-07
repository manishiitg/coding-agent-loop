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
// Example: output, err := QueryLargeOutput(QueryLargeOutputParams{
//     Compact: "value",
//     // ... other parameters
// })
//
func QueryLargeOutput(params QueryLargeOutputParams) (string, error) {
	// Convert params struct to map for API call
	paramsBytes, err := json.Marshal(params)
	if err != nil {
		return "", fmt.Errorf("failed to marshal parameters: %w", err)
	}
	var paramsMap map[string]interface{}
	if err := json.Unmarshal(paramsBytes, &paramsMap); err != nil {
		return "", fmt.Errorf("failed to unmarshal parameters: %w", err)
	}

	// Build request payload and call common API client
	payload := map[string]interface{}{
		"tool": "query_large_output",
		"args": paramsMap,
	}
	return callAPI("/api/virtual/execute", payload)
}

