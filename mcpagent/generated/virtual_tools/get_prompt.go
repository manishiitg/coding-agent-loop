package virtual_tools

import (
	"encoding/json"
	"fmt"
)

type GetPromptParams struct {
	// Prompt name (e.g., aws-msk, how-it-works)
	Name string `json:"name"`
	// Server name
	Server string `json:"server"`
}

// Fetch the full content of a specific prompt by name and server
//
// Usage: Import package and call with typed struct
//       Panics on API errors - check output string for tool execution errors
// Example: output := GetPrompt(GetPromptParams{
//     Name: "value",
//     // ... other parameters
// })
// // Check output for errors (e.g., strings.HasPrefix(output, "Error:"))
// // Handle tool execution error if detected
//
func GetPrompt(params GetPromptParams) string {
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
		"tool": "get_prompt",
		"args": paramsMap,
	}
	return callAPI("/api/virtual/execute", payload)
}

