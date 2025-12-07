package awslabs_aws_pricing_mcp_server_tools

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
func callAPI(endpoint string, payload map[string]interface{}) (string, error) {
	apiURL := os.Getenv("MCP_API_URL")
	if apiURL == "" {
		apiURL = "http://localhost:8000"
	}

	reqBody, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	// Create request with 300 second timeout (from agent ToolTimeout)
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "POST", apiURL+endpoint, bytes.NewBuffer(reqBody))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Success bool   `json:"success"`
		Result  string `json:"result"`
		Error   string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}

	if !result.Success {
		return "", fmt.Errorf("%s", result.Error)
	}
	return result.Result, nil
}

