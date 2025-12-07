package awslabs_aws_pricing_mcp_server_tools

import (
	"encoding/json"
	"fmt"
)

type GetPricingParams struct {
	// Maximum response length in characters (default: 100,000, use -1 for unlimited)
	Max_allowed_characters *int `json:"max_allowed_characters,omitempty"`
	// Maximum number of results to return per page (default: 100, max: 100)
	Max_results *int `json:"max_results,omitempty"`
	// Pagination token from previous response to get next page of results
	Next_token interface{} `json:"next_token,omitempty"`
	// Optional output filtering options to reduce response size. Use {"pricing_terms": ["OnDemand", "FlatRate"]} to significantly reduce response size for large services like EC2.
	Output_options interface{} `json:"output_options,omitempty"`
	// AWS region(s) - single region string (e.g., "us-east-1") or list for multi-region comparison (e.g., ["us-east-1", "us-west-2", "eu-west-1"]). Optional: omit for global services like DataTransfer or CloudFront that don't have region-specific pricing.
	Region interface{} `json:"region,omitempty"`
	// AWS service code (e.g., "AmazonEC2", "AmazonS3", "AmazonES")
	Service_code *string `json:"service_code,omitempty"`
	// Optional list of filters to apply to the pricing query
	Filters interface{} `json:"filters,omitempty"`
}

// 
//     Get detailed pricing information from AWS Price List API with optional filters.
// 
//     **PARAMETERS:**
//     - service_code (required): AWS service code (e.g., 'AmazonEC2', 'AmazonS3', 'AmazonES')
//     - region (optional): AWS region string (e.g., 'us-east-1') OR list for multi-region comparison (e.g., ['us-east-1', 'eu-west-1']). Omit for global services like DataTransfer or CloudFront that don't have region-specific pricing.
//     - filters (optional): List of filter dictionaries in format {'Field': str, 'Type': str, 'Value': str}
//     - max_allowed_characters (optional): Response size limit in characters (default: 100,000, use -1 for unlimited)
//     - output_options (optional): OutputOptions object for response transformation and size reduction
//     - max_results (optional): Maximum number of results to return per page (default: 100, min: 1, max: 100)
//     - next_token (optional): Pagination token from previous response to get next page of results
// 
//     **MANDATORY WORKFLOW - ALWAYS FOLLOW:**
// 
//     **Step 1: Discover Available Options**
//     ```python
//     service_codes = get_pricing_service_codes()                              # Find correct service (skip if known)
//     attributes = get_pricing_service_attributes('AmazonEC2')                 # Discover filterable dimensions
//     attribute_values = get_pricing_attribute_values('AmazonEC2', 'memory')   # Get valid values for filtering
//     ```
// 
//     **Step 2: Build Precise Filters**
//     ```python
//     # Use ONLY values discovered in Step 1
//     filters = [
//        {"Field": "memory", "Value": ["8 GiB", "16 GiB", "32 GiB"], "Type": "ANY_OF"},     # Multiple options
//        {"Field": "instanceType", "Value": "m5", "Type": "CONTAINS"},                      # Pattern matching
//        {"Field": "instanceType", "Value": ["t2", "m4"], "Type": "NONE_OF"}                # Exclude older
//    ]
//     ```
// 
//     **Step 3: Execute Query**
//     ```python
//     pricing = get_pricing('AmazonEC2', 'us-east-1', filters)
//     ```
// 
//     **FILTER TYPES:**
//     - **EQUALS**: Exact match (default) - `{"Field": "instanceType", "Value": "m5.large"}`
//     - **ANY_OF**: Multiple options - `{"Field": "memory", "Value": ["8 GiB", "16 GiB"], "Type": "ANY_OF"}`
//     - **CONTAINS**: Pattern match - `{"Field": "instanceType", "Value": "m5", "Type": "CONTAINS"}`
//     - **NONE_OF**: Exclusion - `{"Field": "instanceType", "Value": ["t2", "m4"], "Type": "NONE_OF"}`
// 
//     **CRITICAL: ANY_OF FILTER VALUE LIMITS:**
//     - **1024 CHARACTER LIMIT**: Total length of all values in ANY_OF arrays cannot exceed 1024 characters
//     - **PROGRESSIVE FILTERING**: Start with minimal qualifying options, expand if needed
//     - **EXAMPLE VIOLATION**: `["8 GiB", "16 GiB", "32 GiB", "64 GiB", "96 GiB", "128 GiB", ...]` (TOO LONG)
//     - **CORRECT APPROACH**: `["8 GiB", "16 GiB", "32 GiB", "36 GiB", "48 GiB"]` (TARGETED LIST)
// 
//     **COMMON USE CASES:**
// 
//     **COST OPTIMIZATION - EXHAUSTIVE MINIMUM-FIRST APPROACH:** When users ask for "lowest price", "cheapest", or cost optimization
//     - **LOWER = CHEAPER ASSUMPTION**: For cost optimization, assume lower capabilities cost less than higher ones
//       * 32 GB storage is cheaper than 300 GB storage
//       * 8 GiB RAM is cheaper than 64 GiB RAM
//     - **CRITICAL FOR COST QUERIES**: Start IMMEDIATELY above minimum requirement and test ALL options incrementally
//     - **EXHAUSTIVE ENUMERATION REQUIRED**: Each storage/memory tier is MUTUALLY EXCLUSIVE - must list each one explicitly
//     - **STOP AT REASONABLE UPPER BOUND**: For cost optimization, limit upper bound to 2-3x minimum requirement to avoid expensive options
//     - **exclude_free_products**: ESSENTIAL for cost analysis - removes $0.00 reservation placeholders, SQL licensing variants, and special pricing entries that obscure actual billable instances when finding cheapest options
//     - Use ANY_OF for efficient multi-option comparison in single API call
//     - Multi-attribute capability filtering for minimum requirements
//     - Combine CONTAINS + NONE_OF for refined discovery
// 
//     **OUTPUT OPTIONS (Response Size & Performance Control):**
//     - **PURPOSE**: Transform and optimize API responses for ALL services, especially critical for large services (EC2, RDS)
//     - **IMMEDIATE COMBINED APPROACH**: `{"pricing_terms": ["OnDemand", "FlatRate"], "product_attributes": ["instanceType", "location", "memory"]}`
//     - **ATTRIBUTE DISCOVERY**: Use get_pricing_service_attributes() - same names for filters and output_options
//     - **SIZE REDUCTION**: 80%+ reduction with combined pricing_terms + product_attributes
//     - **exclude_free_products**: Remove products with $0.00 OnDemand pricing (useful when you know service has paid tiers)
//     - **WHEN TO USE**: Always for large services, recommended for all services to improve performance
// 
//     **CRITICAL REQUIREMENTS:**
//     - **NEVER GUESS VALUES**: Always use get_pricing_attribute_values() to discover valid options
//     - **EXHAUSTIVE ENUMERATION**: For cost optimization, list ALL qualifying tiers individually - they are mutually exclusive
//     - **USE SPECIFIC FILTERS**: Large services (EC2, RDS) require 2-3 filters minimum
//     - **NEVER USE MULTIPLE CALLS**: When ANY_OF can handle it in one call
//     - **VERIFY EXISTENCE**: Ensure all filter values exist in the service before querying
//     - **FOR "CHEAPEST" QUERIES**: Focus on lower-end options that meet minimum requirements, test incrementally
//     - **EXPLORE ALTERNATIVES**: When response includes "alternatives" field, MUST fetch their pricing if applicable to the use case before answering
// 
//     **CONSTRAINTS:**
//     - **CURRENT PRICING ONLY**: Use get_price_list_urls for historical data
//     - **NO SPOT/SAVINGS PLANS**: Only OnDemand, FlatRate, and Reserved Instance pricing available (ANY combination possible)
//     - **CHARACTER LIMIT**: 100,000 characters default response limit (use output_options to reduce)
//     - **REGION AUTO-FILTER**: Region parameter automatically creates regionCode filter
// 
//     **ANTI-PATTERNS:**
//     - DO NOT make multiple API calls that could be combined with ANY_OF
//     - DO NOT build cross-products manually when API can handle combinations
//     - DO NOT call get_pricing_service_codes() when service code is already known (e.g., "AmazonEC2")
//     - DO NOT use EQUALS without first checking get_pricing_attribute_values()
//     - DO NOT skip discovery workflow for any use case
//     - DO NOT use broad queries without specific filters on large services
//     - DO NOT assume attribute values exist across different services/regions
//     - DO NOT skip intermediate tiers: Missing 50GB, 59GB options when testing 32GB → 75GB jump
//     - DO NOT set upper bounds too high: Including 500GB+ storage when user needs ≥30GB (wastes character limit)
//     - DO NOT ignore alternatives field or use only ["OnDemand"] in output_options
// 
//     **EXAMPLE USE CASES:**
// 
//     **1. Cost-Optimized Multi-Attribute Filtering (CORRECT APPROACH):**
//     ```python
//     # Find cheapest EC2 instances meeting minimum requirements (>= 8 GiB memory, >= 30 GB storage)
//     # EXHAUSTIVE ENUMERATION of qualifying tiers - each is mutually exclusive
//     filters = [
//        {"Field": "memory", "Value": ["8 GiB", "16 GiB", "32 GiB"], "Type": "ANY_OF"},  # All tiers ≥8GB up to reasonable limit
//        {"Field": "storage", "Value": ["1 x 32 SSD", "1 x 60 SSD", "1 x 75 NVMe SSD"], "Type": "ANY_OF"},  # All tiers ≥30GB up to reasonable limit
//        {"Field": "instanceType", "Value": ["t2", "m4"], "Type": "NONE_OF"},  # Exclude older generations
//        {"Field": "tenancy", "Value": "Shared", "Type": "EQUALS"}  # Exclude more expensive dedicated
//     ]
//     pricing = get_pricing('AmazonEC2', 'us-east-1', filters)
//     ```
// 
//     **2. Efficient Multi-Region Comparison:**
//     ```python
//     # Compare same configuration across regions - use region parameter for multi-region
//     filters = [{"Field": "instanceType", "Value": "m5.large", "Type": "EQUALS"}]
//     pricing = get_pricing('AmazonEC2', ['us-east-1', 'us-west-2', 'eu-west-1'], filters)
//     ```
// 
//     **3. Large service with output optimization (recommended approach):**
//     ```python
//     output_options = {"pricing_terms": ["OnDemand", "FlatRate"], "product_attributes": ["instanceType", "location"], "exclude_free_products": true}
//     pricing = get_pricing('AmazonEC2', 'us-east-1', filters, output_options=output_options)
//     ```
// 
//     **4. Pattern-Based Discovery:**
//     ```python
//     # Find all Standard storage tiers except expensive ones
//     filters = [
//         {"Field": "storageClass", "Value": "Standard", "Type": "CONTAINS"},
//         {"Field": "storageClass", "Value": ["Standard-IA"], "Type": "NONE_OF"}
//     ]
//     ```
// 
//     **FILTERING STRATEGY:**
//     - **Large Services (EC2, RDS)**: ALWAYS use 2-3 specific filters to prevent 200+ record responses
//     - **Small Services**: May work with single filter or no filters
//     - **Multi-Option Analysis**: Use ANY_OF instead of multiple API calls
//     - **Pattern Discovery**: Use CONTAINS for finding families or tiers
//     - **Smart Exclusion**: Use NONE_OF for compliance or cost filtering
// 
//     **SUCCESS CRITERIA:**
//     - Used discovery workflow (skip get_pricing_service_codes() if service known)
//     - Applied appropriate filters for the service size
//     - Used exact values from get_pricing_attribute_values()
//     - Used ANY_OF for multi-option scenarios instead of multiple calls
//     - For cost optimization: tested ALL qualifying tiers exhaustively (in a reasonable range)
//     - Included ["OnDemand", "FlatRate"] in output_options and explored all alternatives
//     
//
// Usage: Import package and call with typed struct
// Note: This function connects to MCP server 'awslabs.aws-pricing-mcp-server'
//          output, err := GetPricing(GetPricingParams{
//              Max_allowed_characters: "value",
//              // ... other parameters
//          })
//
func GetPricing(params GetPricingParams) (string, error) {
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
		"tool":   "get_pricing",
		"args":   paramsMap,
	}
	return callAPI("/api/mcp/execute", payload)
}

