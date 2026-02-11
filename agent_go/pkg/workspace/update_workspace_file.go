package workspace

import (
	"context"
	"fmt"
	"net/url"
	"strings"
)

type UpdateWorkspaceFileParams struct {
	Filepath string `json:"filepath"`
	Content  string `json:"content"`
}

// UpdateWorkspaceFile creates or updates a file using the REST API: PUT /api/documents/{filepath}
func (c *Client) UpdateWorkspaceFile(ctx context.Context, params UpdateWorkspaceFileParams) (string, error) {
	if params.Filepath == "" {
		return "", fmt.Errorf("filepath is required")
	}

	// Validate path against folder guard (write operation)
	if err := c.ValidatePath(params.Filepath, true); err != nil {
		return "", err
	}

	// URL-encode the filepath segments
	pathSegments := strings.Split(params.Filepath, "/")
	encodedSegments := make([]string, len(pathSegments))
	for i, segment := range pathSegments {
		encodedSegments[i] = url.PathEscape(segment)
	}
	encodedPath := strings.Join(encodedSegments, "/")

	// Build request body
	requestBody := map[string]interface{}{
		"content": params.Content,
	}

	// Use PUT to /api/documents/{filepath} for update/create
	path := fmt.Sprintf("/api/documents/%s", encodedPath)
	respBody, err := c.request(ctx, "PUT", path, requestBody)
	if err != nil {
		return "", err
	}

	return string(respBody), nil
}
