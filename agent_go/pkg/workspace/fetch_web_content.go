package workspace

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	md "github.com/JohannesKaufmann/html-to-markdown"
)

// FetchWebContentParams contains parameters for the fetch_web_content tool
type FetchWebContentParams struct {
	URL               string            `json:"url"`
	Timeout           int               `json:"timeout,omitempty"`
	ConvertToMarkdown *bool             `json:"convert_to_markdown,omitempty"`
	Headers           map[string]string `json:"headers,omitempty"`
}

// FetchWebContent fetches content from a URL
func (c *Client) FetchWebContent(ctx context.Context, params FetchWebContentParams) (string, error) {
	if params.URL == "" {
		return "", fmt.Errorf("url is required")
	}

	// Validate URL format
	parsedURL, err := url.Parse(params.URL)
	if err != nil {
		return "", fmt.Errorf("invalid URL format: %w", err)
	}

	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return "", fmt.Errorf("URL must use http:// or https:// scheme, got: %s", parsedURL.Scheme)
	}

	// Set defaults
	timeout := params.Timeout
	if timeout < 1 {
		timeout = 30
	}
	if timeout > 120 {
		timeout = 120
	}

	convertToMarkdown := true
	if params.ConvertToMarkdown != nil {
		convertToMarkdown = *params.ConvertToMarkdown
	}

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, "GET", params.URL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	// Set default headers
	req.Header.Set("User-Agent", "MCP-Agent-Builder/1.0 (Web Fetch Tool)")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")

	// Apply custom headers
	for k, v := range params.Headers {
		req.Header.Set(k, v)
	}

	// Create HTTP client with timeout
	client := &http.Client{
		Timeout: time.Duration(timeout) * time.Second,
	}

	// Execute request
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to fetch URL: %w", err)
	}
	defer resp.Body.Close()

	// Limit response size to 10MB
	const maxSize = 10 * 1024 * 1024
	limitedReader := io.LimitReader(resp.Body, maxSize)

	body, err := io.ReadAll(limitedReader)
	if err != nil {
		return "", fmt.Errorf("failed to read response body: %w", err)
	}

	contentType := resp.Header.Get("Content-Type")
	content := string(body)

	// Convert HTML to markdown if requested
	isHTML := strings.Contains(strings.ToLower(contentType), "text/html") ||
		strings.Contains(strings.ToLower(contentType), "application/xhtml")

	if convertToMarkdown && isHTML {
		converter := md.NewConverter("", true, nil)
		markdown, err := converter.ConvertString(content)
		if err == nil {
			content = markdown
		}
	}

	// Truncate if too large (100KB for LLM)
	const maxContentSize = 100 * 1024
	truncated := false
	if len(content) > maxContentSize {
		content = content[:maxContentSize]
		truncated = true
	}

	response := map[string]interface{}{
		"url":          params.URL,
		"status_code":  resp.StatusCode,
		"content_type": contentType,
		"content":      content,
	}

	if truncated {
		response["truncated"] = true
		response["truncated_message"] = "Content was truncated to 100KB for LLM processing"
	}

	if resp.StatusCode >= 400 {
		response["error"] = fmt.Sprintf("HTTP error: %s", resp.Status)
	}

	responseJSON, err := json.Marshal(response)
	if err != nil {
		return "", fmt.Errorf("failed to marshal response: %w", err)
	}

	return string(responseJSON), nil
}
