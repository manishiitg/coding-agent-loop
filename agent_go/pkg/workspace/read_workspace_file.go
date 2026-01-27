package workspace

import (
	"context"
	"encoding/json"
	"fmt"
)

type ReadWorkspaceFileParams struct {
	Filepath string `json:"filepath"`
}

// DocumentResponse represents the response from reading a document
type DocumentResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message,omitempty"`
	Data    struct {
		Filepath string `json:"filepath"`
		Content  string `json:"content"`
		Folder   string `json:"folder,omitempty"`
		Type     string `json:"type,omitempty"`
		IsImage  bool   `json:"is_image,omitempty"`
	} `json:"data,omitempty"`
	Error string `json:"error,omitempty"`
}

// ReadWorkspaceFile reads a file using the REST API: GET /api/documents/*filepath
// Returns the file content as a JSON object with filepath and content fields
func (c *Client) ReadWorkspaceFile(ctx context.Context, params ReadWorkspaceFileParams) (string, error) {
	// Validate path against folder guard (read operation)
	if err := c.ValidatePath(params.Filepath, false); err != nil {
		return "", err
	}

	path := fmt.Sprintf("/api/documents/%s", params.Filepath)
	respBody, err := c.request(ctx, "GET", path, nil)
	if err != nil {
		return "", err
	}

	// Parse the API response to extract the document data
	var apiResp DocumentResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return "", fmt.Errorf("failed to parse API response: %w", err)
	}

	// Check if the API returned an error (file not found, etc.)
	if apiResp.Error != "" {
		return "", fmt.Errorf("file not found: %s", params.Filepath)
	}

	// Check if there's actual content
	if apiResp.Data.Content == "" && apiResp.Message == "File does not exist" {
		return "", fmt.Errorf("file not found: %s", params.Filepath)
	}

	// Return the document data as JSON (matching the expected format by callers)
	resultData := map[string]interface{}{
		"filepath": apiResp.Data.Filepath,
		"content":  apiResp.Data.Content,
	}
	if apiResp.Data.Folder != "" {
		resultData["folder"] = apiResp.Data.Folder
	}
	if apiResp.Data.Type != "" {
		resultData["type"] = apiResp.Data.Type
	}
	if apiResp.Data.IsImage {
		resultData["is_image"] = apiResp.Data.IsImage
	}

	resultJSON, err := json.Marshal(resultData)
	if err != nil {
		return "", fmt.Errorf("failed to marshal result: %w", err)
	}

	return string(resultJSON), nil
}