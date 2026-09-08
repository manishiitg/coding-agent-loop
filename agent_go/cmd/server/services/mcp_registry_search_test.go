package services

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

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

func TestSmitheryConfiguredReflectsEnvVar(t *testing.T) {
	original := os.Getenv(smitheryAPIKeyEnv)
	defer os.Setenv(smitheryAPIKeyEnv, original)

	os.Unsetenv(smitheryAPIKeyEnv)
	if SmitheryConfigured() {
		t.Error("expected SmitheryConfigured() to be false when unset")
	}
	os.Setenv(smitheryAPIKeyEnv, "test-key")
	if !SmitheryConfigured() {
		t.Error("expected SmitheryConfigured() to be true when set")
	}
}

func TestSearchSmitheryRegistryRequiresAPIKey(t *testing.T) {
	original := os.Getenv(smitheryAPIKeyEnv)
	os.Unsetenv(smitheryAPIKeyEnv)
	defer os.Setenv(smitheryAPIKeyEnv, original)

	if _, err := SearchSmitheryRegistry(context.Background(), "clickup", 5); err == nil {
		t.Fatal("expected an error when SMITHERY_API_KEY is unset, got nil")
	}
}

func TestSearchSmitheryRegistrySendsBearerTokenAndParsesResults(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("Authorization header = %q, want Bearer test-key", got)
		}
		if got := r.URL.Query().Get("q"); got != "clickup" {
			t.Errorf("q query = %q, want clickup", got)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"servers":[{"qualifiedName":"clickup","displayName":"ClickUp","description":"Project management","remote":true,"homepage":"https://clickup.com"}]}`))
	}))
	defer server.Close()

	originalURL := smitheryRegistryBaseURL
	smitheryRegistryBaseURL = server.URL
	defer func() { smitheryRegistryBaseURL = originalURL }()

	originalKey := os.Getenv(smitheryAPIKeyEnv)
	os.Setenv(smitheryAPIKeyEnv, "test-key")
	defer os.Setenv(smitheryAPIKeyEnv, originalKey)

	results, err := SearchSmitheryRegistry(context.Background(), "clickup", 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 || results[0].Name != "ClickUp" || results[0].Source != "smithery" || !results[0].Remote {
		t.Fatalf("unexpected results: %+v", results)
	}
}
