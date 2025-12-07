package virtual_tools

import (
	"encoding/json"
	"fmt"
)

type SearchLargeOutputParams struct {
	// Name of the tool output file to search
	Filename string `json:"filename"`
	// Maximum number of results to return
	Max_results *int `json:"max_results,omitempty"`
	// Search pattern (regex supported)
	Pattern string `json:"pattern"`
	// Case sensitive search
	Case_sensitive *bool `json:"case_sensitive,omitempty"`
}

// Search for regex patterns in large tool output files
//
// Usage: Import package and call with typed struct
//       Panics on API errors - check output string for tool execution errors
// Example: output := SearchLargeOutput(SearchLargeOutputParams{
//     Filename: "value",
//     // ... other parameters
// })
// // Check output for errors (e.g., strings.HasPrefix(output, "Error:"))
// // Handle tool execution error if detected
//
func SearchLargeOutput(params SearchLargeOutputParams) string {
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
		"tool": "search_large_output",
		"args": paramsMap,
	}
	return callAPI("/api/virtual/execute", payload)
}

