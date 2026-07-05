package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	llm "github.com/manishiitg/mcpagent/llm"
)

func TestMergeStoredProviderKeyValuesPreservesAndUpdatesProviderKeys(t *testing.T) {
	existing := &StoredProviderKeys{
		OpenAI:   "openai-existing",
		ZAI:      "zai-existing",
		Kimi:     "kimi-existing",
		CodexCLI: "codex-existing",
		PiCLI:    "pi-existing",
		PiProviderKeys: map[string]string{
			"google":   "google-existing",
			"deepseek": "deepseek-existing",
		},
		MiniMax:           "minimax-existing",
		MiniMaxCodingPlan: "coding-existing",
		OpenRouter:        "openrouter-existing",
	}

	incoming := &StoredProviderKeys{
		ZAI:     "zai-new",
		MiniMax: "__DELETE__",
		PiProviderKeys: map[string]string{
			"zai":      "zai-pi-new",
			"deepseek": "__DELETE__",
		},
	}

	merged := mergeStoredProviderKeyValues(existing, incoming)

	if merged.OpenAI != "openai-existing" {
		t.Fatalf("expected OpenAI key to be preserved, got %q", merged.OpenAI)
	}
	if merged.ZAI != "zai-new" {
		t.Fatalf("expected ZAI key to be updated, got %q", merged.ZAI)
	}
	if merged.Kimi != "kimi-existing" {
		t.Fatalf("expected Kimi key to be preserved, got %q", merged.Kimi)
	}
	if merged.CodexCLI != "codex-existing" {
		t.Fatalf("expected Codex CLI key to be preserved, got %q", merged.CodexCLI)
	}
	if merged.PiCLI != "pi-existing" {
		t.Fatalf("expected Pi CLI key to be preserved, got %q", merged.PiCLI)
	}
	if merged.PiProviderKeys["google"] != "google-existing" {
		t.Fatalf("expected Pi google key to be preserved, got %q", merged.PiProviderKeys["google"])
	}
	if merged.PiProviderKeys["zai"] != "zai-pi-new" {
		t.Fatalf("expected Pi zai key to be updated, got %q", merged.PiProviderKeys["zai"])
	}
	if _, ok := merged.PiProviderKeys["deepseek"]; ok {
		t.Fatalf("expected Pi deepseek key to be deleted, got %q", merged.PiProviderKeys["deepseek"])
	}
	if merged.MiniMax != "" {
		t.Fatalf("expected MiniMax key to be deleted, got %q", merged.MiniMax)
	}
	if merged.MiniMaxCodingPlan != "" {
		t.Fatalf("expected MiniMax coding plan key to be removed, got %q", merged.MiniMaxCodingPlan)
	}
	if merged.OpenRouter != "openrouter-existing" {
		t.Fatalf("expected OpenRouter key to be preserved, got %q", merged.OpenRouter)
	}
}

func TestMergeStoredProviderKeyValuesIgnoresMaskedIncomingKeys(t *testing.T) {
	maskedIncoming := maskedProviderKeyPrefix + "cret"
	existing := &StoredProviderKeys{
		OpenAI: "openai-existing-secret",
		PiProviderKeys: map[string]string{
			"zai": "zai-existing-secret",
		},
		Azure: &StoredAzureConfig{
			Endpoint: "https://old.openai.azure.com",
			APIKey:   "azure-existing-secret",
		},
	}
	incoming := &StoredProviderKeys{
		OpenAI: maskedIncoming,
		PiProviderKeys: map[string]string{
			"zai": maskedIncoming,
		},
		Azure: &StoredAzureConfig{
			Endpoint: "https://new.openai.azure.com",
			APIKey:   maskedIncoming,
		},
	}

	merged := mergeStoredProviderKeyValues(existing, incoming)

	if merged.OpenAI != "openai-existing-secret" {
		t.Fatalf("OpenAI = %q, want existing secret", merged.OpenAI)
	}
	if merged.PiProviderKeys["zai"] != "zai-existing-secret" {
		t.Fatalf("PiProviderKeys[zai] = %q, want existing secret", merged.PiProviderKeys["zai"])
	}
	if merged.Azure.APIKey != "azure-existing-secret" {
		t.Fatalf("Azure APIKey = %q, want existing secret", merged.Azure.APIKey)
	}
	if merged.Azure.Endpoint != "https://new.openai.azure.com" {
		t.Fatalf("Azure Endpoint = %q, want updated endpoint", merged.Azure.Endpoint)
	}
}

func TestMaskStoredProviderKeysMasksSecretsWithLastFour(t *testing.T) {
	keys := &StoredProviderKeys{
		OpenAI: "sk-openai-123456",
		PiProviderKeys: map[string]string{
			"zai": "zai-secret-7890",
		},
		Azure: &StoredAzureConfig{
			Endpoint: "https://example.openai.azure.com",
			APIKey:   "azure-secret-3456",
		},
	}

	masked := maskStoredProviderKeys(keys)

	if masked.OpenAI != maskedProviderKeyPrefix+"3456" {
		t.Fatalf("masked OpenAI = %q", masked.OpenAI)
	}
	if masked.PiProviderKeys["zai"] != maskedProviderKeyPrefix+"7890" {
		t.Fatalf("masked Pi key = %q", masked.PiProviderKeys["zai"])
	}
	if masked.Azure.APIKey != maskedProviderKeyPrefix+"3456" {
		t.Fatalf("masked Azure key = %q", masked.Azure.APIKey)
	}
	if masked.Azure.Endpoint != keys.Azure.Endpoint {
		t.Fatalf("Azure endpoint changed: %q", masked.Azure.Endpoint)
	}
	if keys.OpenAI != "sk-openai-123456" {
		t.Fatalf("masking mutated original key: %q", keys.OpenAI)
	}
}

func TestHasStoredProviderKeysRequiresMeaningfulValues(t *testing.T) {
	tests := []struct {
		name string
		keys *StoredProviderKeys
		want bool
	}{
		{name: "nil", keys: nil, want: false},
		{name: "empty", keys: &StoredProviderKeys{}, want: false},
		{name: "whitespace key", keys: &StoredProviderKeys{OpenAI: "   "}, want: false},
		{name: "masked key", keys: &StoredProviderKeys{OpenAI: maskedProviderKeyPrefix + "1234"}, want: false},
		{name: "api key", keys: &StoredProviderKeys{OpenAI: "sk-test"}, want: true},
		{name: "pi cli key", keys: &StoredProviderKeys{PiCLI: "pi-key"}, want: true},
		{name: "pi provider key", keys: &StoredProviderKeys{PiProviderKeys: map[string]string{"zai": "zai-key"}}, want: true},
		{name: "bedrock region", keys: &StoredProviderKeys{Bedrock: &StoredBedrockConfig{Region: "us-east-1"}}, want: true},
		{
			name: "azure requires endpoint and key",
			keys: &StoredProviderKeys{Azure: &StoredAzureConfig{Endpoint: "https://example.openai.azure.com"}},
			want: false,
		},
		{
			name: "azure endpoint and key",
			keys: &StoredProviderKeys{Azure: &StoredAzureConfig{Endpoint: "https://example.openai.azure.com", APIKey: "azure-key"}},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hasStoredProviderKeys(tt.keys); got != tt.want {
				t.Fatalf("hasStoredProviderKeys() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSelectPiAPIKeyForModelUsesModelProviderPrefix(t *testing.T) {
	piKey := "gemini-key"
	zaiKey := "zai-key"
	kimiKey := "kimi-key"
	openRouterKey := "openrouter-key"
	keys := &llm.ProviderAPIKeys{
		PiCLI:      &piKey,
		ZAI:        &zaiKey,
		Kimi:       &kimiKey,
		OpenRouter: &openRouterKey,
		PiProviderKeys: map[string]string{
			"deepseek":      "deepseek-key",
			"zai-coding-cn": "zai-cn-key",
		},
	}

	tests := []struct {
		modelID string
		want    string
	}{
		{modelID: "google/gemini-3.5-flash", want: "gemini-key"},
		{modelID: "zai/glm-5.2", want: "zai-key"},
		{modelID: "zai-coding-cn/glm-5.2", want: "zai-cn-key"},
		{modelID: "kimi-coding/k2p7", want: "kimi-key"},
		{modelID: "deepseek/deepseek-v4-pro", want: "deepseek-key"},
		{modelID: "openrouter/minimax/minimax-m3-20260531", want: "openrouter-key"},
	}
	for _, tt := range tests {
		t.Run(tt.modelID, func(t *testing.T) {
			if got := selectPiAPIKeyForModel(keys, tt.modelID); got != tt.want {
				t.Fatalf("selectPiAPIKeyForModel(%q) = %q, want %q", tt.modelID, got, tt.want)
			}
		})
	}
}

func TestSetStoredPiProviderAPIKeyUsesModelPrefix(t *testing.T) {
	keys := &StoredProviderKeys{}

	provider, ok := setStoredPiProviderAPIKey(keys, "", "openrouter/minimax/minimax-m3-20260531", " openrouter-key ")
	if !ok {
		t.Fatal("setStoredPiProviderAPIKey() ok = false, want true")
	}
	if provider != "openrouter" {
		t.Fatalf("provider = %q, want openrouter", provider)
	}
	if keys.PiProviderKeys["openrouter"] != "openrouter-key" {
		t.Fatalf("PiProviderKeys[openrouter] = %q, want openrouter-key", keys.PiProviderKeys["openrouter"])
	}
	if keys.OpenRouter != "openrouter-key" {
		t.Fatalf("OpenRouter = %q, want openrouter-key", keys.OpenRouter)
	}
}

func TestSetStoredPiProviderAPIKeyAllowsExplicitProvider(t *testing.T) {
	keys := &StoredProviderKeys{}

	provider, ok := setStoredPiProviderAPIKey(keys, "deepseek", "", "deepseek-key")
	if !ok {
		t.Fatal("setStoredPiProviderAPIKey() ok = false, want true")
	}
	if provider != "deepseek" {
		t.Fatalf("provider = %q, want deepseek", provider)
	}
	if keys.PiProviderKeys["deepseek"] != "deepseek-key" {
		t.Fatalf("PiProviderKeys[deepseek] = %q, want deepseek-key", keys.PiProviderKeys["deepseek"])
	}
}

func TestSelectStoredPiAPIKeyForModelUsesProviderSpecificKey(t *testing.T) {
	keys := &StoredProviderKeys{
		OpenRouter: "openrouter-key",
		PiProviderKeys: map[string]string{
			"deepseek": "deepseek-key",
		},
	}

	if got := selectStoredPiAPIKeyForModel(keys, "openrouter/minimax/minimax-m3-20260531"); got != "openrouter-key" {
		t.Fatalf("selectStoredPiAPIKeyForModel(openrouter model) = %q, want openrouter-key", got)
	}
	if got := selectStoredPiAPIKeyForModel(keys, "deepseek/deepseek-v4-pro-20260423"); got != "deepseek-key" {
		t.Fatalf("selectStoredPiAPIKeyForModel(deepseek model) = %q, want deepseek-key", got)
	}
}

func TestSaveProviderKeysDeletesEmptyStore(t *testing.T) {
	var called bool
	var method string
	var requestPath string
	var confirm string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		method = r.Method
		requestPath = r.URL.Path
		confirm = r.URL.Query().Get("confirm")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	t.Setenv("WORKSPACE_API_URL", server.URL)

	if err := SaveProviderKeys(context.Background(), &StoredProviderKeys{}); err != nil {
		t.Fatalf("SaveProviderKeys() error = %v", err)
	}
	if !called {
		t.Fatal("expected empty provider key store to delete provider keys file")
	}
	if method != http.MethodDelete {
		t.Fatalf("method = %s, want DELETE", method)
	}
	if requestPath != "/api/documents/config/provider-api-keys.json" {
		t.Fatalf("path = %s, want /api/documents/config/provider-api-keys.json", requestPath)
	}
	if confirm != "true" {
		t.Fatalf("confirm = %q, want true", confirm)
	}
}
