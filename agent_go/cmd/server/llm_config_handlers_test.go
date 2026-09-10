package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBuildProviderAPIKeysFromEnvUsesClaudeCodeSetupToken(t *testing.T) {
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "setup-token-for-test")

	keys := buildProviderAPIKeysFromEnv()
	if keys.ClaudeCodeOAuthToken == nil || *keys.ClaudeCodeOAuthToken != "setup-token-for-test" {
		t.Fatalf("ClaudeCodeOAuthToken = %#v, want configured setup token", keys.ClaudeCodeOAuthToken)
	}
}

func TestLLMDiscoveryHTTPShowsCursorLoginRequired(t *testing.T) {
	t.Setenv("WORKSPACE_DOCS_PATH", t.TempDir())
	t.Setenv("SUPPORTED_LLM_PROVIDERS", "cursor-cli")
	withFakeExecutable(t, "cursor-agent")
	withCursorStatusJSON(t, `{"status":"unauthenticated","isAuthenticated":false,"message":"Not logged in"}`, nil)

	api := &StreamingAPI{}
	req := httptest.NewRequest(http.MethodGet, "/api/llm-config/discovery", nil)
	rec := httptest.NewRecorder()
	api.handleDiscoverLLMSetup(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("discovery status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}

	var response llmDiscoveryResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode discovery response: %v", err)
	}
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
}

func TestClaudeCodeDiscoveryOptionsIncludeManualNewModels(t *testing.T) {
	options := discoveryModelOptions("claude-code")
	if !containsLLMCapabilityString(options, "claude-fable-5-1") {
		t.Fatalf("claude-code options = %v, want claude-fable-5-1", options)
	}
	if !containsLLMCapabilityString(options, "claude-opus-5") {
		t.Fatalf("claude-code options = %v, want claude-opus-5", options)
	}
	for _, modelID := range []string{"claude-opus-4-8", "claude-opus-4-7", "claude-opus-4-6"} {
		if !containsLLMCapabilityString(options, modelID) {
			t.Fatalf("claude-code options = %v, want %s", options, modelID)
		}
	}
	if !containsLLMCapabilityString(options, "claude-sonnet-5") {
		t.Fatalf("claude-code options = %v, want claude-sonnet-5", options)
	}
	if !containsLLMCapabilityString(options, "claude-sonnet-4-6") {
		t.Fatalf("claude-code options = %v, want claude-sonnet-4-6", options)
	}
	if !containsLLMCapabilityString(options, "high") {
		t.Fatalf("claude-code options = %v, want tier aliases preserved", options)
	}
}

func TestProviderManifestMarksDeprecatedCodingAgents(t *testing.T) {
	t.Setenv("WORKSPACE_DOCS_PATH", t.TempDir())
	t.Setenv("SUPPORTED_LLM_PROVIDERS", "pi-cli")
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
			Models              []struct {
				ModelID string `json:"model_id"`
			} `json:"models"`
			CodingAgent *struct {
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

	want := map[string]string{}
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
			modelIDs := make(map[string]bool, len(provider.Models))
			for _, model := range provider.Models {
				modelIDs[model.ModelID] = true
			}
			for _, modelID := range []string{"zai/glm-5.3", "moonshotai/kimi-k3"} {
				if !modelIDs[modelID] {
					t.Fatalf("pi-cli models = %v, want %s", modelIDs, modelID)
				}
			}
		}
	}
	for provider := range want {
		if !seen[provider] {
			t.Fatalf("manifest missing %s", provider)
		}
	}
}

func TestProviderManifestPublishesCodexTierDefaults(t *testing.T) {
	t.Setenv("WORKSPACE_DOCS_PATH", t.TempDir())
	t.Setenv("SUPPORTED_LLM_PROVIDERS", "codex-cli")
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
			ID     string `json:"id"`
			Models []struct {
				ModelID string `json:"model_id"`
			} `json:"models"`
			DefaultTierModels map[string]struct {
				ModelID string                 `json:"model_id"`
				Options map[string]interface{} `json:"options"`
			} `json:"default_tier_models"`
		} `json:"providers"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}

	for _, provider := range resp.Providers {
		if provider.ID != "codex-cli" {
			continue
		}
		models := make(map[string]bool, len(provider.Models))
		for _, model := range provider.Models {
			models[model.ModelID] = true
		}
		for _, modelID := range []string{"gpt-5.6-sol", "gpt-5.6-terra", "gpt-5.6-luna"} {
			if !models[modelID] {
				t.Fatalf("codex-cli models = %v, want %s", models, modelID)
			}
		}
		for tier, want := range map[string]struct {
			model  string
			effort string
		}{
			"high":   {model: "gpt-5.6-terra", effort: "medium"},
			"medium": {model: "gpt-5.6-luna", effort: "high"},
			"low":    {model: "gpt-5.6-luna", effort: "medium"},
			"pulse":  {model: "gpt-6-astra", effort: "medium"},
		} {
			got := provider.DefaultTierModels[tier]
			if got.ModelID != want.model || got.Options["reasoning_effort"] != want.effort {
				t.Fatalf("codex-cli %s default = %+v, want %s/%s", tier, got, want.model, want.effort)
			}
		}
		return
	}
	t.Fatal("manifest missing codex-cli")
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
	if len(candidate.Options) != len(piFallbackModels()) || candidate.Options[0] != "google/gemini-3.8-flash" {
		t.Fatalf("options = %v, want curated Pi shortlist starting with google/gemini-3.8-flash", candidate.Options)
	}
	foundOpenRouter := false
	for _, option := range candidate.Options {
		if option == "openrouter/minimax/minimax-m3-20260531" {
			foundOpenRouter = true
			break
		}
	}
	if !foundOpenRouter {
		t.Fatalf("options = %v, want OpenRouter MiniMax top model", candidate.Options)
	}
}

// TestIsPublishedLLMProviderAllowedDerivesFromRegistry pins the registry
// migration: the gate must accept exactly the shared ValidateProvider set
// (so new coding CLIs validate without an agent_go edit) while still
// rejecting retired media providers, empty, and unknown names.
func TestIsPublishedLLMProviderAllowedDerivesFromRegistry(t *testing.T) {
	for _, provider := range []string{
		"bedrock", "openai", "vertex", "anthropic", "azure",
		"claude-code", "codex-cli", "cursor-cli", "pi-cli",
		"openrouter", "z-ai", "kimi", "minimax", "minimax-coding-plan",
	} {
		if !isPublishedLLMProviderAllowed(provider) {
			t.Errorf("%q should be allowed (shared registry provider)", provider)
		}
	}
	for _, provider := range []string{"", "  ", "nope", "elevenlabs", "deepgram"} {
		if isPublishedLLMProviderAllowed(provider) {
			t.Errorf("%q should be rejected", provider)
		}
	}
	if !isPublishedLLMProviderAllowed("  Claude-Code ") {
		t.Error("provider matching should be case- and whitespace-insensitive")
	}
}

// TestProviderManifestMarksDeprecatedAPIModelProviders is the regression for
// the 2026-08-20 direct-API-transport deprecation
// (docs/design/api_transport_vs_pi_tradeoff.md). Uses the real HTTP handler,
// matching TestProviderManifestMarksDeprecatedCodingAgents's pattern.
func TestProviderManifestMarksDeprecatedAPIModelProviders(t *testing.T) {
	t.Setenv("WORKSPACE_DOCS_PATH", t.TempDir())
	t.Setenv("SUPPORTED_LLM_PROVIDERS", "openai,anthropic,vertex,bedrock,azure,minimax,elevenlabs,deepgram,pi-cli")
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
			DeprecationReason   string `json:"deprecation_reason"`
			ReplacementProvider string `json:"replacement_provider"`
			IntegrationKind     string `json:"integration_kind"`
		} `json:"providers"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}

	byID := map[string]struct {
		Deprecated          bool
		DeprecationReason   string
		ReplacementProvider string
		IntegrationKind     string
	}{}
	for _, p := range resp.Providers {
		byID[p.ID] = struct {
			Deprecated          bool
			DeprecationReason   string
			ReplacementProvider string
			IntegrationKind     string
		}{p.Deprecated, p.DeprecationReason, p.ReplacementProvider, p.IntegrationKind}
	}

	for _, id := range []string{"openai", "anthropic", "vertex", "bedrock", "azure"} {
		got, ok := byID[id]
		if !ok {
			t.Fatalf("%s missing from manifest entirely", id)
		}
		if !got.Deprecated {
			t.Errorf("%s deprecated = false, want true", id)
		}
		if got.ReplacementProvider != "pi-cli" {
			t.Errorf("%s replacement_provider = %q, want %q", id, got.ReplacementProvider, "pi-cli")
		}
		if strings.TrimSpace(got.DeprecationReason) == "" {
			t.Errorf("%s deprecation_reason is empty", id)
		}
	}

	// These were media-tool-only direct providers. They must not come back into
	// the setup manifest just because an older deployment still lists them in
	// SUPPORTED_LLM_PROVIDERS. MiniMax remains available as a Pi sub-provider,
	// not as a top-level provider entry.
	for _, id := range []string{"minimax", "elevenlabs", "deepgram"} {
		if _, ok := byID[id]; ok {
			t.Errorf("retired media provider %q appeared in the setup manifest", id)
		}
	}

	// pi-cli is the replacement, not another casualty.
	if got, ok := byID["pi-cli"]; ok && got.Deprecated {
		t.Error("pi-cli unexpectedly marked deprecated")
	}
}
