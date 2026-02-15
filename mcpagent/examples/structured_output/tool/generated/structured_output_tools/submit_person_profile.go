package structured_output_tools

import (
	"encoding/json"
	"fmt"
)

type SubmitPersonProfileParams struct {
	Name string `json:"name"`
	Age int `json:"age"`
	Email string `json:"email"`
}

// Submit a person profile with name, age, and email
//
// Usage: Import package and call with typed struct
//       Panics on API errors - check output string for tool execution errors
// Example: output := SubmitPersonProfile(SubmitPersonProfileParams{
//     Name: "value",
//     // ... other parameters
// })
// // Check output for errors (e.g., strings.HasPrefix(output, "Error:"))
// // Handle tool execution error if detected
//
func SubmitPersonProfile(params SubmitPersonProfileParams) string {
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
		"tool": "submit_person_profile",
		"args": paramsMap,
	}
	return callAPI("/api/custom/execute", payload)
}

