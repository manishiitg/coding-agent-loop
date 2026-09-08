package services

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// External MCP server discovery, beyond our own curated catalog
// (agent_go/configs/mcp_servers_clean.json). The catalog is small and every
// entry was hand-verified (see PR #191); GitHub's public MCP Registry and
// Smithery's directory are much larger and unvetted — good for "does an MCP
// exist for X" discovery, not something to auto-add without the same
// scrutiny the curated catalog got. Read-only search only; nothing here
// writes to any config.

// MCPRegistryResult is one hit from an external registry, normalized enough
// for the search_mcp_catalog tool to render consistently across sources.
type MCPRegistryResult struct {
	Source      string `json:"source"` // "github" or "smithery"
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	// Remote reports whether this can be added the way our own catalog works
	// (a URL, optionally with OAuth) rather than needing a local package run
	// (npm/npx, docker, etc.).
	Remote bool   `json:"remote"`
	Link   string `json:"link,omitempty"`
}

var mcpRegistryHTTPClient = &http.Client{Timeout: 8 * time.Second}

// Base URLs as vars, not constants, so tests can point them at a local
// httptest server instead of hitting the real registries.
var (
	githubMCPRegistryBaseURL = "https://api.mcp.github.com"
	smitheryRegistryBaseURL  = "https://api.smithery.ai"
)

// SearchGitHubMCPRegistry queries the public GitHub MCP Registry
// (api.mcp.github.com), the API behind github.com/mcp. No API key required.
func SearchGitHubMCPRegistry(ctx context.Context, query string, limit int) ([]MCPRegistryResult, error) {
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	endpoint := fmt.Sprintf("%s/v0.1/servers?search=%s&limit=%d", githubMCPRegistryBaseURL, url.QueryEscape(query), limit)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := mcpRegistryHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("github mcp registry: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github mcp registry: unexpected status %d", resp.StatusCode)
	}

	var raw struct {
		Servers []struct {
			Server struct {
				Name        string `json:"name"`
				Description string `json:"description"`
				Repository  struct {
					URL string `json:"url"`
				} `json:"repository"`
				Remotes []struct {
					URL string `json:"url"`
				} `json:"remotes"`
				Packages []struct {
					Identifier string `json:"identifier"`
				} `json:"packages"`
			} `json:"server"`
		} `json:"servers"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("github mcp registry: parse response: %w", err)
	}

	out := make([]MCPRegistryResult, 0, len(raw.Servers))
	for _, entry := range raw.Servers {
		s := entry.Server
		result := MCPRegistryResult{
			Source:      "github",
			Name:        s.Name,
			Description: strings.TrimSpace(s.Description),
			Remote:      len(s.Remotes) > 0,
			Link:        s.Repository.URL,
		}
		if result.Remote {
			result.Link = s.Remotes[0].URL
		}
		out = append(out, result)
	}
	return out, nil
}

// smitheryAPIKeyEnv is the environment variable holding the Smithery API
// bearer token. Never hardcode the key itself here — it lives in .env
// (gitignored) locally and in the server's own .env in each deployment.
const smitheryAPIKeyEnv = "SMITHERY_API_KEY"

// SmitheryConfigured reports whether a Smithery search can run at all, so
// callers can skip it cleanly instead of surfacing a confusing auth error.
func SmitheryConfigured() bool {
	return strings.TrimSpace(os.Getenv(smitheryAPIKeyEnv)) != ""
}

// SearchSmitheryRegistry queries Smithery's server directory
// (api.smithery.ai), which requires a bearer API key.
func SearchSmitheryRegistry(ctx context.Context, query string, pageSize int) ([]MCPRegistryResult, error) {
	apiKey := strings.TrimSpace(os.Getenv(smitheryAPIKeyEnv))
	if apiKey == "" {
		return nil, fmt.Errorf("%s is not configured on this server", smitheryAPIKeyEnv)
	}
	if pageSize <= 0 || pageSize > 50 {
		pageSize = 10
	}
	endpoint := fmt.Sprintf("%s/servers?q=%s&pageSize=%d", smitheryRegistryBaseURL, url.QueryEscape(query), pageSize)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := mcpRegistryHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("smithery: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("smithery: unexpected status %d", resp.StatusCode)
	}

	var raw struct {
		Servers []struct {
			QualifiedName string `json:"qualifiedName"`
			DisplayName   string `json:"displayName"`
			Description   string `json:"description"`
			Remote        bool   `json:"remote"`
			Homepage      string `json:"homepage"`
			Verified      bool   `json:"verified"`
		} `json:"servers"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("smithery: parse response: %w", err)
	}

	out := make([]MCPRegistryResult, 0, len(raw.Servers))
	for _, s := range raw.Servers {
		name := strings.TrimSpace(s.DisplayName)
		if name == "" {
			name = s.QualifiedName
		}
		out = append(out, MCPRegistryResult{
			Source:      "smithery",
			Name:        name,
			Description: strings.TrimSpace(s.Description),
			Remote:      s.Remote,
			Link:        s.Homepage,
		})
	}
	return out, nil
}
