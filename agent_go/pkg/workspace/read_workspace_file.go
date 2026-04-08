package workspace

import (
	"context"
	"encoding/json"
	"fmt"
)

type ReadWorkspaceFileParams struct {
	Filepath string `json:"filepath"`
}

// documentAPIResponse represents the raw response from the workspace documents API (internal)
type documentAPIResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message,omitempty"`
	Data    struct {
		Filepath string `json:"filepath"`
		Content  string `json:"content"`
		Type     string `json:"type,omitempty"`
		IsImage  bool   `json:"is_image,omitempty"`
	} `json:"data,omitempty"`
	Error string `json:"error,omitempty"`
}

// ReadWorkspaceFile reads a file using the REST API: GET /api/documents/*filepath
func (c *Client) ReadWorkspaceFile(ctx context.Context, params ReadWorkspaceFileParams) (ReadFileResult, error) {
	// Validate path against folder guard (read operation)
	if err := c.ValidatePath(params.Filepath, false); err != nil {
		return ReadFileResult{}, err
	}

	path := fmt.Sprintf("/api/documents/%s", params.Filepath)
	respBody, err := c.request(ctx, "GET", path, nil)
	if err != nil {
		return ReadFileResult{}, err
	}

	// Parse the API response to extract the document data
	var apiResp documentAPIResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return ReadFileResult{}, fmt.Errorf("failed to parse API response: %w", err)
	}

	// Check if the API returned an error (file not found, etc.)
	if apiResp.Error != "" {
		return ReadFileResult{}, fmt.Errorf("file not found: %s", params.Filepath)
	}

	// Check if there's actual content
	if apiResp.Data.Content == "" && apiResp.Message == "File does not exist" {
		return ReadFileResult{}, fmt.Errorf("file not found: %s", params.Filepath)
	}

	return ReadFileResult{
		Filepath: apiResp.Data.Filepath,
		Content:  apiResp.Data.Content,
		Type:     apiResp.Data.Type,
		IsImage:  apiResp.Data.IsImage,
	}, nil
}
