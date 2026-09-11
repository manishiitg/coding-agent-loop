package services

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOfficialRegistryUsesPublicMetadataAndDirectEndpoints(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Error("public discovery must not send credentials")
		}
		if r.URL.Path != "/v0.1/servers" || r.URL.Query().Get("version") != "latest" || r.URL.Query().Get("search") != "jam & logs" {
			t.Errorf("unexpected registry query: %s", r.URL)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"servers":[
		{"server":{"name":"dev.jam/mcp","remotes":[{"url":"https://mcp.jam.dev/mcp"}],"repository":{"url":"https://github.com/example/jam"}}},
		{"server":{"name":"proxy/jam","remotes":[{"url":"https://server.smithery.ai/jam/mcp"}]}},
		{"server":{"name":"multiple/jam","remotes":[{"url":"https://jam.smithery.run/mcp"},{"url":"https://mcp.jam.dev/mcp"}]}}
		]}`))
	}))
	defer server.Close()
	original := officialMCPRegistryBaseURL
	officialMCPRegistryBaseURL = server.URL
	defer func() { officialMCPRegistryBaseURL = original }()
	results, err := SearchOfficialMCPRegistry(context.Background(), "jam & logs", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("expected direct results only: %+v", results)
	}
	for _, r := range results {
		if r.Source != "official" || r.Link != "https://mcp.jam.dev/mcp" || !r.Remote {
			t.Errorf("wrong metadata: %+v", r)
		}
	}
	if results[0].RepositoryURL != "https://github.com/example/jam" {
		t.Fatal("lost verification source")
	}
}

func TestSmitheryEndpointFilterUsesHostname(t *testing.T) {
	for _, raw := range []string{"https://smithery.ai", "https://server.smithery.ai/mcp", "https://x.smithery.run/mcp", "https://SERVER.SMITHERY.AI./mcp"} {
		if !isSmitheryURL(raw) {
			t.Errorf("missed %s", raw)
		}
	}
	for _, raw := range []string{"https://mcp.jam.dev/mcp", "https://example.com/smithery.ai", "https://notsmithery.ai/mcp"} {
		if isSmitheryURL(raw) {
			t.Errorf("incorrectly removed %s", raw)
		}
	}
}

func TestSearchGitHubMCPRegistryParsesRemoteAndPackageEntries(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("search"); got != "clickup" {
			t.Errorf("search query = %q, want clickup", got)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"servers":[
			{"server":{"name":"acme/clickup-remote","description":"Remote ClickUp server","remotes":[{"url":"https://mcp.example.com/clickup"}],"repository":{"url":"https://github.com/acme/clickup-remote"}}},
			{"server":{"name":"acme/clickup-stdio","description":"Local package server","packages":[{"identifier":"@acme/clickup-mcp"}],"repository":{"url":"https://github.com/acme/clickup-stdio"}}}
		]}`))
	}))
	defer server.Close()

	original := githubMCPRegistryBaseURL
	githubMCPRegistryBaseURL = server.URL
	defer func() { githubMCPRegistryBaseURL = original }()

	results, err := SearchGitHubMCPRegistry(context.Background(), "clickup", 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}
	if results[0].Source != "github" || !results[0].Remote || results[0].Link != "https://mcp.example.com/clickup" {
		t.Errorf("remote entry parsed wrong: %+v", results[0])
	}
	if results[1].Remote {
		t.Errorf("package-only entry should not be marked remote: %+v", results[1])
	}
	if results[1].Link != "https://github.com/acme/clickup-stdio" {
		t.Errorf("package-only entry should fall back to repository URL, got %q", results[1].Link)
	}
}

func TestSearchGitHubMCPRegistryPropagatesHTTPErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	original := githubMCPRegistryBaseURL
	githubMCPRegistryBaseURL = server.URL
	defer func() { githubMCPRegistryBaseURL = original }()

	if _, err := SearchGitHubMCPRegistry(context.Background(), "clickup", 5); err == nil {
		t.Fatal("expected an error on non-200 status, got nil")
	}
}
