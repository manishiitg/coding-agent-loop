package server

import (
	"context"
	"strings"
	"testing"
)

func TestBuildLLMDiscoveryShowsMissingCodingCLI(t *testing.T) {
	t.Setenv("WORKSPACE_DOCS_PATH", t.TempDir())
	t.Setenv("SUPPORTED_LLM_PROVIDERS", "gemini-cli")
	t.Setenv("PATH", t.TempDir())
	t.Setenv("GEMINI_API_KEY", "")

	response := buildLLMDiscovery(context.Background())
	if len(response.Candidates) != 1 {
		t.Fatalf("candidate count = %d, want 1: %+v", len(response.Candidates), response.Candidates)
	}

	candidate := response.Candidates[0]
	if candidate.Provider != "gemini-cli" {
		t.Fatalf("provider = %q, want gemini-cli", candidate.Provider)
	}
	if candidate.RuntimeAvailable == nil || *candidate.RuntimeAvailable {
		t.Fatalf("runtime_available = %v, want false", candidate.RuntimeAvailable)
	}
	if candidate.Usable {
		t.Fatal("usable = true, want false for missing runtime")
	}
	if candidate.DetectionSource != "CLI not found" {
		t.Fatalf("detection_source = %q, want CLI not found", candidate.DetectionSource)
	}
	if candidate.Reason != "CLI runtime was not detected." {
		t.Fatalf("reason = %q, want missing runtime reason", candidate.Reason)
	}
	if !strings.Contains(candidate.SetupHint, "Install Gemini CLI") {
		t.Fatalf("setup_hint = %q, want install hint", candidate.SetupHint)
	}
}

func TestBuildLLMDiscoveryShowsMissingPiCLI(t *testing.T) {
	t.Setenv("WORKSPACE_DOCS_PATH", t.TempDir())
	t.Setenv("SUPPORTED_LLM_PROVIDERS", "pi-cli")
	t.Setenv("PATH", t.TempDir())
	t.Setenv("PI_API_KEY", "")

	response := buildLLMDiscovery(context.Background())
	if len(response.Candidates) != 1 {
		t.Fatalf("candidate count = %d, want 1: %+v", len(response.Candidates), response.Candidates)
	}

	candidate := response.Candidates[0]
	if candidate.Provider != "pi-cli" {
		t.Fatalf("provider = %q, want pi-cli", candidate.Provider)
	}
	if candidate.RuntimeCommand != "pi" {
		t.Fatalf("runtime_command = %q, want pi", candidate.RuntimeCommand)
	}
	if candidate.RuntimeAvailable == nil || *candidate.RuntimeAvailable {
		t.Fatalf("runtime_available = %v, want false", candidate.RuntimeAvailable)
	}
	if candidate.Usable {
		t.Fatal("usable = true, want false for missing runtime")
	}
}

func TestBuildLLMDiscoveryHidesMissingAPIProvider(t *testing.T) {
	t.Setenv("WORKSPACE_DOCS_PATH", t.TempDir())
	t.Setenv("SUPPORTED_LLM_PROVIDERS", "openai")
	t.Setenv("OPENAI_API_KEY", "")

	response := buildLLMDiscovery(context.Background())
	if len(response.Candidates) != 0 {
		t.Fatalf("candidate count = %d, want 0: %+v", len(response.Candidates), response.Candidates)
	}
}

func TestAgyCLIIsPublishedAsAlpha(t *testing.T) {
	t.Setenv("WORKSPACE_DOCS_PATH", t.TempDir())
	t.Setenv("SUPPORTED_LLM_PROVIDERS", "agy-cli")
	t.Setenv("PATH", t.TempDir())

	if !isPublishedLLMProviderAllowed("agy-cli") {
		t.Fatal("agy-cli should be allowed in published provider lists as an alpha local CLI")
	}
	foundSupported := false
	for _, provider := range getSupportedProviders() {
		if provider == "agy-cli" {
			foundSupported = true
		}
	}
	if !foundSupported {
		t.Fatalf("supported providers missing agy-cli: %v", getSupportedProviders())
	}

	response := buildLLMDiscovery(context.Background())
	if len(response.Candidates) != 1 {
		t.Fatalf("candidate count = %d, want 1: %+v", len(response.Candidates), response.Candidates)
	}
	candidate := response.Candidates[0]
	if candidate.Provider != "agy-cli" {
		t.Fatalf("provider = %q, want agy-cli", candidate.Provider)
	}
	if candidate.Label != "Antigravity CLI (Alpha)" {
		t.Fatalf("label = %q, want alpha label", candidate.Label)
	}
	if candidate.RuntimeCommand != "agy" {
		t.Fatalf("runtime_command = %q, want agy", candidate.RuntimeCommand)
	}
	if candidate.RuntimeAvailable == nil || *candidate.RuntimeAvailable {
		t.Fatalf("runtime_available = %v, want false for missing agy binary", candidate.RuntimeAvailable)
	}
	if candidate.Usable {
		t.Fatal("usable = true, want false when agy binary is missing")
	}
	if len(candidate.Options) != 1 || candidate.Options[0] != "agy-cli" {
		t.Fatalf("options = %v, want [agy-cli]", candidate.Options)
	}
}
