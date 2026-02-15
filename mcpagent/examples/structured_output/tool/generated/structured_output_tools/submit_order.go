package structured_output_tools

import (
	"encoding/json"
	"fmt"
)

type SubmitOrderParams struct {
	Total_price float64 `json:"total_price"`
	Customer_id string `json:"customer_id"`
	Items []map[string]interface{} `json:"items"`
	Order_id string `json:"order_id"`
	Status string `json:"status"`
}

// Submit an order with customer ID, items, and total price
//
// Usage: Import package and call with typed struct
//       Panics on API errors - check output string for tool execution errors
// Example: output := SubmitOrder(SubmitOrderParams{
//     Total_price: "value",
//     // ... other parameters
// })
// // Check output for errors (e.g., strings.HasPrefix(output, "Error:"))
// // Handle tool execution error if detected
//
func SubmitOrder(params SubmitOrderParams) string {
	// Convert params struct to map for API call
	paramsBytes, err := json.Marshal(params)
	if err != nil {
		panic(fmt.Sprintf("failed to marshal parameters: %%v", err))
	}
	var paramsMap map[string]interface{}
	if err := json.Unmarshal(paramsBytes, &paramsMap); err != nil {
		panic(fmt.Sprintf("failed to unmarshal parameters: %%v", err))
	}

	// Build request payload and call common API client
	payload := map[string]interface{}{
		"tool": "submit_order",
		"args": paramsMap,
	}
	return callAPI("/api/custom/execute", payload)
}

