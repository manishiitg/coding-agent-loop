package server

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	llm "github.com/manishiitg/multi-llm-provider-go"
)

func TestProviderAuthConfiguredTreatsPiProviderKeysAsPiAuth(t *testing.T) {
	configured, source := providerAuthConfigured("pi-cli", &llm.ProviderAPIKeys{
		PiProviderKeys: map[string]string{"zai": "zai-key"},
	})
	if !configured {
		t.Fatalf("pi-cli auth configured = false, want true")
	}
	if source != "Provider-specific Pi API key or workspace provider auth" {
		t.Fatalf("pi-cli auth source = %q", source)
	}
}

func TestBuildLLMDiscoveryShowsCursorLoginRequired(t *testing.T) {
	t.Setenv("WORKSPACE_DOCS_PATH", t.TempDir())
	t.Setenv("SUPPORTED_LLM_PROVIDERS", "cursor-cli")
	withFakeExecutable(t, "cursor-agent")
	withCursorStatusJSON(t, `{"status":"unauthenticated","isAuthenticated":false,"message":"Not logged in"}`, nil)

	configured, source := providerAuthConfigured("cursor-cli", &llm.ProviderAPIKeys{})
	if configured {
		t.Fatal("cursor-cli auth configured = true, want false for logged-out CLI")
	}
	if source != "Cursor CLI login or CURSOR_API_KEY/workspace provider auth" {
		t.Fatalf("cursor-cli auth source = %q", source)
	}

	response := buildLLMDiscovery(context.Background())
	if len(response.Candidates) != 1 {
		t.Fatalf("candidate count = %d, want 1: %+v", len(response.Candidates), response.Candidates)
	}
	candidate := response.Candidates[0]
	if candidate.Provider != "cursor-cli" {
		t.Fatalf("provider = %q, want cursor-cli", candidate.Provider)
	}
	if candidate.RuntimeAvailable == nil || !*candidate.RuntimeAvailable {
		t.Fatalf("runtime_available = %v, want true", candidate.RuntimeAvailable)
	}
	if candidate.AuthConfigured {
		t.Fatal("auth_configured = true, want false")
	}
	if candidate.Usable {
		t.Fatal("usable = true, want false for logged-out Cursor CLI")
	}
	if !strings.Contains(candidate.SetupHint, "cursor-agent login") {
		t.Fatalf("setup_hint = %q, want cursor-agent login hint", candidate.SetupHint)
	}
	if !containsLLMCapabilityString(candidate.Options, "composer-2.5") {
		t.Fatalf("options = %v, want composer-2.5 available as explicit Cursor model", candidate.Options)
	}
}

func TestProviderAuthConfiguredAcceptsCursorWorkspaceKey(t *testing.T) {
	key := "cursor-key"
	withCursorStatusJSON(t, `{"status":"unauthenticated","isAuthenticated":false}`, nil)

	configured, source := providerAuthConfigured("cursor-cli", &llm.ProviderAPIKeys{
		CursorCLI: &key,
	})
	if !configured {
		t.Fatal("cursor-cli auth configured = false, want true for workspace key")
	}
	if source != "CURSOR_API_KEY or workspace provider auth" {
		t.Fatalf("cursor-cli auth source = %q", source)
	}
}

func TestProviderAuthConfiguredAcceptsCursorAuthenticatedJSON(t *testing.T) {
	t.Setenv("WORKSPACE_DOCS_PATH", t.TempDir())
	withFakeExecutable(t, "cursor-agent")
	withCursorStatusJSON(t, `{"status":"authenticated","isAuthenticated":true}`, nil)

	configured, source := providerAuthConfigured("cursor-cli", &llm.ProviderAPIKeys{})
	if !configured {
		t.Fatal("cursor-cli auth configured = false, want true for authenticated local Cursor CLI")
	}
	if source != "Cursor CLI login or CURSOR_API_KEY/workspace provider auth" {
		t.Fatalf("cursor-cli auth source = %q", source)
	}
}

func TestProviderAuthConfiguredAcceptsCursorPlainLoggedInStatus(t *testing.T) {
	t.Setenv("WORKSPACE_DOCS_PATH", t.TempDir())
	withFakeExecutable(t, "cursor-agent")
	withCursorStatusJSON(t, `Logged in as manish@example.com`, nil)

	configured, _ := providerAuthConfigured("cursor-cli", &llm.ProviderAPIKeys{})
	if !configured {
		t.Fatal("cursor-cli auth configured = false, want true for plain logged-in Cursor CLI status")
	}
}

func TestCursorCLIAuthProbeKeepsLastConfirmedLoginOnTransientFailure(t *testing.T) {
	t.Setenv("WORKSPACE_DOCS_PATH", t.TempDir())
	withFakeExecutable(t, "cursor-agent")

	previous := cursorCLIStatusJSON
	resetCursorCLIAuthProbeCache()
	probeCalls := 0
	cursorCLIStatusJSON = func(context.Context) ([]byte, error) {
		probeCalls++
		if probeCalls == 1 {
			return []byte(`{"status":"authenticated","isAuthenticated":true}`), nil
		}
		return nil, context.DeadlineExceeded
	}
	t.Cleanup(func() {
		cursorCLIStatusJSON = previous
		resetCursorCLIAuthProbeCache()
	})

	authenticated, conclusive := cursorCLILocalAuthState()
	if !authenticated || !conclusive {
		t.Fatalf("first probe = authenticated:%v conclusive:%v, want true/true", authenticated, conclusive)
	}

	cursorCLIAuthProbeCache.Lock()
	cursorCLIAuthProbeCache.checkedAt = time.Now().Add(-31 * time.Second)
	cursorCLIAuthProbeCache.Unlock()
	authenticated, conclusive = cursorCLILocalAuthState()
	if !authenticated || !conclusive {
		t.Fatalf("transient retry = authenticated:%v conclusive:%v, want preserved true/true", authenticated, conclusive)
	}
	if probeCalls != 2 {
		t.Fatalf("status probe calls = %d, want 2", probeCalls)
	}
}

func TestValidateCursorCLIReportsLoginRequiredBeforeTmuxRun(t *testing.T) {
	t.Setenv("WORKSPACE_DOCS_PATH", t.TempDir())
	t.Setenv("CURSOR_API_KEY", "")
	withFakeExecutable(t, "cursor-agent")
	withFakeExecutable(t, "tmux")
	withCursorStatusJSON(t, `{"status":"unauthenticated","isAuthenticated":false,"message":"Not logged in"}`, nil)

	resp := validateProviderConfig(llm.APIKeyValidationRequest{
		Provider: "cursor-cli",
		ModelID:  "composer-2.5",
	})
	if resp.Valid {
		t.Fatal("valid = true, want false for logged-out Cursor CLI")
	}
	if !strings.Contains(resp.Message, "cursor-agent login") {
		t.Fatalf("message = %q, want cursor-agent login hint", resp.Message)
	}
}

func withFakeExecutable(t *testing.T, name string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write fake %s: %v", name, err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func withCursorStatusJSON(t *testing.T, output string, err error) {
	t.Helper()
	previous := cursorCLIStatusJSON
	resetCursorCLIAuthProbeCache()
	cursorCLIStatusJSON = func(context.Context) ([]byte, error) {
		return []byte(output), err
	}
	t.Cleanup(func() {
		cursorCLIStatusJSON = previous
		resetCursorCLIAuthProbeCache()
	})
}

func resetCursorCLIAuthProbeCache() {
	cursorCLIAuthProbeCache.Lock()
	defer cursorCLIAuthProbeCache.Unlock()
	cursorCLIAuthProbeCache.checkedAt = time.Time{}
	cursorCLIAuthProbeCache.authenticated = false
	cursorCLIAuthProbeCache.conclusive = false
}

func containsLLMCapabilityString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
