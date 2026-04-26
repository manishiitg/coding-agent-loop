package virtualtools

import "testing"

func TestNormalizeImageAnalysisProviderAndModelMiniMaxDefault(t *testing.T) {
	provider, modelID, err := normalizeImageAnalysisProviderAndModel("minimax-coding-plan", "")
	if err != nil {
		t.Fatalf("normalizeImageAnalysisProviderAndModel returned error: %v", err)
	}
	if provider != "minimax-coding-plan" {
		t.Fatalf("provider = %q, want minimax-coding-plan", provider)
	}
	if modelID != "claude-sonnet-4-5" {
		t.Fatalf("modelID = %q, want claude-sonnet-4-5", modelID)
	}
}

func TestNormalizeImageAnalysisProviderAndModelInfersMiniMaxFromClaudeModel(t *testing.T) {
	provider, modelID, err := normalizeImageAnalysisProviderAndModel("", "claude-sonnet-4-5")
	if err != nil {
		t.Fatalf("normalizeImageAnalysisProviderAndModel returned error: %v", err)
	}
	if provider != "minimax-coding-plan" {
		t.Fatalf("provider = %q, want minimax-coding-plan", provider)
	}
	if modelID != "claude-sonnet-4-5" {
		t.Fatalf("modelID = %q, want claude-sonnet-4-5", modelID)
	}
}

func TestNormalizeImageAnalysisProviderAndModelKimiDefault(t *testing.T) {
	provider, modelID, err := normalizeImageAnalysisProviderAndModel("kimi", "")
	if err != nil {
		t.Fatalf("normalizeImageAnalysisProviderAndModel returned error: %v", err)
	}
	if provider != "kimi" {
		t.Fatalf("provider = %q, want kimi", provider)
	}
	if modelID != "kimi-k2.6" {
		t.Fatalf("modelID = %q, want kimi-k2.6", modelID)
	}
}

func TestNormalizeImageAnalysisProviderAndModelInfersKimiFromVisionModel(t *testing.T) {
	provider, modelID, err := normalizeImageAnalysisProviderAndModel("", "kimi-k2.6")
	if err != nil {
		t.Fatalf("normalizeImageAnalysisProviderAndModel returned error: %v", err)
	}
	if provider != "kimi" {
		t.Fatalf("provider = %q, want kimi", provider)
	}
	if modelID != "kimi-k2.6" {
		t.Fatalf("modelID = %q, want kimi-k2.6", modelID)
	}
}
