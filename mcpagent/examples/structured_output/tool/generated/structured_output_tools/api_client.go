package structured_output_tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// isBrokenPipeError checks if an error is a broken pipe error
// This is used to detect connection issues that require retry
func isBrokenPipeError(err error) bool {
	if err == nil {
		return false
	}
	errorMessage := err.Error()
	return strings.Contains(errorMessage, "Broken pipe") ||
		strings.Contains(errorMessage, "broken pipe") ||
		strings.Contains(errorMessage, "[Errno 32]") ||
		strings.Contains(errorMessage, "EOF") ||
		strings.Contains(errorMessage, "connection reset")
}

// isBrokenPipeInContent checks if a string contains broken pipe error indicators
// This is used when the error is embedded in tool result content rather than returned as an error
func isBrokenPipeInContent(content string) bool {
	return strings.Contains(content, "Broken pipe") ||
		strings.Contains(content, "[Errno 32]")
}

// callAPI makes an HTTP POST request to the specified endpoint with the given payload
// This is a common function used by all tool functions to reduce code duplication
// Panics on API errors (network, HTTP failures) - tool execution errors are in result string
// Includes broken pipe retry logic: retries once with 100ms delay if broken pipe detected
func callAPI(endpoint string, payload map[string]interface{}) string {
	apiURL := os.Getenv("MCP_API_URL")
	if apiURL == "" {
		apiURL = "http://localhost:8000"
	}

	reqBody, err := json.Marshal(payload)
	if err != nil {
		panic(fmt.Sprintf("failed to marshal request: %v", err))
	}

	// Retry logic for broken pipe errors
	maxRetries := 2
	for attempt := 0; attempt < maxRetries; attempt++ {
		// Create request with 300 second timeout (from agent ToolTimeout)
		ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
		
		req, err := http.NewRequestWithContext(ctx, "POST", apiURL+endpoint, bytes.NewBuffer(reqBody))
		if err != nil {
			cancel()
			panic(fmt.Sprintf("failed to create request: %v", err))
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := http.DefaultClient.Do(req)
		
		// Check for broken pipe error in HTTP request
		if err != nil {
			cancel()
			if isBrokenPipeError(err) && attempt < maxRetries-1 {
				// Broken pipe detected - retry with short delay
				time.Sleep(100 * time.Millisecond)
				continue
			}
			panic(fmt.Sprintf("HTTP request failed: %v", err))
		}

		// Read response body
		bodyBytes, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		cancel()

		if readErr != nil {
			if isBrokenPipeError(readErr) && attempt < maxRetries-1 {
				// Broken pipe detected while reading response - retry
				time.Sleep(100 * time.Millisecond)
				continue
			}
			panic(fmt.Sprintf("failed to read response body: %v", readErr))
		}

		if resp.StatusCode != http.StatusOK {
			bodyStr := string(bodyBytes)
			if isBrokenPipeInContent(bodyStr) && attempt < maxRetries-1 {
				// Broken pipe detected in error response - retry
				time.Sleep(100 * time.Millisecond)
				continue
			}
			panic(fmt.Sprintf("HTTP %d: %s", resp.StatusCode, bodyStr))
		}

		var result struct {
			Success bool   `json:"success"`
			Result  string `json:"result"`
			Error   string `json:"error"`
		}
		if err := json.Unmarshal(bodyBytes, &result); err != nil {
			panic(fmt.Sprintf("failed to decode response: %v", err))
		}

		if !result.Success {
			// Check for broken pipe in error message or result
			if (isBrokenPipeInContent(result.Error) || isBrokenPipeInContent(result.Result)) && attempt < maxRetries-1 {
				// Broken pipe detected in error/result - retry
				time.Sleep(100 * time.Millisecond)
				continue
			}
			panic(fmt.Sprintf("API error: %s", result.Error))
		}

		// Check for broken pipe in successful result content
		if isBrokenPipeInContent(result.Result) && attempt < maxRetries-1 {
			// Broken pipe detected in result content - retry
			time.Sleep(100 * time.Millisecond)
			continue
		}

		// Return result string - tool execution errors will be in result (check output for error indicators)
		return result.Result
	}

	// Should never reach here, but panic if we do
	panic("callAPI: max retries exceeded without returning")
}

