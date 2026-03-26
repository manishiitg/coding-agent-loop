package workspace

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"mcp-agent-builder-go/agent_go/pkg/common"
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
	BaseURL            string
	HTTPClient         *http.Client
	FolderGuard        *FolderGuardConfig
	UserID             string            // User ID for per-user folder isolation
	ExtraEnv           map[string]string // Extra env vars to inject into shell commands (e.g., MCP_API_URL, MCP_API_TOKEN)
	DefaultWorkingDir  string            // Default working directory for shell commands (relative to docs-dir)
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

// WithUserID sets the user ID for per-user folder isolation
// When set, the client includes X-User-ID header in all requests
// The workspace API uses this to route per-user folders (Chats/, Downloads/)
// to /_users/{userID}/ while keeping shared folders at root
func WithUserID(userID string) ClientOption {
	return func(c *Client) {
		c.UserID = userID
	}
}

// WithExtraEnv sets extra environment variables to inject into shell commands.
// Only MCP_* and SECRET_* prefixed vars are forwarded to the shell (enforced server-side).
func WithExtraEnv(env map[string]string) ClientOption {
	return func(c *Client) {
		c.ExtraEnv = env
	}
}

// WithDefaultWorkingDir sets the default working directory for shell commands
// (relative to docs-dir, e.g., "Chats/", "Workflow/my-project/").
func WithDefaultWorkingDir(dir string) ClientOption {
	return func(c *Client) {
		c.DefaultWorkingDir = dir
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

	// Check if input is under base path using filepath.Rel
	// This works correctly when both paths are the same type (both relative or both absolute)
	rel, err := filepath.Rel(basePath, inputPath)
	if err == nil && !strings.HasPrefix(rel, "..") {
		return true
	}

	// Handle mixed relative/absolute case:
	// If input is relative and base is absolute (or vice versa), check if the base path
	// ends with the input path at a directory boundary. This handles the common case where
	// folder guard has absolute paths like "/workspace/docs" and the agent sends "docs/file.txt".
	if !filepath.IsAbs(inputPath) && filepath.IsAbs(basePath) {
		// Check if input path equals the base path's basename or is a subpath of it
		// e.g., base="/workspace/docs", input="docs" → match
		// e.g., base="/workspace/docs", input="docs/file.txt" → match
		// e.g., base="/workspace/src/docs", input="docs" → NO match (ambiguous)
		baseName := filepath.Base(basePath)
		inputSegments := strings.Split(filepath.Clean(inputPath), string(filepath.Separator))

		if len(inputSegments) > 0 && inputSegments[0] == baseName {
			// The input starts with the base's last segment. To avoid ambiguity
			// (e.g., "/a/docs" vs "/b/docs" both ending in "docs"), only match if
			// the base path has exactly one trailing segment that matches.
			// Construct the expected relative path from base's parent and check.
			parentDir := filepath.Dir(basePath)
			resolvedInput := filepath.Join(parentDir, inputPath)
			resolvedInput = filepath.Clean(resolvedInput)
			relCheck, err := filepath.Rel(basePath, resolvedInput)
			if err == nil && !strings.HasPrefix(relCheck, "..") {
				return true
			}
		}
	}

	return false
}

// getUserIDFromContext extracts user ID from context or returns the static UserID
func (c *Client) getUserIDFromContext(ctx context.Context) string {
	// First check if user ID is set on the client directly
	if c.UserID != "" {
		log.Printf("[USER_ID_DEBUGGING] getUserIDFromContext: using client.UserID=%q", c.UserID)
		return c.UserID
	}

	// Then check the context for user ID (set by auth middleware)
	if userID, ok := ctx.Value(common.UserIDKey).(string); ok && userID != "" {
		log.Printf("[USER_ID_DEBUGGING] getUserIDFromContext: using context UserIDKey=%q", userID)
		return userID
	}

	// Return empty string - workspace API will use default user
	log.Printf("[USER_ID_DEBUGGING] WARNING: no user ID available (client.UserID empty, context key missing)")
	return ""
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

	// Include user ID header for per-user folder isolation
	// Check both static UserID and context-based user ID
	if userID := c.getUserIDFromContext(ctx); userID != "" {
		req.Header.Set("X-User-ID", userID)
		log.Printf("[USER_ID_DEBUGGING] HTTP request: %s %s with X-User-ID=%q", method, path, userID)
	} else {
		log.Printf("[USER_ID_DEBUGGING] HTTP request: %s %s with NO X-User-ID header", method, path)
	}

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

// UploadBinary uploads raw binary data as a file via the workspace upload endpoint.
// folderPath is the destination folder (e.g. "Chats/generated-images").
// fileName is the file name including extension (e.g. "image-1234.png").
// data is the raw binary content.
// Returns the saved workspace filepath on success.
func (c *Client) UploadBinary(ctx context.Context, folderPath, fileName string, data []byte) (string, error) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)

	// Add folder_path form field
	if err := mw.WriteField("folder_path", folderPath); err != nil {
		return "", fmt.Errorf("write folder_path field: %w", err)
	}

	// Add file form field
	fw, err := mw.CreateFormFile("file", fileName)
	if err != nil {
		return "", fmt.Errorf("create form file: %w", err)
	}
	if _, err := fw.Write(data); err != nil {
		return "", fmt.Errorf("write file data: %w", err)
	}
	mw.Close()

	url := c.BaseURL + "/api/upload"
	req, err := http.NewRequestWithContext(ctx, "POST", url, &buf)
	if err != nil {
		return "", fmt.Errorf("create upload request: %w", err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	if userID := c.getUserIDFromContext(ctx); userID != "" {
		req.Header.Set("X-User-ID", userID)
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("execute upload request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read upload response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("upload HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		FilePath string `json:"filepath"`
	}
	if err := json.Unmarshal(respBody, &result); err == nil && result.FilePath != "" {
		return result.FilePath, nil
	}
	// Fallback: construct path manually
	return folderPath + "/" + fileName, nil
}

// DownloadFile downloads a file from the workspace and returns its raw bytes.
// filePath is the workspace path (e.g. "Chats/generated-images/image-1234.png").
func (c *Client) DownloadFile(ctx context.Context, filePath string) ([]byte, error) {
	encodedPath := strings.ReplaceAll(filePath, "/", "%2F")
	return c.request(ctx, "GET", "/api/documents/"+encodedPath+"/raw", nil)
}

// CreateFolder creates a folder via the workspace API: POST /api/folders
func (c *Client) CreateFolder(ctx context.Context, folderPath string) error {
	body := map[string]string{"folder_path": folderPath}
	_, err := c.request(ctx, "POST", "/api/folders", body)
	return err
}
