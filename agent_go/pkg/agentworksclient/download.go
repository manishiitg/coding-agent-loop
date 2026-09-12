package agentworksclient

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
)

// Download streams a workflow asset to a new local file. Atomic publication
// avoids partial outputs and refuses to overwrite an existing file or symlink.
func (c *Client) Download(ctx context.Context, workflowID, path, output string) (int64, error) {
	if workflowID == "" || path == "" || output == "" {
		return 0, errors.New("workflow, path and output are required")
	}
	destination, err := filepath.Abs(output)
	if err != nil {
		return 0, err
	}
	if _, err = os.Lstat(destination); err == nil {
		return 0, errors.New("output already exists; choose a new path")
	} else if !os.IsNotExist(err) {
		return 0, err
	}
	q := url.Values{"workflow_id": {workflowID}, "path": {path}, "download": {"true"}}
	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/api/external/v1/files/content?"+q.Encode(), nil)
	if err != nil {
		return 0, err
	}
	if c.token == "" {
		return 0, errors.New("login is required")
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	response, err := c.http.Do(req)
	if err != nil {
		return 0, err
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		var envelope struct {
			Error struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		_ = json.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(&envelope)
		if envelope.Error.Code == "" {
			envelope.Error.Code = "download_failed"
		}
		if envelope.Error.Message == "" {
			envelope.Error.Message = "Asset download failed"
		}
		return 0, &APIError{Status: response.StatusCode, Code: envelope.Error.Code, Message: envelope.Error.Message}
	}
	temp, err := os.CreateTemp(filepath.Dir(destination), ".agentworks-download-*")
	if err != nil {
		return 0, err
	}
	defer os.Remove(temp.Name())
	defer temp.Close()
	n, err := io.Copy(temp, response.Body)
	if err != nil {
		return n, err
	}
	if err = temp.Sync(); err != nil {
		return n, err
	}
	if err = temp.Close(); err != nil {
		return n, err
	}
	if err = os.Link(temp.Name(), destination); err != nil {
		return n, err
	}
	return n, nil
}
