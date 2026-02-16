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

type ReadImageParams struct {
	Filepath string `json:"filepath"`
	Query    string `json:"query"`
}

// ReadImageResult is the structured result from ReadImage (pure I/O).
// The LLM call is handled by the wrapper in virtual-tools, not here.
type ReadImageResult struct {
	Filepath string `json:"filepath"`
	Query    string `json:"query"`
	MimeType string `json:"mime_type"`
	Data     string `json:"data"` // base64-encoded image bytes
}

// Supported image extensions for the read_image tool
var supportedImageExtensions = map[string]bool{
	".png":  true,
	".jpg":  true,
	".jpeg": true,
	".gif":  true,
	".bmp":  true,
	".webp": true,
	".svg":  true,
	".ico":  true,
}

// GetImageMimeType returns the MIME type for an image file based on its extension.
func GetImageMimeType(filename string) string {
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".bmp":
		return "image/bmp"
	case ".webp":
		return "image/webp"
	case ".svg":
		return "image/svg+xml"
	case ".ico":
		return "image/x-icon"
	default:
		return "image/png"
	}
}

// ReadImage reads an image file from the workspace and returns the base64-encoded data.
// This is a pure I/O operation — no LLM call is made here.
// The LLM analysis is handled by the wrapper in virtual-tools/workspace_advanced_tools.go.
func (c *Client) ReadImage(ctx context.Context, params ReadImageParams) (string, error) {
	log.Printf("[READ_IMAGE_DEBUG] ReadImage called: filepath=%q, query=%q", params.Filepath, params.Query)

	if params.Filepath == "" {
		return "", fmt.Errorf("filepath is required")
	}
	if params.Query == "" {
		return "", fmt.Errorf("query is required")
	}

	// Validate path against folder guard (read operation)
	if err := c.ValidatePath(params.Filepath, false); err != nil {
		log.Printf("[READ_IMAGE_DEBUG] Path validation failed: %v", err)
		return "", err
	}

	// Validate file extension
	ext := strings.ToLower(filepath.Ext(params.Filepath))
	if !supportedImageExtensions[ext] {
		log.Printf("[READ_IMAGE_DEBUG] Unsupported extension: %s", ext)
		return "", fmt.Errorf("unsupported image format (got extension: %s). Supported: png, jpg, jpeg, gif, bmp, webp, svg, ico", ext)
	}

	// URL-encode the filepath segments
	pathSegments := strings.Split(params.Filepath, "/")
	encodedSegments := make([]string, len(pathSegments))
	for i, segment := range pathSegments {
		encodedSegments[i] = url.PathEscape(segment)
	}
	encodedPath := strings.Join(encodedSegments, "/")

	// Fetch raw image bytes using c.request() which adds X-User-ID header
	apiPath := "/api/documents/" + encodedPath + "/raw"
	log.Printf("[READ_IMAGE_DEBUG] Fetching raw image from workspace API: %s", apiPath)
	rawData, err := c.request(ctx, "GET", apiPath, nil)
	if err != nil {
		log.Printf("[READ_IMAGE_DEBUG] Workspace API request failed: %v", err)
		return "", fmt.Errorf("failed to read image file: %w. Use 'list_workspace_files' to find the correct path", err)
	}

	log.Printf("[READ_IMAGE_DEBUG] Raw image bytes received: %d bytes", len(rawData))

	// Cap at ~10MB
	const maxImageSize = 10 * 1024 * 1024
	if len(rawData) > maxImageSize {
		log.Printf("[READ_IMAGE_DEBUG] Image too large: %d bytes (max %d)", len(rawData), maxImageSize)
		return "", fmt.Errorf("image file too large (%d bytes, max %d bytes)", len(rawData), maxImageSize)
	}

	// Determine MIME type and base64-encode
	mimeType := GetImageMimeType(params.Filepath)
	base64Data := base64.StdEncoding.EncodeToString(rawData)

	log.Printf("[READ_IMAGE_DEBUG] Image encoded: mimeType=%s, base64Length=%d", mimeType, len(base64Data))

	// Return structured result — the virtual-tools wrapper will handle the LLM call
	result := ReadImageResult{
		Filepath: params.Filepath,
		Query:    params.Query,
		MimeType: mimeType,
		Data:     base64Data,
	}

	responseJSON, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("failed to marshal response: %w", err)
	}

	log.Printf("[READ_IMAGE_DEBUG] ReadImage I/O complete, returning base64 data for LLM processing")
	return string(responseJSON), nil
}
