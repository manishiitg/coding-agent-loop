package virtual_tools

import (
	"encoding/json"
	"fmt"
)

type SearchLargeOutputParams struct {
	// Case sensitive search
	Case_sensitive *bool `json:"case_sensitive,omitempty"`
	// Name of the tool output file to search
	Filename string `json:"filename"`
	// Maximum number of results to return
	Max_results *int `json:"max_results,omitempty"`
	// Search pattern (regex supported)
	Pattern string `json:"pattern"`
}

// Search for regex patterns in large tool output files
//
// Usage: Import package and call with typed struct
// Example: output, err := SearchLargeOutput(SearchLargeOutputParams{
//     Case_sensitive: "value",
//     // ... other parameters
// })
//
func SearchLargeOutput(params SearchLargeOutputParams) (string, error) {
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
		"tool": "search_large_output",
		"args": paramsMap,
	}
	return callAPI("/api/virtual/execute", payload)
}

