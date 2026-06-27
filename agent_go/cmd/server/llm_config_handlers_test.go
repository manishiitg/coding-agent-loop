package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
	if !candidate.Deprecated {
		t.Fatal("gemini-cli discovery candidate should be marked deprecated")
	}
	if candidate.ReplacementProvider != "pi-cli" {
		t.Fatalf("replacement_provider = %q, want pi-cli", candidate.ReplacementProvider)
	}
}

func TestProviderManifestMarksDeprecatedCodingAgents(t *testing.T) {
	t.Setenv("WORKSPACE_DOCS_PATH", t.TempDir())
	t.Setenv("SUPPORTED_LLM_PROVIDERS", "gemini-cli,agy-cli,pi-cli")
	t.Setenv("PATH", t.TempDir())

	api := &StreamingAPI{}
	req := httptest.NewRequest(http.MethodGet, "/api/llm-config/providers", nil)
	rec := httptest.NewRecorder()
	api.handleGetProviderManifest(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("manifest status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Providers []struct {
			ID                  string `json:"id"`
			Deprecated          bool   `json:"deprecated"`
			ReplacementProvider string `json:"replacement_provider"`
			CodingAgent         *struct {
				SupportsStatusLine      bool `json:"supports_status_line"`
				UsesMCPBridge           bool `json:"uses_mcp_bridge"`
				SupportsBridgeOnlyTools bool `json:"supports_bridge_only_tools"`
				SupportsNativeResume    bool `json:"supports_native_resume"`
				HandlesTmuxSessionLoss  bool `json:"handles_tmux_session_loss"`
			} `json:"coding_agent"`
		} `json:"providers"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}

	want := map[string]string{
		"gemini-cli": "pi-cli",
		"agy-cli":    "pi-cli",
	}
	seen := map[string]bool{}
	for _, provider := range resp.Providers {
		replacement, ok := want[provider.ID]
		if !ok {
			if provider.Deprecated {
				t.Fatalf("%s unexpectedly marked deprecated", provider.ID)
			}
			continue
		}
		seen[provider.ID] = true
		if !provider.Deprecated {
			t.Fatalf("%s deprecated = false, want true", provider.ID)
		}
		if provider.ReplacementProvider != replacement {
			t.Fatalf("%s replacement_provider = %q, want %q", provider.ID, provider.ReplacementProvider, replacement)
		}
	}
	for _, provider := range resp.Providers {
		if provider.ID == "pi-cli" {
			if provider.CodingAgent == nil {
				t.Fatal("pi-cli missing coding_agent manifest block")
			}
			if !provider.CodingAgent.SupportsStatusLine {
				t.Fatal("pi-cli supports_status_line = false, want true")
			}
			if !provider.CodingAgent.UsesMCPBridge {
				t.Fatal("pi-cli uses_mcp_bridge = false, want true")
			}
			if !provider.CodingAgent.SupportsBridgeOnlyTools {
				t.Fatal("pi-cli supports_bridge_only_tools = false, want true")
			}
			if !provider.CodingAgent.SupportsNativeResume {
				t.Fatal("pi-cli supports_native_resume = false, want true")
			}
			if !provider.CodingAgent.HandlesTmuxSessionLoss {
				t.Fatal("pi-cli handles_tmux_session_loss = false, want true")
			}
		}
	}
	for provider := range want {
		if !seen[provider] {
			t.Fatalf("manifest missing %s", provider)
		}
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

func TestAgyCLIIsDeprecatedButRuntimeAllowed(t *testing.T) {
	t.Setenv("WORKSPACE_DOCS_PATH", t.TempDir())
	t.Setenv("SUPPORTED_LLM_PROVIDERS", "agy-cli")
	t.Setenv("PATH", t.TempDir())

	if !isPublishedLLMProviderAllowed("agy-cli") {
		t.Fatal("agy-cli should remain allowed for restored/published legacy provider lists")
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
	if candidate.Label != "Antigravity CLI (Deprecated)" {
		t.Fatalf("label = %q, want deprecated label", candidate.Label)
	}
	if !candidate.Deprecated {
		t.Fatal("agy-cli discovery candidate should be marked deprecated")
	}
	if candidate.ReplacementProvider != "pi-cli" {
		t.Fatalf("replacement_provider = %q, want pi-cli", candidate.ReplacementProvider)
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

func TestPiCLIIsPublishedAsCodingAgent(t *testing.T) {
	t.Setenv("WORKSPACE_DOCS_PATH", t.TempDir())
	t.Setenv("SUPPORTED_LLM_PROVIDERS", "pi-cli")
	t.Setenv("PATH", t.TempDir())
	t.Setenv("PI_API_KEY", "")
	t.Setenv("GEMINI_API_KEY", "")
	t.Setenv("GOOGLE_API_KEY", "")

	if !isPublishedLLMProviderAllowed("pi-cli") {
		t.Fatal("pi-cli should be allowed in published provider lists")
	}
	foundSupported := false
	for _, provider := range getSupportedProviders() {
		if provider == "pi-cli" {
			foundSupported = true
		}
	}
	if !foundSupported {
		t.Fatalf("supported providers missing pi-cli: %v", getSupportedProviders())
	}

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
		t.Fatalf("runtime_available = %v, want false for missing pi/npx runtime", candidate.RuntimeAvailable)
	}
	if candidate.Usable {
		t.Fatal("usable = true, want false when pi/npx runtime is missing")
	}
	if len(candidate.Options) != 1 || candidate.Options[0] != "google/gemini-3.5-flash" {
		t.Fatalf("options = %v, want [google/gemini-3.5-flash]", candidate.Options)
	}
}
