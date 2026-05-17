package workspace

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"path/filepath"
	"strings"
)

type ReadVideoParams struct {
	Filepath string `json:"filepath"`
	Query    string `json:"query"`
	Provider string `json:"provider,omitempty"`
	ModelID  string `json:"model_id,omitempty"`
}

// ReadVideoResult is the structured result from ReadVideo (pure I/O).
// The Kimi/Moonshot upload and LLM call are handled by the virtual-tools wrapper.
type ReadVideoResult struct {
	Filepath string `json:"filepath"`
	Query    string `json:"query"`
	Provider string `json:"provider,omitempty"`
	ModelID  string `json:"model_id,omitempty"`
	MimeType string `json:"mime_type"`
	Data     string `json:"data"` // base64-encoded video bytes
}

var supportedVideoReadExtensions = map[string]bool{
	".mp4":  true,
	".mpeg": true,
	".mov":  true,
	".avi":  true,
	".flv":  true,
	".mpg":  true,
	".webm": true,
	".wmv":  true,
	".3gp":  true,
	".3gpp": true,
}

func GetVideoMimeType(filename string) string {
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".mp4":
		return "video/mp4"
	case ".mpeg", ".mpg":
		return "video/mpeg"
	case ".mov":
		return "video/quicktime"
	case ".avi":
		return "video/x-msvideo"
	case ".flv":
		return "video/x-flv"
	case ".webm":
		return "video/webm"
	case ".wmv":
		return "video/x-ms-wmv"
	case ".3gp", ".3gpp":
		return "video/3gpp"
	default:
		return "video/mp4"
	}
}

// ReadVideo reads a video file from the workspace and returns base64-encoded data.
// This is intentionally pure I/O; model upload/analysis is handled by virtual tools.
func (c *Client) ReadVideo(ctx context.Context, params ReadVideoParams) (string, error) {
	log.Printf("[READ_VIDEO] ReadVideo called: filepath=%q, query=%q", params.Filepath, params.Query)

	if params.Filepath == "" {
		return "", fmt.Errorf("filepath is required")
	}
	if params.Query == "" {
		return "", fmt.Errorf("query is required")
	}
	absolutePath, guardPath, err := normalizeWorkspaceAbsoluteToolPath(params.Filepath, "filepath", "read_video")
	if err != nil {
		return "", err
	}

	if err := c.ValidatePathWithContext(ctx, guardPath, false); err != nil {
		return "", err
	}

	ext := strings.ToLower(filepath.Ext(absolutePath))
	if !supportedVideoReadExtensions[ext] {
		return "", fmt.Errorf("unsupported video format (got extension: %s). Supported: mp4, mpeg, mov, avi, flv, mpg, webm, wmv, 3gp, 3gpp", ext)
	}

	pathSegments := strings.Split(filepath.ToSlash(guardPath), "/")
	encodedSegments := make([]string, len(pathSegments))
	for i, segment := range pathSegments {
		encodedSegments[i] = url.PathEscape(segment)
	}
	encodedPath := strings.Join(encodedSegments, "/")

	rawData, err := c.request(ctx, "GET", "/api/documents/"+encodedPath+"/raw", nil)
	if err != nil {
		return "", fmt.Errorf("failed to read video file: %w. Use execute_shell_command to verify the path, for example with 'ls' or 'find'.", err)
	}

	const maxVideoSize = 100 * 1024 * 1024
	if len(rawData) > maxVideoSize {
		return "", fmt.Errorf("video file too large (%d bytes, max %d bytes)", len(rawData), maxVideoSize)
	}

	result := ReadVideoResult{
		Filepath: absolutePath,
		Query:    params.Query,
		Provider: strings.TrimSpace(params.Provider),
		ModelID:  strings.TrimSpace(params.ModelID),
		MimeType: GetVideoMimeType(absolutePath),
		Data:     base64.StdEncoding.EncodeToString(rawData),
	}

	responseJSON, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("failed to marshal response: %w", err)
	}

	return string(responseJSON), nil
}
