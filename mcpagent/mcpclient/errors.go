package mcpclient

import "strings"

// IsBrokenPipeError checks if an error is a broken pipe error
// This is used to detect connection issues that require reconnection
func IsBrokenPipeError(err error) bool {
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

// IsBrokenPipeInContent checks if a string contains broken pipe error indicators
// This is used when the error is embedded in tool result content rather than returned as an error
func IsBrokenPipeInContent(content string) bool {
	return strings.Contains(content, "Broken pipe") ||
		strings.Contains(content, "[Errno 32]")
}
