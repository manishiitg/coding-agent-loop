package workspace

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// SemanticSearchWorkspaceFilesParams contains parameters for the semantic_search_workspace_files tool
type SemanticSearchWorkspaceFilesParams struct {
	Query        string   `json:"query"`
	Folder       string   `json:"folder"`
	Limit        int      `json:"limit,omitempty"`
	BlockedPaths []string `json:"blocked_paths,omitempty"`
}

// SemanticSearchWorkspaceFiles searches files using semantic similarity
func (c *Client) SemanticSearchWorkspaceFiles(ctx context.Context, params SemanticSearchWorkspaceFilesParams) (string, error) {
	if params.Query == "" {
		return "", fmt.Errorf("query is required")
	}
	if params.Folder == "" {
		return "", fmt.Errorf("folder is required")
	}

	// Validate folder path against folder guard (read operation)
	if err := c.ValidatePath(params.Folder, false); err != nil {
		return "", err
	}

	limit := params.Limit
	if limit == 0 {
		limit = 10
	}
	if limit > 50 {
		limit = 50
	}

	// Build API URL with proper URL encoding
	baseURL := c.BaseURL + "/api/search/semantic"
	u, err := url.Parse(baseURL)
	if err != nil {
		return "", fmt.Errorf("failed to parse base URL: %w", err)
	}

	q := u.Query()
	q.Set("query", params.Query)
	q.Set("folder", params.Folder)
	q.Set("limit", fmt.Sprintf("%d", limit))
	if len(params.BlockedPaths) > 0 {
		q.Set("blocked_paths", strings.Join(params.BlockedPaths, ","))
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	body, err := readResponseBody(resp)
	if err != nil {
		return "", err
	}

	var apiResp APIResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return "", fmt.Errorf("failed to parse API response: %w", err)
	}

	if !apiResp.Success {
		return "", fmt.Errorf("semantic search API error: %s", apiResp.Error)
	}

	return formatSemanticSearchResults(apiResp.Data, params.Query)
}

// formatSemanticSearchResults formats semantic search results for the LLM
func formatSemanticSearchResults(data interface{}, query string) (string, error) {
	if data == nil {
		return fmt.Sprintf("**Semantic Search: `%s`** - Found 0 results\n\nNo files found matching the query.\n", query), nil
	}

	var results []map[string]interface{}

	switch v := data.(type) {
	case map[string]interface{}:
		for _, key := range []string{"results", "files", "documents", "matches"} {
			if r, exists := v[key]; exists {
				if arr, ok := r.([]interface{}); ok {
					for _, item := range arr {
						if m, ok := item.(map[string]interface{}); ok {
							results = append(results, m)
						}
					}
					break
				}
			}
		}
	case []interface{}:
		for _, item := range v {
			if m, ok := item.(map[string]interface{}); ok {
				results = append(results, m)
			}
		}
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("**Semantic Search: `%s`** - Found %d results\n\n", query, len(results)))

	if len(results) == 0 {
		sb.WriteString("No files found matching the query.\n")
		return sb.String(), nil
	}

	for i, result := range results {
		filepath := getStringFromMap(result, "filepath")
		if filepath == "" {
			filepath = getStringFromMap(result, "path")
		}
		score := getFloatFromMap(result, "score")
		sb.WriteString(fmt.Sprintf("%d. **%s** (score: %.3f)\n", i+1, filepath, score))

		if snippet := getStringFromMap(result, "snippet"); snippet != "" {
			sb.WriteString(fmt.Sprintf("   ```\n   %s\n   ```\n", snippet))
		}
	}

	return sb.String(), nil
}
