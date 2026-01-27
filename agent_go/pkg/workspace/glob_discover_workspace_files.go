package workspace

import (
	"context"
	"fmt"
	"net/url"
)

type GlobDiscoverWorkspaceFilesParams struct {
	Pattern     string  `json:"pattern"`
	Folder      *string `json:"folder,omitempty"`
	IncludeDirs *bool   `json:"include_dirs,omitempty"`
	MaxDepth    *int    `json:"max_depth,omitempty"`
}

// GlobDiscoverWorkspaceFiles searches files using the REST API: GET /api/glob
func (c *Client) GlobDiscoverWorkspaceFiles(ctx context.Context, params GlobDiscoverWorkspaceFilesParams) (string, error) {
	// Validate folder path against folder guard (read operation)
	if params.Folder != nil && *params.Folder != "" {
		if err := c.ValidatePath(*params.Folder, false); err != nil {
			return "", err
		}
	}

	query := url.Values{}
	query.Set("pattern", params.Pattern)
	if params.Folder != nil {
		query.Set("folder", *params.Folder)
	}
	if params.IncludeDirs != nil {
		query.Set("include_dirs", fmt.Sprintf("%v", *params.IncludeDirs))
	}
	if params.MaxDepth != nil {
		query.Set("max_depth", fmt.Sprintf("%d", *params.MaxDepth))
	}

	path := "/api/glob?" + query.Encode()
	respBody, err := c.request(ctx, "GET", path, nil)
	if err != nil {
		return "", err
	}

	return string(respBody), nil
}