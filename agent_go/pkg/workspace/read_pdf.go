package workspace

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
)

// ReadPDFParams contains parameters for the read_pdf tool
type ReadPDFParams struct {
	Filepath  string `json:"filepath"`
	PageRange string `json:"page_range,omitempty"`
	MaxPages  int    `json:"max_pages,omitempty"`
	Password  string `json:"password,omitempty"`
}

// ReadPDF reads and extracts text content from a PDF file
func (c *Client) ReadPDF(ctx context.Context, params ReadPDFParams) (string, error) {
	if params.Filepath == "" {
		return "", fmt.Errorf("filepath is required")
	}

	// Validate path against folder guard (read operation)
	if err := c.ValidatePath(params.Filepath, false); err != nil {
		return "", err
	}

	// Validate file extension
	ext := strings.ToLower(filepath.Ext(params.Filepath))
	if ext != ".pdf" {
		return "", fmt.Errorf("file must be a PDF (got extension: %s)", ext)
	}

	// Set defaults
	maxPages := params.MaxPages
	if maxPages < 1 {
		maxPages = 50
	}
	if maxPages > 100 {
		maxPages = 100
	}

	pageRange := params.PageRange
	if pageRange == "" {
		pageRange = "all"
	}

	// URL-encode the filepath segments
	pathSegments := strings.Split(params.Filepath, "/")
	encodedSegments := make([]string, len(pathSegments))
	for i, segment := range pathSegments {
		encodedSegments[i] = url.PathEscape(segment)
	}
	encodedPath := strings.Join(encodedSegments, "/")

	// Build API URL to get raw file content
	apiURL := c.BaseURL + "/api/documents/" + encodedPath + "/raw"

	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to call workspace API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return "", fmt.Errorf("PDF file not found: %s. Use 'list_workspace_files' to find the correct path", params.Filepath)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("workspace API returned status %d: %s", resp.StatusCode, string(body))
	}

	// Read the PDF content (limit to 50MB)
	const maxPDFSize = 50 * 1024 * 1024
	limitedReader := io.LimitReader(resp.Body, maxPDFSize)
	pdfData, err := io.ReadAll(limitedReader)
	if err != nil {
		return "", fmt.Errorf("failed to read PDF data: %w", err)
	}

	// Parse and extract text using the PDF library
	content, totalPages, extractedPages, err := extractPDFText(pdfData, pageRange, maxPages, params.Password)
	if err != nil {
		return "", err
	}

	// Truncate if content is too large (100KB for LLM)
	const maxContentSize = 100 * 1024
	truncated := false
	if len(content) > maxContentSize {
		content = content[:maxContentSize]
		truncated = true
	}

	response := map[string]interface{}{
		"filepath":        params.Filepath,
		"total_pages":     totalPages,
		"extracted_pages": extractedPages,
		"page_range":      pageRange,
		"content":         content,
	}

	if truncated {
		response["truncated"] = true
		response["truncated_message"] = "Content was truncated to 100KB for LLM processing"
	}

	responseJSON, err := json.Marshal(response)
	if err != nil {
		return "", fmt.Errorf("failed to marshal response: %w", err)
	}

	return string(responseJSON), nil
}

// extractPDFText extracts text from PDF data
// Note: This is a simplified implementation. For production, consider using a proper PDF library.
func extractPDFText(pdfData []byte, pageRange string, maxPages int, password string) (string, int, int, error) {
	// Try to use the ledongthuc/pdf library
	var pdfReader *pdfReaderWrapper
	var err error
	if password != "" {
		pdfReader, err = newPDFReaderWithPassword(bytes.NewReader(pdfData), int64(len(pdfData)), password)
	} else {
		pdfReader, err = newPDFReader(bytes.NewReader(pdfData), int64(len(pdfData)))
	}
	if err != nil {
		return "", 0, 0, fmt.Errorf("failed to parse PDF: %w", err)
	}

	totalPages := pdfReader.NumPage()
	if totalPages == 0 {
		return "", 0, 0, fmt.Errorf("PDF has no pages")
	}

	pagesToExtract, err := parsePageRange(pageRange, totalPages, maxPages)
	if err != nil {
		return "", 0, 0, fmt.Errorf("invalid page_range: %w", err)
	}

	var textContent strings.Builder
	extractedPages := 0

	for _, pageNum := range pagesToExtract {
		if pageNum < 1 || pageNum > totalPages {
			continue
		}

		pageText := pdfReader.GetPageText(pageNum)
		if pageText != "" {
			textContent.WriteString(fmt.Sprintf("\n--- Page %d ---\n", pageNum))
			textContent.WriteString(pageText)
			extractedPages++
		}
	}

	content := strings.TrimSpace(textContent.String())
	if content == "" {
		content = "(No text content could be extracted from this PDF. It may contain only images or scanned content.)"
	}

	return content, totalPages, extractedPages, nil
}

// parsePageRange parses a page range string and returns a slice of page numbers
func parsePageRange(rangeStr string, totalPages, maxPages int) ([]int, error) {
	rangeStr = strings.TrimSpace(strings.ToLower(rangeStr))

	if rangeStr == "all" || rangeStr == "" {
		pages := make([]int, 0, min(totalPages, maxPages))
		for i := 1; i <= totalPages && len(pages) < maxPages; i++ {
			pages = append(pages, i)
		}
		return pages, nil
	}

	var pages []int
	parts := strings.Split(rangeStr, ",")

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if strings.Contains(part, "-") {
			rangeParts := strings.Split(part, "-")
			if len(rangeParts) != 2 {
				return nil, fmt.Errorf("invalid range format: %s", part)
			}
			start, err := strconv.Atoi(strings.TrimSpace(rangeParts[0]))
			if err != nil {
				return nil, fmt.Errorf("invalid page number: %s", rangeParts[0])
			}
			end, err := strconv.Atoi(strings.TrimSpace(rangeParts[1]))
			if err != nil {
				return nil, fmt.Errorf("invalid page number: %s", rangeParts[1])
			}
			for i := start; i <= end && len(pages) < maxPages; i++ {
				pages = append(pages, i)
			}
		} else {
			pageNum, err := strconv.Atoi(part)
			if err != nil {
				return nil, fmt.Errorf("invalid page number: %s", part)
			}
			if len(pages) < maxPages {
				pages = append(pages, pageNum)
			}
		}
	}

	return pages, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
