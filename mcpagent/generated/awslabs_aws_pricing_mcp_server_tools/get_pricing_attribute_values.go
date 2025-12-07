package awslabs_aws_pricing_mcp_server_tools

import (
	"encoding/json"
	"fmt"
)

type GetPricingAttributeValuesParams struct {
	// Optional dictionary mapping attribute names to regex patterns for filtering their values (e.g., {"instanceType": "t3", "operatingSystem": "Linux"})
	Filters interface{} `json:"filters,omitempty"`
	// AWS service code (e.g., "AmazonEC2", "AmazonS3", "AmazonES")
	Service_code *string `json:"service_code,omitempty"`
	// List of attribute names (e.g., ["instanceType", "location", "storageClass"])
	Attribute_names *[]string `json:"attribute_names,omitempty"`
}

// Get valid values for pricing filter attributes.
// 
//     **PURPOSE:** Discover what values are available for specific pricing filter attributes of an AWS service.
// 
//     **WORKFLOW:** Use this after get_pricing_service_attributes() to see valid values for each filter attribute.
// 
//     **PARAMETERS:**
//     - Service code from get_pricing_service_codes() (e.g., 'AmazonEC2', 'AmazonRDS')
//     - List of attribute names from get_pricing_service_attributes() (e.g., ['instanceType', 'location'])
//     - filters (optional): Dictionary mapping attribute names to regex patterns (e.g., {'instanceType': 't3'})
// 
//     **RETURNS:** Dictionary mapping attribute names to their valid values. Filtered attributes return only matching values, unfiltered attributes return all values.
// 
//     **EXAMPLE RETURN:**
//     ```
//     {
//         'instanceType': ['t2.micro', 't3.medium', 'm5.large', ...],
//         'location': ['US East (N. Virginia)', 'EU (London)', ...]
//     }
//     ```
// 
//     **NEXT STEPS:** Use these values in get_pricing() filters to get specific pricing data.
// 
//     **ERROR HANDLING:** Uses "all-or-nothing" approach - if any attribute fails, the entire operation fails.
// 
//     **EXAMPLES:**
//     - Single attribute: ['instanceType'] returns {'instanceType': ['t2.micro', 't3.medium', ...]}
//     - Multiple attributes: ['instanceType', 'location'] returns both mappings
//     - Partial filtering: filters={'instanceType': 't3'} applies only to instanceType, location returns all values
//     
//
// Usage: Import package and call with typed struct
// Note: This function connects to MCP server 'awslabs.aws-pricing-mcp-server'
//          output, err := GetPricingAttributeValues(GetPricingAttributeValuesParams{
//              Filters: "value",
//              // ... other parameters
//          })
//
func GetPricingAttributeValues(params GetPricingAttributeValuesParams) (string, error) {
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
		"tool":   "get_pricing_attribute_values",
		"args":   paramsMap,
	}
	return callAPI("/api/mcp/execute", payload)
}

