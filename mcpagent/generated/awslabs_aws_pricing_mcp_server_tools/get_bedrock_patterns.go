package awslabs_aws_pricing_mcp_server_tools

import (
	"encoding/json"
	"fmt"
)

type GetBedrockPatternsParams struct{}

// Get architecture patterns for Amazon Bedrock applications, including component relationships and cost considerations
//
// Usage: Import package and call with typed struct
// Note: This function connects to MCP server 'awslabs.aws-pricing-mcp-server'
//          output, err := GetBedrockPatterns(GetBedrockPatternsParams{})
//
func GetBedrockPatterns(params GetBedrockPatternsParams) (string, error) {
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
		"server": "awslabs.aws-pricing-mcp-server",
		"tool":   "get_bedrock_patterns",
		"args":   paramsMap,
	}
	return callAPI("/api/mcp/execute", payload)
}

