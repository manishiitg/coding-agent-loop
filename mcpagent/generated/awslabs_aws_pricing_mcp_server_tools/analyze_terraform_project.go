package awslabs_aws_pricing_mcp_server_tools

import (
	"encoding/json"
	"fmt"
)

type AnalyzeTerraformProjectParams struct {
	// Path to the project directory
	Project_path *string `json:"project_path,omitempty"`
}

// Analyze a Terraform project to identify AWS services used. This tool dynamically extracts service information from Terraform resource declarations.
//
// Usage: Import package and call with typed struct
// Note: This function connects to MCP server 'awslabs.aws-pricing-mcp-server'
//          output, err := AnalyzeTerraformProject(AnalyzeTerraformProjectParams{
//              Project_path: "value",
//          })
//
func AnalyzeTerraformProject(params AnalyzeTerraformProjectParams) (string, error) {
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
		"tool":   "analyze_terraform_project",
		"args":   paramsMap,
	}
	return callAPI("/api/mcp/execute", payload)
}

