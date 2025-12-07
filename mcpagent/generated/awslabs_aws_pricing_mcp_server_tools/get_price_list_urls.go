package awslabs_aws_pricing_mcp_server_tools

import (
	"encoding/json"
	"fmt"
)

type GetPriceListUrlsParams struct {
	// Effective date for pricing in format "YYYY-MM-DD HH:MM" (default: current timestamp)
	Effective_date interface{} `json:"effective_date,omitempty"`
	// AWS region (e.g., "us-east-1", "eu-west-1")
	Region *string `json:"region,omitempty"`
	// AWS service code (e.g., "AmazonEC2", "AmazonS3", "AmazonES")
	Service_code *string `json:"service_code,omitempty"`
}

// Get download URLs for bulk pricing data files.
// 
//     **PURPOSE:** Access complete AWS pricing datasets as downloadable files for historical analysis and bulk processing.
// 
//     **WORKFLOW:** Use this for historical pricing analysis or bulk data processing when current pricing from get_pricing() isn't sufficient.
// 
//     **PARAMETERS:**
//     - Service code from get_pricing_service_codes() (e.g., 'AmazonEC2', 'AmazonS3')
//     - AWS region (e.g., 'us-east-1', 'eu-west-1')
//     - Optional: effective_date for historical pricing (default: current date)
// 
//     **RETURNS:** Dictionary with download URLs for different formats:
//     - 'csv': Direct download URL for CSV format
//     - 'json': Direct download URL for JSON format
// 
//     **USE CASES:**
//     - Historical pricing analysis (get_pricing() only provides current pricing)
//     - Bulk data processing without repeated API calls
//     - Offline analysis of complete pricing datasets
//     - Savings Plans analysis across services
// 
//     **FILE PROCESSING:**
//     - CSV files: Lines 1-5 are metadata, Line 6 contains headers, Line 7+ contains pricing data
//     - Use `tail -n +7 pricing.csv | grep "t3.medium"` to filter data
//     
//
// Usage: Import package and call with typed struct
// Note: This function connects to MCP server 'awslabs.aws-pricing-mcp-server'
//          output, err := GetPriceListUrls(GetPriceListUrlsParams{
//              Effective_date: "value",
//              // ... other parameters
//          })
//
func GetPriceListUrls(params GetPriceListUrlsParams) (string, error) {
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
		"tool":   "get_price_list_urls",
		"args":   paramsMap,
	}
	return callAPI("/api/mcp/execute", payload)
}

