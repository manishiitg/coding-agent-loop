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
// Example: output, err := ReadLargeOutput(ReadLargeOutputParams{
//     End: "value",
//     // ... other parameters
// })
//
func ReadLargeOutput(params ReadLargeOutputParams) (string, error) {
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
		"tool": "read_large_output",
		"args": paramsMap,
	}
	return callAPI("/api/virtual/execute", payload)
}

