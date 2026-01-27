package workspace

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"time"
)

// FolderGuardConfig represents folder access restrictions
type FolderGuardConfig struct {
	Enabled      bool     `json:"enabled"`
	ReadPaths    []string `json:"read_paths"`
	WritePaths   []string `json:"write_paths"`
	BlockedPaths []string `json:"blocked_paths"`
}

// Client handles communication with the workspace API directly via REST
type Client struct {
	BaseURL     string
	HTTPClient  *http.Client
	FolderGuard *FolderGuardConfig
}

// ClientOption is a functional option for configuring the Client
type ClientOption func(*Client)

// WithFolderGuard sets the folder guard configuration for the client
func WithFolderGuard(config *FolderGuardConfig) ClientOption {
	return func(c *Client) {
		c.FolderGuard = config
	}
}

// WithHTTPClient sets a custom HTTP client
func WithHTTPClient(httpClient *http.Client) ClientOption {
	return func(c *Client) {
		c.HTTPClient = httpClient
	}
}

// NewClient creates a new workspace REST client with optional configuration
func NewClient(baseURL string, opts ...ClientOption) *Client {
	c := &Client{
		BaseURL:    strings.TrimRight(baseURL, "/"),
		HTTPClient: &http.Client{Timeout: 300 * time.Second},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// ValidatePath checks if a path is allowed based on folder guard configuration
// isWrite indicates if this is a write operation (more restrictive)
func (c *Client) ValidatePath(inputPath string, isWrite bool) error {
	if c.FolderGuard == nil || !c.FolderGuard.Enabled {
		return nil // No folder guard configured, allow all
	}

	// Normalize input path
	inputPath = filepath.Clean(inputPath)

	// Check blocked paths first
	for _, blocked := range c.FolderGuard.BlockedPaths {
		blocked = filepath.Clean(blocked)
		if isPathUnder(inputPath, blocked) {
			return fmt.Errorf("path %q is blocked", inputPath)
		}
	}

	// Determine allowed paths
	var allowedPaths []string
	if isWrite {
		allowedPaths = c.FolderGuard.WritePaths
	} else {
		// Read operations can use both read and write paths
		allowedPaths = append(c.FolderGuard.ReadPaths, c.FolderGuard.WritePaths...)
	}

	// Empty allowed paths means allow all (when folder guard is enabled but no paths specified)
	if len(allowedPaths) == 0 {
		return nil
	}

	// Check if path is under any allowed path
	for _, allowed := range allowedPaths {
		allowed = filepath.Clean(allowed)
		if isPathUnder(inputPath, allowed) {
			return nil
		}
	}

	opType := "read"
	if isWrite {
		opType = "write"
	}
	return fmt.Errorf("path %q is outside allowed %s boundaries (allowed: %v)", inputPath, opType, allowedPaths)
}

// isPathUnder checks if inputPath is equal to or under basePath
func isPathUnder(inputPath, basePath string) bool {
	// Exact match
	if inputPath == basePath {
		return true
	}

	// Check if input is under base path
	rel, err := filepath.Rel(basePath, inputPath)
	if err == nil && !strings.HasPrefix(rel, "..") {
		return true
	}

	// For relative input paths, check if they match the suffix of base path
	if !filepath.IsAbs(inputPath) {
		inputSegments := strings.Split(inputPath, string(filepath.Separator))
		baseSegments := strings.Split(basePath, string(filepath.Separator))

		if len(inputSegments) > 0 && len(baseSegments) > 0 {
			// Check if input's first segment matches base path's last segment
			inputFirst := inputSegments[0]
			baseLast := baseSegments[len(baseSegments)-1]
			if inputFirst == baseLast {
				return true
			}

			// Check if input matches trailing segments of base path
			for i := 0; i <= len(baseSegments)-len(inputSegments); i++ {
				match := true
				for j := 0; j < len(inputSegments); j++ {
					if baseSegments[i+j] != inputSegments[j] {
						match = false
						break
					}
				}
				if match {
					return true
				}
			}
		}
	}

	return false
}

// request executes a generic HTTP request and returns the response body
func (c *Client) request(ctx context.Context, method, path string, body interface{}) ([]byte, error) {
	var bodyReader io.Reader
	if body != nil {
		jsonData, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal request: %w", err)
		}
		bodyReader = bytes.NewBuffer(jsonData)
	}

	url := c.BaseURL + path
	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	return respBody, nil
}