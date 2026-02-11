package workspace

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// DiffPatchWorkspaceFileParams contains parameters for the diff_patch_workspace_file tool
type DiffPatchWorkspaceFileParams struct {
	Filepath string `json:"filepath"`
	Diff     string `json:"diff"`
}

// DiffPatchWorkspaceFile applies a unified diff patch to a file
func (c *Client) DiffPatchWorkspaceFile(ctx context.Context, params DiffPatchWorkspaceFileParams) (string, error) {
	if params.Filepath == "" {
		return "", fmt.Errorf("filepath is required")
	}
	if params.Diff == "" {
		return "", fmt.Errorf("diff is required")
	}

	// Validate path against folder guard (write operation)
	if err := c.ValidatePath(params.Filepath, true); err != nil {
		return "", err
	}

	// Build API URL for diff patching
	apiURL := c.BaseURL + "/api/documents/" + params.Filepath + "/diff"

	// Prepare request body
	requestBody := map[string]interface{}{
		"diff": params.Diff,
	}

	jsonBody, err := json.Marshal(requestBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "PATCH", apiURL, strings.NewReader(string(jsonBody)))
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

	responseData, err := json.Marshal(apiResp.Data)
	if err != nil {
		return "", fmt.Errorf("failed to marshal API response: %w", err)
	}

	return string(responseData), nil
}
