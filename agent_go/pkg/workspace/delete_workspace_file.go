package workspace

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// DeleteWorkspaceFileParams contains parameters for the delete_workspace_file tool
type DeleteWorkspaceFileParams struct {
	Filepath string `json:"filepath"`
}

// DeleteWorkspaceFile deletes a file from the workspace
func (c *Client) DeleteWorkspaceFile(ctx context.Context, params DeleteWorkspaceFileParams) (string, error) {
	if params.Filepath == "" {
		return "", fmt.Errorf("filepath is required")
	}

	// Validate path against folder guard (write operation)
	if err := c.ValidatePath(params.Filepath, true); err != nil {
		return "", err
	}

	// Build API URL with confirm parameter
	apiURL := c.BaseURL + "/api/documents/" + params.Filepath + "?confirm=true"

	req, err := http.NewRequestWithContext(ctx, "DELETE", apiURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

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

	// Return structured JSON for frontend parsing
	resultJSON := map[string]interface{}{
		"filepath": params.Filepath,
		"deleted":  true,
	}

	jsonBytes, err := json.Marshal(resultJSON)
	if err != nil {
		return "", fmt.Errorf("failed to marshal result: %w", err)
	}

	return string(jsonBytes), nil
}
