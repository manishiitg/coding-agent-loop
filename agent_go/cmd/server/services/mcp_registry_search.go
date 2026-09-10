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
	// Identifier is the machine-usable ID needed to look this hit up again,
	// e.g. via InspectSmitheryServer. Empty when the source has no such ID
	// (GitHub registry hits are identified by Link instead).
	Identifier string `json:"identifier,omitempty"`
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
// bearer token, overriding smitheryDefaultAPIKey below.
const smitheryAPIKeyEnv = "SMITHERY_API_KEY"

// smitheryDefaultAPIKey is a free-tier Smithery key checked in so
// search_mcp_catalog works out of the box for anyone who clones this repo,
// without requiring per-deployment setup. Smithery's free tier has no
// sensitive scope (search only, rate-limited); set SMITHERY_API_KEY in the
// environment to override it with a different key.
const smitheryDefaultAPIKey = "989c2d5a-8e01-4a72-9033-081cb08a8a4d"

func smitheryAPIKey() string {
	if key := strings.TrimSpace(os.Getenv(smitheryAPIKeyEnv)); key != "" {
		return key
	}
	return smitheryDefaultAPIKey
}

// SmitheryConfigured reports whether a Smithery search can run at all. Always
// true now that smitheryDefaultAPIKey provides a fallback; kept as a function
// so callers don't need to change if that ever stops being the case.
func SmitheryConfigured() bool {
	return smitheryAPIKey() != ""
}

// SearchSmitheryRegistry queries Smithery's server directory
// (api.smithery.ai), which requires a bearer API key.
func SearchSmitheryRegistry(ctx context.Context, query string, pageSize int) ([]MCPRegistryResult, error) {
	apiKey := smitheryAPIKey()
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
			Identifier:  s.QualifiedName,
		})
	}
	return out, nil
}

// MCPToolInfo is one tool a server exposes, as reported by Smithery's server
// detail endpoint.
type MCPToolInfo struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema,omitempty"`
}

// MCPServerDetail is the full inspection result for one Smithery server:
// its connection shape plus every tool it exposes, so an agent can review
// what a server actually does before installing it.
type MCPServerDetail struct {
	QualifiedName string        `json:"qualified_name"`
	DisplayName   string        `json:"display_name,omitempty"`
	Description   string        `json:"description,omitempty"`
	Remote        bool          `json:"remote"`
	DeploymentURL string        `json:"deployment_url,omitempty"`
	Tools         []MCPToolInfo `json:"tools"`
}

// InspectSmitheryServer fetches full detail for one Smithery server —
// notably its complete tool list with descriptions and input schemas — via
// Smithery's per-server detail endpoint (api.smithery.ai/servers/{qualifiedName}).
// This is the one piece search_mcp_catalog can't show: what a candidate
// server actually does, before deciding whether to install it.
//
// GitHub's MCP Registry has no equivalent endpoint — those servers only
// advertise their tools via the live MCP protocol handshake once running,
// so there is nothing to inspect ahead of time for a GitHub hit.
func InspectSmitheryServer(ctx context.Context, qualifiedName string) (*MCPServerDetail, error) {
	qualifiedName = strings.TrimSpace(qualifiedName)
	if qualifiedName == "" {
		return nil, fmt.Errorf("qualifiedName is required")
	}

	segments := strings.Split(qualifiedName, "/")
	for i, seg := range segments {
		segments[i] = url.PathEscape(seg)
	}
	endpoint := fmt.Sprintf("%s/servers/%s", smitheryRegistryBaseURL, strings.Join(segments, "/"))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+smitheryAPIKey())
	req.Header.Set("Accept", "application/json")

	resp, err := mcpRegistryHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("smithery: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("smithery: no server found for qualified name %q", qualifiedName)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("smithery: unexpected status %d", resp.StatusCode)
	}

	var raw struct {
		QualifiedName string `json:"qualifiedName"`
		DisplayName   string `json:"displayName"`
		Description   string `json:"description"`
		Remote        bool   `json:"remote"`
		DeploymentURL string `json:"deploymentUrl"`
		Tools         []struct {
			Name        string          `json:"name"`
			Description string          `json:"description"`
			InputSchema json.RawMessage `json:"inputSchema"`
		} `json:"tools"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("smithery: parse response: %w", err)
	}

	detail := &MCPServerDetail{
		QualifiedName: raw.QualifiedName,
		DisplayName:   strings.TrimSpace(raw.DisplayName),
		Description:   strings.TrimSpace(raw.Description),
		Remote:        raw.Remote,
		DeploymentURL: raw.DeploymentURL,
		Tools:         make([]MCPToolInfo, 0, len(raw.Tools)),
	}
	for _, t := range raw.Tools {
		detail.Tools = append(detail.Tools, MCPToolInfo{
			Name:        t.Name,
			Description: strings.TrimSpace(t.Description),
			InputSchema: t.InputSchema,
		})
	}
	return detail, nil
}
