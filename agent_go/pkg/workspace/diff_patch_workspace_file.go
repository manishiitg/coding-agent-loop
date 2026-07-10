package workspace

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// DiffPatchWorkspaceFileParams contains parameters for the diff_patch_workspace_file tool
type DiffPatchWorkspaceFileParams struct {
	Filepath string `json:"filepath"`
	Diff     string `json:"diff"`
}

// DiffPatchWorkspaceFile applies a unified diff patch to a file
func (c *Client) DiffPatchWorkspaceFile(ctx context.Context, params DiffPatchWorkspaceFileParams) (DiffPatchResult, error) {
	if params.Filepath == "" {
		return DiffPatchResult{}, fmt.Errorf("filepath is required")
	}
	if params.Diff == "" {
		return DiffPatchResult{}, fmt.Errorf("diff is required")
	}

	// Normalize absolute paths to workspace-relative before building the URL.
	// LLMs often send absolute paths (e.g. "/app/workspace-docs/Workflow/..."
	// or native desktop paths ending in "/workspace-docs/Workflow/...").
	// The workspace HTTP handler does its own validation with the real docs-dir,
	// but the filepath goes into the URL path so it must be relative.
	params.Filepath = stripWorkspacePrefix(params.Filepath)

	// Build API URL for diff patching
	apiURL := c.BaseURL + "/api/documents/" + encodeWorkspaceDocumentPath(params.Filepath) + "/diff"

	// Prepare request body
	requestBody := map[string]interface{}{
		"diff": params.Diff,
	}

	jsonBody, err := json.Marshal(requestBody)
	if err != nil {
		return DiffPatchResult{}, fmt.Errorf("failed to marshal request body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "PATCH", apiURL, strings.NewReader(string(jsonBody)))
	if err != nil {
		return DiffPatchResult{}, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return DiffPatchResult{}, fmt.Errorf("failed to call workspace API: %w", err)
	}
	defer resp.Body.Close()

	body, err := readResponseBody(resp)
	if err != nil {
		return DiffPatchResult{}, err
	}

	var apiResp APIResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return DiffPatchResult{}, fmt.Errorf("failed to parse API response: %w", err)
	}

	if !apiResp.Success {
		return DiffPatchResult{}, fmt.Errorf("workspace API error: %s", apiResp.Error)
	}

	return DiffPatchResult{
		Data: apiResp.Data,
	}, nil
}

func encodeWorkspaceDocumentPath(path string) string {
	pathSegments := strings.Split(path, "/")
	encodedSegments := make([]string, len(pathSegments))
	for i, segment := range pathSegments {
		encodedSegments[i] = url.PathEscape(segment)
	}
	return strings.Join(encodedSegments, "/")
}

// stripWorkspacePrefix converts absolute workspace paths to relative.
// Checks WORKSPACE_DOCS_PATH env first (desktop/Mac), discovered local
// workspace-docs roots next, then Docker defaults.
func stripWorkspacePrefix(p string) string {
	if !filepath.IsAbs(p) {
		return p
	}
	cleanPath := filepath.Clean(p)
	for _, root := range workspaceDocsRootsForPatch() {
		root = filepath.Clean(root)
		if cleanPath == root {
			return "."
		}
		prefix := root + string(filepath.Separator)
		if strings.HasPrefix(cleanPath, prefix) {
			return strings.TrimPrefix(cleanPath, prefix)
		}
	}
	return p
}

func workspaceDocsRootsForPatch() []string {
	var roots []string
	if envRoot := os.Getenv("WORKSPACE_DOCS_PATH"); envRoot != "" {
		roots = append(roots, envRoot)
	}
	if cwd, err := os.Getwd(); err == nil {
		for dir := filepath.Clean(cwd); ; dir = filepath.Dir(dir) {
			roots = append(roots, filepath.Join(dir, "workspace-docs"))
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
		}
	}
	roots = append(roots, "/app/workspace-docs", "/workspace-docs")
	return deduplicatePathStrings(roots)
}

func deduplicatePathStrings(paths []string) []string {
	seen := make(map[string]struct{}, len(paths))
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		p = filepath.Clean(strings.TrimSpace(p))
		if p == "." || p == "" {
			continue
		}
		if _, exists := seen[p]; exists {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	return out
}
