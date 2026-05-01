package virtualtools

import "testing"

func TestNormalizeImageAnalysisProviderAndModelVertexDefault(t *testing.T) {
	provider, modelID, err := normalizeImageAnalysisProviderAndModel("vertex", "")
	if err != nil {
		t.Fatalf("normalizeImageAnalysisProviderAndModel returned error: %v", err)
	}
	if provider != "vertex" {
		t.Fatalf("provider = %q, want vertex", provider)
	}
	if modelID != "gemini-3-pro-preview" {
		t.Fatalf("modelID = %q, want gemini-3-pro-preview", modelID)
	}
}

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

func TestNormalizeImageAnalysisProviderAndModelCodexDefault(t *testing.T) {
	provider, modelID, err := normalizeImageAnalysisProviderAndModel("codex-cli", "")
	if err != nil {
		t.Fatalf("normalizeImageAnalysisProviderAndModel returned error: %v", err)
	}
	if provider != "codex-cli" {
		t.Fatalf("provider = %q, want codex-cli", provider)
	}
	if modelID != "codex-cli" {
		t.Fatalf("modelID = %q, want codex-cli", modelID)
	}
}

func TestNormalizeImageAnalysisProviderAndModelClaudeCodeDefault(t *testing.T) {
	provider, modelID, err := normalizeImageAnalysisProviderAndModel("claude-code", "")
	if err != nil {
		t.Fatalf("normalizeImageAnalysisProviderAndModel returned error: %v", err)
	}
	if provider != "claude-code" {
		t.Fatalf("provider = %q, want claude-code", provider)
	}
	if modelID != "claude-code" {
		t.Fatalf("modelID = %q, want claude-code", modelID)
	}
}

func TestNormalizeImageAnalysisProviderAndModelInfersCodexFromModel(t *testing.T) {
	provider, modelID, err := normalizeImageAnalysisProviderAndModel("", "gpt-5.4-mini")
	if err != nil {
		t.Fatalf("normalizeImageAnalysisProviderAndModel returned error: %v", err)
	}
	if provider != "codex-cli" {
		t.Fatalf("provider = %q, want codex-cli", provider)
	}
	if modelID != "gpt-5.4-mini" {
		t.Fatalf("modelID = %q, want gpt-5.4-mini", modelID)
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
