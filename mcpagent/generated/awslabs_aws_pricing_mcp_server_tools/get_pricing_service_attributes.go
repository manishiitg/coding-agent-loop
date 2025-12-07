package awslabs_aws_pricing_mcp_server_tools

import (
	"encoding/json"
	"fmt"
)

type GetPricingServiceAttributesParams struct {
	// Optional case-insensitive regex pattern to filter service attribute names
	Filter interface{} `json:"filter,omitempty"`
	// AWS service code (e.g., "AmazonEC2", "AmazonS3", "AmazonES")
	Service_code *string `json:"service_code,omitempty"`
}

// Get filterable attributes available for an AWS service in the Pricing API.
// 
//     **PURPOSE:** Discover what pricing dimensions (filters) are available for a specific AWS service.
// 
//     **WORKFLOW:** Use this after get_pricing_service_codes() to see what filters you can apply to narrow down pricing queries.
// 
//     **PARAMETERS:**
//     - service_code: AWS service code from get_pricing_service_codes() (e.g., 'AmazonEC2', 'AmazonRDS')
//     - filter (optional): Case-insensitive regex pattern to filter attribute names (e.g., "instance" matches "instanceType", "instanceFamily")
// 
//     **RETURNS:** List of attribute names (e.g., 'instanceType', 'location', 'storageClass') that can be used as filters.
// 
//     **NEXT STEPS:**
//     - Use get_pricing_attribute_values() to see valid values for each attribute
//     - Use these attributes in get_pricing() filters to get specific pricing data
// 
//     **EXAMPLE:** For 'AmazonRDS' you might get ['engineCode', 'instanceType', 'deploymentOption', 'location'].
//     
//
// Usage: Import package and call with typed struct
// Note: This function connects to MCP server 'awslabs.aws-pricing-mcp-server'
//          output, err := GetPricingServiceAttributes(GetPricingServiceAttributesParams{
//              Filter: "value",
//              // ... other parameters
//          })
//
func GetPricingServiceAttributes(params GetPricingServiceAttributesParams) (string, error) {
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
		"tool":   "get_pricing_service_attributes",
		"args":   paramsMap,
	}
	return callAPI("/api/mcp/execute", payload)
}

