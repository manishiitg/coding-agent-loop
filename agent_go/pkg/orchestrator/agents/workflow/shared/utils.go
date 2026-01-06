package shared

import "time"

// GetTimestamp returns current time in RFC3339 format
func GetTimestamp() string {
	return time.Now().Format(time.RFC3339)
}
