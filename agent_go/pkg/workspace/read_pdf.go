package workspace

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
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

// pythonPDFResult is the JSON structure returned by extract_pdf_text.py
type pythonPDFResult struct {
	TotalPages     int    `json:"total_pages"`
	ExtractedPages int    `json:"extracted_pages"`
	Content        string `json:"content"`
	Error          string `json:"error"`
}

// ReadPDF reads and extracts text content from a PDF file
func (c *Client) ReadPDF(ctx context.Context, params ReadPDFParams) (string, error) {
	if params.Filepath == "" {
		return "", fmt.Errorf("filepath is required")
	}
	absolutePath, guardPath, err := normalizeWorkspaceAbsoluteToolPath(params.Filepath, "filepath", "read_pdf")
	if err != nil {
		return "", err
	}

	// Validate path against folder guard (read operation)
	if err := c.ValidatePathWithContext(ctx, guardPath, false); err != nil {
		return "", err
	}

	// Validate file extension
	ext := strings.ToLower(filepath.Ext(absolutePath))
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

	// URL-encode the workspace API path. The external contract is absolute-only,
	// but the workspace API route expects a workspace-docs-relative path.
	pathSegments := strings.Split(filepath.ToSlash(guardPath), "/")
	encodedSegments := make([]string, len(pathSegments))
	for i, segment := range pathSegments {
		encodedSegments[i] = url.PathEscape(segment)
	}
	encodedPath := strings.Join(encodedSegments, "/")

	pdfData, err := c.request(ctx, "GET", "/api/documents/"+encodedPath+"/raw", nil)
	if err != nil {
		return "", fmt.Errorf("failed to read PDF file: %w. Use execute_shell_command to verify the path, for example with 'ls' or 'find'.", err)
	}

	// Limit PDF size to 50MB.
	const maxPDFSize = 50 * 1024 * 1024
	if len(pdfData) > maxPDFSize {
		return "", fmt.Errorf("PDF file too large (%d bytes, max %d bytes)", len(pdfData), maxPDFSize)
	}

	// Extract text using Python/pypdf subprocess
	result, err := extractPDFTextPython(ctx, pdfData, pageRange, maxPages, params.Password)
	if err != nil {
		return "", err
	}

	if result.Error != "" {
		return "", fmt.Errorf("PDF extraction failed: %s", result.Error)
	}

	content := result.Content

	// Truncate if content is too large (100KB for LLM)
	const maxContentSize = 100 * 1024
	truncated := false
	if len(content) > maxContentSize {
		content = content[:maxContentSize]
		truncated = true
	}

	response := map[string]interface{}{
		"filepath":        absolutePath,
		"total_pages":     result.TotalPages,
		"extracted_pages": result.ExtractedPages,
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

// findPDFScript locates extract_pdf_text.py by checking:
// 1. /app/scripts/ (Docker container path)
// 2. Next to the running binary under scripts/
// 3. scripts/ relative to working directory (local dev — cwd is agent_go/)
// 4. agent_go/scripts/ relative to working directory (if cwd is repo root)
func findPDFScript() (string, error) {
	const scriptName = "extract_pdf_text.py"
	candidates := []string{filepath.Join("/app", "scripts", scriptName)}

	if exe, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Join(filepath.Dir(exe), "scripts", scriptName))
	}

	if wd, err := os.Getwd(); err == nil {
		candidates = append(candidates,
			filepath.Join(wd, "scripts", scriptName),
			filepath.Join(wd, "agent_go", "scripts", scriptName),
		)
	}

	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("extract_pdf_text.py not found (searched: %v)", candidates)
}

// extractPDFTextPython calls the Python extract_pdf_text.py script via subprocess,
// piping PDF bytes through stdin and reading JSON from stdout.
func extractPDFTextPython(ctx context.Context, pdfData []byte, pageRange string, maxPages int, password string) (*pythonPDFResult, error) {
	scriptPath, err := findPDFScript()
	if err != nil {
		return nil, err
	}

	args := []string{scriptPath,
		"--page-range", pageRange,
		"--max-pages", strconv.Itoa(maxPages),
	}
	if password != "" {
		args = append(args, "--password", password)
	}

	cmd := exec.CommandContext(ctx, "python3", args...)
	cmd.Stdin = bytes.NewReader(pdfData)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("python PDF extraction failed: %w (stderr: %s)", err, stderr.String())
	}

	var result pythonPDFResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		return nil, fmt.Errorf("failed to parse Python PDF output: %w (stdout: %s)", err, stdout.String())
	}

	return &result, nil
}
