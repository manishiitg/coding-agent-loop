package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMergeStoredProviderKeyValuesPreservesAndUpdatesProviderKeys(t *testing.T) {
	existing := &StoredProviderKeys{
		OpenAI:            "openai-existing",
		ZAI:               "zai-existing",
		Kimi:              "kimi-existing",
		CodexCLI:          "codex-existing",
		PiCLI:             "pi-existing",
		MiniMax:           "minimax-existing",
		MiniMaxCodingPlan: "coding-existing",
		OpenRouter:        "openrouter-existing",
	}

	incoming := &StoredProviderKeys{
		ZAI:     "zai-new",
		MiniMax: "__DELETE__",
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
	if merged.MiniMax != "" {
		t.Fatalf("expected MiniMax key to be deleted, got %q", merged.MiniMax)
	}
	if merged.MiniMaxCodingPlan != "" {
		t.Fatalf("expected MiniMax coding plan key to be removed, got %q", merged.MiniMaxCodingPlan)
	}
	if merged.OpenRouter != "" {
		t.Fatalf("expected OpenRouter key to be removed, got %q", merged.OpenRouter)
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
		{name: "api key", keys: &StoredProviderKeys{OpenAI: "sk-test"}, want: true},
		{name: "pi cli key", keys: &StoredProviderKeys{PiCLI: "pi-key"}, want: true},
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
