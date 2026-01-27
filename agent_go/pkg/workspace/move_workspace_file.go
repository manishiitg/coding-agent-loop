package workspace

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// MoveWorkspaceFileParams contains parameters for the move_workspace_file tool
type MoveWorkspaceFileParams struct {
	SourceFilepath      string `json:"source_filepath"`
	DestinationFilepath string `json:"destination_filepath"`
	CommitMessage       string `json:"commit_message,omitempty"`
}

// MoveWorkspaceFile moves a file from one location to another
func (c *Client) MoveWorkspaceFile(ctx context.Context, params MoveWorkspaceFileParams) (string, error) {
	if params.SourceFilepath == "" {
		return "", fmt.Errorf("source_filepath is required")
	}
	if params.DestinationFilepath == "" {
		return "", fmt.Errorf("destination_filepath is required")
	}

	// Validate both paths against folder guard (write operations)
	if err := c.ValidatePath(params.SourceFilepath, true); err != nil {
		return "", fmt.Errorf("source path validation failed: %w", err)
	}
	if err := c.ValidatePath(params.DestinationFilepath, true); err != nil {
		return "", fmt.Errorf("destination path validation failed: %w", err)
	}

	// Build API URL for moving documents
	apiURL := c.BaseURL + "/api/documents/" + params.SourceFilepath + "/move"

	// Prepare request body
	requestBody := map[string]interface{}{
		"destination_path": params.DestinationFilepath,
	}
	if params.CommitMessage != "" {
		requestBody["commit_message"] = params.CommitMessage
	}

	jsonBody, err := json.Marshal(requestBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", apiURL, strings.NewReader(string(jsonBody)))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to call workspace API: %w", err)
	}
	defer resp.Body.Close()

	body, err := readResponseBody(resp)
	if err != nil {
		return "", err
	}

	var apiResp APIResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return "", fmt.Errorf("failed to parse API response: %w", err)
	}

	if !apiResp.Success {
		return "", fmt.Errorf("workspace API error: %s", apiResp.Error)
	}

	// Format the response
	var result strings.Builder
	result.WriteString(fmt.Sprintf("**File Moved: `%s` -> `%s`**\n\n", params.SourceFilepath, params.DestinationFilepath))

	if params.CommitMessage != "" {
		result.WriteString(fmt.Sprintf("**Commit Message**: %s\n", params.CommitMessage))
	}

	result.WriteString("**Status**: File successfully moved to new location")

	return result.String(), nil
}
