package workspace

import (
	"fmt"
	"io"
	"net/http"
)

// APIResponse represents the standard workspace API response wrapper
type APIResponse struct {
	Success bool        `json:"success"`
	Message string      `json:"message,omitempty"`
	Data    interface{} `json:"data,omitempty"`
	Error   string      `json:"error,omitempty"`
}

// readResponseBody reads and validates HTTP response
func readResponseBody(resp *http.Response) ([]byte, error) {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("workspace API returned status %d: %s", resp.StatusCode, string(body))
	}

	return body, nil
}

// Helper functions for safe type conversion from map[string]interface{}

func getStringFromMap(m map[string]interface{}, key string) string {
	if val, exists := m[key]; exists {
		if str, ok := val.(string); ok {
			return str
		}
	}
	return ""
}

func getFloatFromMap(m map[string]interface{}, key string) float64 {
	if val, exists := m[key]; exists {
		switch v := val.(type) {
		case float64:
			return v
		case int:
			return float64(v)
		}
	}
	return 0.0
}
