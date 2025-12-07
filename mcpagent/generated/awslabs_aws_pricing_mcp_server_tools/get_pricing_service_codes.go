package awslabs_aws_pricing_mcp_server_tools

import (
	"encoding/json"
	"fmt"
)

type GetPricingServiceCodesParams struct {
	// Optional case-insensitive regex pattern to filter service codes
	Filter interface{} `json:"filter,omitempty"`
}

// Get AWS service codes available in the Price List API.
// 
//     **PURPOSE:** Discover which AWS services have pricing information available in the AWS Price List API.
// 
//     **PARAMETERS:**
//     - filter (optional): Case-insensitive regex pattern to filter service codes (e.g., "bedrock" matches "AmazonBedrock", "AmazonBedrockService")
// 
//     **WORKFLOW:** This is the starting point for any pricing query. Use this first to find the correct service code.
// 
//     **RETURNS:** List of service codes (e.g., 'AmazonEC2', 'AmazonS3', 'AWSLambda') that can be used with other pricing tools.
// 
//     **NEXT STEPS:**
//     - Use get_pricing_service_attributes() to see what filters are available for a service
//     - Use get_pricing() to get actual pricing data for a service
// 
//     **NOTE:** Service codes may differ from AWS console names (e.g., 'AmazonES' for OpenSearch, 'AWSLambda' for Lambda).
//     
//
// Usage: Import package and call with typed struct
// Note: This function connects to MCP server 'awslabs.aws-pricing-mcp-server'
//          output, err := GetPricingServiceCodes(GetPricingServiceCodesParams{
//              Filter: "value",
//          })
//
func GetPricingServiceCodes(params GetPricingServiceCodesParams) (string, error) {
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
		"tool":   "get_pricing_service_codes",
		"args":   paramsMap,
	}
	return callAPI("/api/mcp/execute", payload)
}

