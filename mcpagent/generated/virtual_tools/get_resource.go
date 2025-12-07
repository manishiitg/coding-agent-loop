package virtual_tools

import (
	"encoding/json"
	"fmt"
)

type GetResourceParams struct {
	// Server name
	Server string `json:"server"`
	// Resource URI
	Uri string `json:"uri"`
}

// Fetch the content of a specific resource by URI and server. Only use URIs that are listed in the system prompt's 'AVAILABLE RESOURCES' section.
//
// Usage: Import package and call with typed struct
// Example: output, err := GetResource(GetResourceParams{
//     Server: "value",
//     // ... other parameters
// })
//
func GetResource(params GetResourceParams) (string, error) {
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
		"tool": "get_resource",
		"args": paramsMap,
	}
	return callAPI("/api/virtual/execute", payload)
}

