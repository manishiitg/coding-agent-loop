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
// Example: output, err := GetPrompt(GetPromptParams{
//     Name: "value",
//     // ... other parameters
// })
//
func GetPrompt(params GetPromptParams) (string, error) {
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
		"tool": "get_prompt",
		"args": paramsMap,
	}
	return callAPI("/api/virtual/execute", payload)
}

