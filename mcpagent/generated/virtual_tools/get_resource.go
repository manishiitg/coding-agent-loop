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
//       Panics on API errors - check output string for tool execution errors
// Example: output := GetResource(GetResourceParams{
//     Server: "value",
//     // ... other parameters
// })
// // Check output for errors (e.g., strings.HasPrefix(output, "Error:"))
// // Handle tool execution error if detected
//
func GetResource(params GetResourceParams) string {
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
		"tool": "get_resource",
		"args": paramsMap,
	}
	return callAPI("/api/virtual/execute", payload)
}

