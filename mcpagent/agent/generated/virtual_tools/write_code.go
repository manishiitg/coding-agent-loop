package virtual_tools

import (
	"encoding/json"
	"fmt"
)

type WriteCodeParams struct {
	// Optional array of command-line arguments to pass to the Go program. Accessible via os.Args[1], os.Args[2], etc. (os.Args[0] is the program name).
	Args *[]string `json:"args,omitempty"`
	// Go source code to write
	Code string `json:"code"`
}

// Write Go code to workspace. Code can import generated tool packages from 'generated/' directory. Filename is automatically generated. Optional CLI arguments can be passed to the program via os.Args.
//
// Usage: Import package and call with typed struct
//
//	Panics on API errors - check output string for tool execution errors
//
//	Example: output := WriteCode(WriteCodeParams{
//	    Args: "value",
//	    // ... other parameters
//	})
//
// // Check output for errors (e.g., strings.HasPrefix(output, "Error:"))
// // Handle tool execution error if detected
func WriteCode(params WriteCodeParams) string {
	// Convert params struct to map for API call
	paramsBytes, err := json.Marshal(params)
	if err != nil {
		panic(fmt.Sprintf("failed to marshal parameters: %v", err))
	}
	var paramsMap map[string]interface{}
	if err := json.Unmarshal(paramsBytes, &paramsMap); err != nil {
		panic(fmt.Sprintf("failed to unmarshal parameters: %v", err))
	}

	// Build request payload and call common API client
	payload := map[string]interface{}{
		"tool": "write_code",
		"args": paramsMap,
	}
	return callAPI("/api/virtual/execute", payload)
}
