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

func TestNormalizeImageAnalysisProviderAndModelRejectsMiniMax(t *testing.T) {
	_, _, err := normalizeImageAnalysisProviderAndModel("minimax-coding-plan", "")
	if err == nil {
		t.Fatal("normalizeImageAnalysisProviderAndModel returned nil error for minimax-coding-plan")
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
	if modelID != "gpt-5.4-mini" {
		t.Fatalf("modelID = %q, want gpt-5.4-mini", modelID)
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

func TestNormalizeImageAnalysisProviderAndModelCursorDefault(t *testing.T) {
	provider, modelID, err := normalizeImageAnalysisProviderAndModel("cursor-cli", "")
	if err != nil {
		t.Fatalf("normalizeImageAnalysisProviderAndModel returned error: %v", err)
	}
	if provider != "cursor-cli" {
		t.Fatalf("provider = %q, want cursor-cli", provider)
	}
	if modelID != "cursor-cli" {
		t.Fatalf("modelID = %q, want cursor-cli", modelID)
	}
}

func TestNormalizeImageAnalysisProviderAndModelAgyDefault(t *testing.T) {
	provider, modelID, err := normalizeImageAnalysisProviderAndModel("agy-cli", "")
	if err != nil {
		t.Fatalf("normalizeImageAnalysisProviderAndModel returned error: %v", err)
	}
	if provider != "agy-cli" {
		t.Fatalf("provider = %q, want agy-cli", provider)
	}
	if modelID != "agy-cli" {
		t.Fatalf("modelID = %q, want agy-cli", modelID)
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

func TestNormalizeImageAnalysisProviderAndModelInfersCursorFromModel(t *testing.T) {
	provider, modelID, err := normalizeImageAnalysisProviderAndModel("", "sonnet-4-thinking")
	if err != nil {
		t.Fatalf("normalizeImageAnalysisProviderAndModel returned error: %v", err)
	}
	if provider != "cursor-cli" {
		t.Fatalf("provider = %q, want cursor-cli", provider)
	}
	if modelID != "sonnet-4-thinking" {
		t.Fatalf("modelID = %q, want sonnet-4-thinking", modelID)
	}
}

func TestNormalizeImageAnalysisProviderAndModelInfersClaudeCodeFromSonnet5(t *testing.T) {
	provider, modelID, err := normalizeImageAnalysisProviderAndModel("", "claude-sonnet-5")
	if err != nil {
		t.Fatalf("normalizeImageAnalysisProviderAndModel returned error: %v", err)
	}
	if provider != "claude-code" {
		t.Fatalf("provider = %q, want claude-code", provider)
	}
	if modelID != "claude-sonnet-5" {
		t.Fatalf("modelID = %q, want claude-sonnet-5", modelID)
	}
}

func TestNormalizeImageAnalysisProviderAndModelInfersAgyFromModel(t *testing.T) {
	provider, modelID, err := normalizeImageAnalysisProviderAndModel("", "agy-cli")
	if err != nil {
		t.Fatalf("normalizeImageAnalysisProviderAndModel returned error: %v", err)
	}
	if provider != "agy-cli" {
		t.Fatalf("provider = %q, want agy-cli", provider)
	}
	if modelID != "agy-cli" {
		t.Fatalf("modelID = %q, want agy-cli", modelID)
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

func TestPathBasedImageAnalysisProviderIncludesAgy(t *testing.T) {
	for _, provider := range []string{"codex-cli", "cursor-cli", "agy-cli", "claude-code"} {
		if !pathBasedImageAnalysisProvider(provider) {
			t.Fatalf("pathBasedImageAnalysisProvider(%q) = false, want true", provider)
		}
	}
	if pathBasedImageAnalysisProvider("vertex") {
		t.Fatal("pathBasedImageAnalysisProvider(vertex) = true, want false")
	}
}
