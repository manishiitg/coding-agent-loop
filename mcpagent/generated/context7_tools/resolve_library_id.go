package context7_tools

import (
	"encoding/json"
	"fmt"
)

type ResolveLibraryIdParams struct {
	// Library name to search for and retrieve a Context7-compatible library ID.
	LibraryName *string `json:"libraryName,omitempty"`
}

// Resolves a package/product name to a Context7-compatible library ID and returns a list of matching libraries.
// 
// You MUST call this function before 'get-library-docs' to obtain a valid Context7-compatible library ID UNLESS the user explicitly provides a library ID in the format '/org/project' or '/org/project/version' in their query.
// 
// Selection Process:
// 1. Analyze the query to understand what library/package the user is looking for
// 2. Return the most relevant match based on:
// - Name similarity to the query (exact matches prioritized)
// - Description relevance to the query's intent
// - Documentation coverage (prioritize libraries with higher Code Snippet counts)
// - Source reputation (consider libraries with High or Medium reputation more authoritative)
// - Benchmark Score: Quality indicator (100 is the highest score)
// 
// Response Format:
// - Return the selected library ID in a clearly marked section
// - Provide a brief explanation for why this library was chosen
// - If multiple good matches exist, acknowledge this but proceed with the most relevant one
// - If no good matches exist, clearly state this and suggest query refinements
// 
// For ambiguous queries, request clarification before proceeding with a best-guess match.
//
// Usage: Import package and call with typed struct
// Note: This function connects to MCP server 'context7'
//          output, err := ResolveLibraryId(ResolveLibraryIdParams{
//              LibraryName: "value",
//          })
//
func ResolveLibraryId(params ResolveLibraryIdParams) (string, error) {
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
		"tool":   "resolve-library-id",
		"args":   paramsMap,
	}
	return callAPI("/api/mcp/execute", payload)
}

