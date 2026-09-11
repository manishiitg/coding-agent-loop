package services

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// External registries supply discovery metadata, not hosting or authorization.
// Prefer endpoints verified against the provider's documentation before install.

// MCPRegistryResult is one hit from an external registry, normalized enough
// for the search_mcp_catalog tool to render consistently across sources.
type MCPRegistryResult struct {
	Source      string `json:"source"` // "github" or "official"
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	// Remote reports whether this can be added the way our own catalog works
	// (a URL, optionally with OAuth) rather than needing a local package run
	// (npm/npx, docker, etc.).
	Remote        bool   `json:"remote"`
	Link          string `json:"link,omitempty"`
	RepositoryURL string `json:"repository_url,omitempty"`
}

var mcpRegistryHTTPClient = &http.Client{Timeout: 8 * time.Second}

// Base URLs as vars, not constants, so tests can point them at a local
// httptest server instead of hitting the real registries.
var (
	githubMCPRegistryBaseURL   = "https://api.mcp.github.com"
	officialMCPRegistryBaseURL = "https://registry.modelcontextprotocol.io"
)

// SearchGitHubMCPRegistry queries the public GitHub MCP Registry
// (api.mcp.github.com), the API behind github.com/mcp. No API key required.
func SearchGitHubMCPRegistry(ctx context.Context, query string, limit int) ([]MCPRegistryResult, error) {
	return searchPublicMCPRegistry(ctx, githubMCPRegistryBaseURL, "github", query, limit)
}

// SearchOfficialMCPRegistry uses the MCP project's open, unauthenticated registry.
func SearchOfficialMCPRegistry(ctx context.Context, query string, limit int) ([]MCPRegistryResult, error) {
	return searchPublicMCPRegistry(ctx, officialMCPRegistryBaseURL, "official", query, limit)
}

func searchPublicMCPRegistry(ctx context.Context, baseURL, source, query string, limit int) ([]MCPRegistryResult, error) {
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	endpoint := fmt.Sprintf("%s/v0.1/servers?search=%s&limit=%d", baseURL, url.QueryEscape(query), limit)
	if source == "official" {
		endpoint += "&version=latest"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := mcpRegistryHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s MCP registry: %w", source, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s MCP registry: unexpected status %d", source, resp.StatusCode)
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
		return nil, fmt.Errorf("%s MCP registry: parse response: %w", source, err)
	}

	out := make([]MCPRegistryResult, 0, len(raw.Servers))
	for _, entry := range raw.Servers {
		s := entry.Server
		result := MCPRegistryResult{
			Source:        source,
			RepositoryURL: s.Repository.URL,
			Name:          s.Name,
			Description:   strings.TrimSpace(s.Description),
			Remote:        len(s.Remotes) > 0,
			Link:          s.Repository.URL,
		}
		if result.Remote {
			result.Link = ""
			for _, remote := range s.Remotes {
				if !isSmitheryURL(remote.URL) {
					result.Link = remote.URL
					break
				}
			}
			if result.Link == "" {
				continue
			}
		}
		if isSmitheryURL(result.Link) {
			continue
		}
		out = append(out, result)
	}
	return out, nil
}

// Other registries can list hosted intermediaries too. Excluding the Smithery
// source must not reintroduce its deployment endpoints through another registry.
func isSmitheryURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	return host == "smithery.ai" || strings.HasSuffix(host, ".smithery.ai") || host == "smithery.run" || strings.HasSuffix(host, ".smithery.run")
}
