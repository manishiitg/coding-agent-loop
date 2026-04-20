package server

import "testing"

func TestMergeStoredProviderKeyValuesPreservesAndUpdatesProviderKeys(t *testing.T) {
	existing := &StoredProviderKeys{
		OpenAI:            "openai-existing",
		ZAI:               "zai-existing",
		MiniMax:           "minimax-existing",
		MiniMaxCodingPlan: "coding-existing",
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
	if merged.MiniMax != "" {
		t.Fatalf("expected MiniMax key to be deleted, got %q", merged.MiniMax)
	}
	if merged.MiniMaxCodingPlan != "coding-existing" {
		t.Fatalf("expected MiniMax coding plan key to be preserved, got %q", merged.MiniMaxCodingPlan)
	}
}
