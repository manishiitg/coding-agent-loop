package awslabs_aws_pricing_mcp_server_tools

import (
	"encoding/json"
	"fmt"
)

type GenerateCostReportParams struct {
	// Detailed cost information for complex scenarios
	Detailed_cost_data interface{} `json:"detailed_cost_data,omitempty"`
	// List of items excluded from cost analysis
	Exclusions interface{} `json:"exclusions,omitempty"`
	// Raw pricing data from AWS pricing tools
	Pricing_data *map[string]interface{} `json:"pricing_data,omitempty"`
	// Pricing model (e.g., "ON DEMAND", "Reserved")
	Pricing_model *string `json:"pricing_model,omitempty"`
	// Direct recommendations or guidance for generation
	Recommendations interface{} `json:"recommendations,omitempty"`
	// List of related AWS services
	Related_services interface{} `json:"related_services,omitempty"`
	// List of assumptions for cost analysis
	Assumptions interface{} `json:"assumptions,omitempty"`
	// Output format ("markdown" or "csv")
	Format *string `json:"format,omitempty"`
	// Path to save the report file
	Output_file interface{} `json:"output_file,omitempty"`
	// Name of the AWS service
	Service_name *string `json:"service_name,omitempty"`
}

// Generate a detailed cost analysis report based on pricing data for one or more AWS services.
// 
// This tool requires AWS pricing data and provides options for adding detailed cost information.
// 
// IMPORTANT REQUIREMENTS:
// - ALWAYS include detailed unit pricing information (e.g., "$0.0008 per 1K input tokens")
// - ALWAYS show calculation breakdowns (unit price × usage = total cost)
// - ALWAYS specify the pricing model (e.g., "ON DEMAND")
// - ALWAYS list all assumptions and exclusions explicitly
// 
// Output Format Options:
// - 'markdown' (default): Generates a well-formatted markdown report
// - 'csv': Generates a CSV format report with sections for service information, unit pricing, cost calculations, etc.
// 
// Example usage:
// 
// ```json
// {
//   // Required parameters
//   "pricing_data": {
//     // This should contain pricing data retrieved from get_pricing
//     "status": "success",
//     "service_name": "bedrock",
//     "data": "... pricing information ...",
//     "message": "Retrieved pricing for bedrock from AWS Pricing url"
//   },
//   "service_name": "Amazon Bedrock",
// 
//   // Core parameters (commonly used)
//   "related_services": ["Lambda", "S3"],
//   "pricing_model": "ON DEMAND",
//   "assumptions": [
//     "Standard ON DEMAND pricing model",
//     "No caching or optimization applied",
//     "Average request size of 4KB"
//   ],
//   "exclusions": [
//     "Data transfer costs between regions",
//     "Custom model training costs",
//     "Development and maintenance costs"
//   ],
//   "output_file": "cost_analysis_report.md",  // or "cost_analysis_report.csv" for CSV format
//   "format": "markdown",  // or "csv" for CSV format
// 
//   // Advanced parameter for complex scenarios
//   "detailed_cost_data": {
//     "services": {
//       "Amazon Bedrock Foundation Models": {
//         "usage": "Processing 1M input tokens and 500K output tokens with Claude 3.5 Haiku",
//         "estimated_cost": "$80.00",
//         "free_tier_info": "No free tier for Bedrock foundation models",
//         "unit_pricing": {
//           "input_tokens": "$0.0008 per 1K tokens",
//           "output_tokens": "$0.0016 per 1K tokens"
//         },
//         "usage_quantities": {
//           "input_tokens": "1,000,000 tokens",
//           "output_tokens": "500,000 tokens"
//         },
//         "calculation_details": "$0.0008/1K × 1,000K input tokens + $0.0016/1K × 500K output tokens = $80.00"
//       },
//       "AWS Lambda": {
//         "usage": "6,000 requests per month with 512 MB memory",
//         "estimated_cost": "$0.38",
//         "free_tier_info": "First 12 months: 1M requests/month free",
//         "unit_pricing": {
//           "requests": "$0.20 per 1M requests",
//           "compute": "$0.0000166667 per GB-second"
//         },
//         "usage_quantities": {
//           "requests": "6,000 requests",
//           "compute": "6,000 requests × 1s × 0.5GB = 3,000 GB-seconds"
//         },
//         "calculation_details": "$0.20/1M × 0.006M requests + $0.0000166667 × 3,000 GB-seconds = $0.38"
//       }
//     }
//   },
// 
//   // Recommendations parameter - can be provided directly or generated
//   "recommendations": {
//     "immediate": [
//       "Optimize prompt engineering to reduce token usage for Claude 3.5 Haiku",
//       "Configure Knowledge Base OCUs based on actual query patterns",
//       "Implement response caching for common queries to reduce token usage"
//     ],
//     "best_practices": [
//       "Monitor OCU utilization metrics and adjust capacity as needed",
//       "Use prompt caching for repeated context across API calls",
//       "Consider provisioned throughput for predictable workloads"
//     ]
//   }
// }
// ```
// 
//
// Usage: Import package and call with typed struct
// Note: This function connects to MCP server 'awslabs.aws-pricing-mcp-server'
//          output, err := GenerateCostReport(GenerateCostReportParams{
//              Detailed_cost_data: "value",
//              // ... other parameters
//          })
//
func GenerateCostReport(params GenerateCostReportParams) (string, error) {
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
		"tool":   "generate_cost_report",
		"args":   paramsMap,
	}
	return callAPI("/api/mcp/execute", payload)
}

