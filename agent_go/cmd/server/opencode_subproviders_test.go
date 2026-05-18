package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProviderManifestExposesOpenCodeSubProviders(t *testing.T) {
	api := &StreamingAPI{}
	req := httptest.NewRequest(http.MethodGet, "/api/llm-config/providers", nil)
	rec := httptest.NewRecorder()
	api.handleGetProviderManifest(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("manifest status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Providers []struct {
			ID                 string `json:"id"`
			DisplayName        string `json:"display_name"`
			IntegrationKind    string `json:"integration_kind"`
			ModelSelectionMode string `json:"model_selection_mode"`
			RequiresAPIKey     bool   `json:"requires_api_key"`
			APIKeyEnv          string `json:"api_key_env"`
			Models             []struct {
				ModelID string `json:"model_id"`
			} `json:"models"`
		} `json:"providers"`
		ProviderOrder []string `json:"provider_order"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode manifest: %v\nbody=%s", err, rec.Body.String())
	}

	wantIDs := []string{
		"opencode-cli-kimi",
		"opencode-cli-deepseek",
		"opencode-cli-qwen",
		"opencode-cli-minimax",
		"opencode-cli-glm",
		"opencode-cli-free",
	}
	wantEnvVars := map[string]string{
		"opencode-cli-kimi":     "KIMI_API_KEY",
		"opencode-cli-deepseek": "DEEPSEEK_API_KEY",
		"opencode-cli-qwen":     "DASHSCOPE_API_KEY",
		"opencode-cli-minimax":  "MINIMAX_API_KEY",
		"opencode-cli-glm":      "ZHIPU_API_KEY",
		"opencode-cli-free":     "",
	}

	byID := map[string]bool{}
	for _, p := range resp.Providers {
		byID[p.ID] = true
		if wantEnv, ok := wantEnvVars[p.ID]; ok {
			if p.APIKeyEnv != wantEnv {
				t.Errorf("%s: api_key_env = %q, want %q", p.ID, p.APIKeyEnv, wantEnv)
			}
			if p.ID == "opencode-cli-free" {
				if p.RequiresAPIKey {
					t.Errorf("%s: RequiresAPIKey = true, want false (free tier)", p.ID)
				}
			} else if !p.RequiresAPIKey {
				t.Errorf("%s: RequiresAPIKey = false, want true", p.ID)
			}
			if p.IntegrationKind != "coding_agent" {
				t.Errorf("%s: IntegrationKind = %q, want coding_agent", p.ID, p.IntegrationKind)
			}
			if len(p.Models) == 0 {
				t.Errorf("%s: returned zero models", p.ID)
			}
		}
	}
	for _, id := range wantIDs {
		if !byID[id] {
			t.Errorf("manifest missing OpenCode sub-provider tile %q", id)
		}
	}

	// provider_order should advertise the sub-providers between
	// opencode-cli and claude-code so the UI groups them together.
	orderJoined := strings.Join(resp.ProviderOrder, ",")
	if !strings.Contains(orderJoined, "opencode-cli,opencode-cli-kimi") {
		t.Errorf("provider_order missing or misordered: %v", resp.ProviderOrder)
	}
}

func TestProviderRuntimeRoutesSubProvidersToOpenCode(t *testing.T) {
	cases := []string{
		"opencode-cli-kimi",
		"opencode-cli-deepseek",
		"opencode-cli-qwen",
		"opencode-cli-minimax",
		"opencode-cli-glm",
		"opencode-cli-free",
	}
	for _, id := range cases {
		t.Run(id, func(t *testing.T) {
			if got := providerRuntime(id); got != "opencode" {
				t.Fatalf("providerRuntime(%q) = %q, want opencode", id, got)
			}
		})
	}
}

func TestMergedOpenCodeSubProviderKeysReadsEnv(t *testing.T) {
	t.Setenv("KIMI_API_KEY", "sk-kimi-env-test")
	t.Setenv("DEEPSEEK_API_KEY", "sk-deepseek-env-test")
	t.Setenv("DASHSCOPE_API_KEY", "")
	got := MergedOpenCodeSubProviderKeys(context.Background())
	if v := got["KIMI_API_KEY"]; v != "sk-kimi-env-test" {
		t.Errorf("KIMI_API_KEY = %q, want sk-kimi-env-test", v)
	}
	if v := got["DEEPSEEK_API_KEY"]; v != "sk-deepseek-env-test" {
		t.Errorf("DEEPSEEK_API_KEY = %q, want sk-deepseek-env-test", v)
	}
	if _, ok := got["DASHSCOPE_API_KEY"]; ok {
		t.Errorf("DASHSCOPE_API_KEY should not be present when env is empty")
	}
}
