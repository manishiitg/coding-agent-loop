package context7_tools

import (
	"encoding/json"
	"fmt"
)

type GetLibraryDocsParams struct {
	// Exact Context7-compatible library ID (e.g., '/mongodb/docs', '/vercel/next.js', '/supabase/supabase', '/vercel/next.js/v14.3.0-canary.87') retrieved from 'resolve-library-id' or directly from user query in the format '/org/project' or '/org/project/version'.
	Context7CompatibleLibraryID *string `json:"context7CompatibleLibraryID,omitempty"`
	// Documentation mode: 'code' for API references and code examples (default), 'info' for conceptual guides, narrative information, and architectural questions.
	Mode *string `json:"mode,omitempty"`
	// Page number for pagination (start: 1, default: 1). If the context is not sufficient, try page=2, page=3, page=4, etc. with the same topic.
	Page *int `json:"page,omitempty"`
	// Topic to focus documentation on (e.g., 'hooks', 'routing').
	Topic *string `json:"topic,omitempty"`
}

// Fetches up-to-date documentation for a library. You must call 'resolve-library-id' first to obtain the exact Context7-compatible library ID required to use this tool, UNLESS the user explicitly provides a library ID in the format '/org/project' or '/org/project/version' in their query. Use mode='code' (default) for API references and code examples, or mode='info' for conceptual guides, narrative information, and architectural questions.
//
// Usage: Import package and call with typed struct
// Note: This function connects to MCP server 'context7'
//          output, err := GetLibraryDocs(GetLibraryDocsParams{
//              Context7CompatibleLibraryID: "value",
//              // ... other parameters
//          })
//
func GetLibraryDocs(params GetLibraryDocsParams) (string, error) {
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
		"server": "context7",
		"tool":   "get-library-docs",
		"args":   paramsMap,
	}
	return callAPI("/api/mcp/execute", payload)
}

