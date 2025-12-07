package virtual_tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// callAPI makes an HTTP POST request to the specified endpoint with the given payload
// This is a common function used by all tool functions to reduce code duplication
// Panics on API errors (network, HTTP failures) - tool execution errors are in result string
func callAPI(endpoint string, payload map[string]interface{}) string {
	apiURL := os.Getenv("MCP_API_URL")
	if apiURL == "" {
		apiURL = "http://localhost:8000"
	}

	reqBody, err := json.Marshal(payload)
	if err != nil {
		panic(fmt.Sprintf("failed to marshal request: %v", err))
	}

	// Create request with 300 second timeout (from agent ToolTimeout)
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "POST", apiURL+endpoint, bytes.NewBuffer(reqBody))
	if err != nil {
		panic(fmt.Sprintf("failed to create request: %v", err))
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		panic(fmt.Sprintf("HTTP request failed: %v", err))
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		panic(fmt.Sprintf("HTTP %d: %s", resp.StatusCode, string(body)))
	}

	var result struct {
		Success bool   `json:"success"`
		Result  string `json:"result"`
		Error   string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		panic(fmt.Sprintf("failed to decode response: %v", err))
	}

	if !result.Success {
		panic(fmt.Sprintf("API error: %s", result.Error))
	}
	// Return result string - tool execution errors will be in result (check output for error indicators)
	return result.Result
}

