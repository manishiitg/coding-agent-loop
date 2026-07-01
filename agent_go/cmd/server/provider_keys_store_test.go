package server

import "testing"

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
