package virtualtools

import (
	"strings"
	"testing"
)

func TestNormalizeImageProviderAndModelProviderAliasDefaultsModel(t *testing.T) {
	tests := []struct {
		name      string
		provider  string
		modelID   string
		wantModel string
	}{
		{
			name:      "vertex alias",
			provider:  "vertex",
			modelID:   "vertex",
			wantModel: "gemini-3.1-flash-image",
		},
		{
			name:      "codex alias",
			provider:  "codex-cli",
			modelID:   "codex-cli",
			wantModel: "codex-cli",
		},
		{
			name:      "agy alias",
			provider:  "agy-cli",
			modelID:   "agy-cli",
			wantModel: "agy-cli",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider, modelID, err := normalizeImageProviderAndModel(tt.provider, tt.modelID)
			if err != nil {
				t.Fatalf("normalizeImageProviderAndModel returned error: %v", err)
			}
			if provider != tt.provider {
				t.Fatalf("provider = %q, want %q", provider, tt.provider)
			}
			if modelID != tt.wantModel {
				t.Fatalf("modelID = %q, want %q", modelID, tt.wantModel)
			}
		})
	}
}

func TestNormalizeImageProviderAndModelRejectsWrongModelForProvider(t *testing.T) {
	_, _, err := normalizeImageProviderAndModel("codex-cli", "gemini-3.1-flash-image")
	if err == nil {
		t.Fatal("normalizeImageProviderAndModel returned nil error for unsupported provider/model pair")
	}
}

func TestNormalizeImageProviderAndModelMigratesLegacyImagen(t *testing.T) {
	provider, modelID, err := normalizeImageProviderAndModel("vertex", "imagen-deprecated-model")
	if err != nil {
		t.Fatalf("normalizeImageProviderAndModel returned error: %v", err)
	}
	if provider != "vertex" {
		t.Fatalf("provider = %q, want vertex", provider)
	}
	if modelID != "gemini-3.1-flash-image" {
		t.Fatalf("modelID = %q, want gemini-3.1-flash-image", modelID)
	}
}

func TestNormalizeImageProviderAndModelMigratesPreviewGeminiImage(t *testing.T) {
	provider, modelID, err := normalizeImageProviderAndModel("vertex", "gemini-3.1-flash-image-preview")
	if err != nil {
		t.Fatalf("normalizeImageProviderAndModel returned error: %v", err)
	}
	if provider != "vertex" {
		t.Fatalf("provider = %q, want vertex", provider)
	}
	if modelID != "gemini-3.1-flash-image" {
		t.Fatalf("modelID = %q, want gemini-3.1-flash-image", modelID)
	}
}

func TestNormalizeImageProviderAndModelRejectsMiniMaxCodingPlan(t *testing.T) {
	_, _, err := normalizeImageProviderAndModel("minimax-coding-plan", "")
	if err == nil {
		t.Fatal("normalizeImageProviderAndModel returned nil error for removed minimax-coding-plan provider")
	}
}

func TestNormalizeImageProviderAndModelRejectsTierLabelsAsImageModels(t *testing.T) {
	for _, tier := range []string{"low", "medium", "high", "auto"} {
		t.Run(tier, func(t *testing.T) {
			_, _, err := normalizeImageProviderAndModel("codex-cli", tier)
			if err == nil {
				t.Fatal("normalizeImageProviderAndModel returned nil error for tier label")
			}
			if !strings.Contains(err.Error(), "unsupported image generation model") {
				t.Fatalf("error = %v, want unsupported model", err)
			}
		})
	}
}

func TestNormalizeImageProviderAndModelInfersAgyFromModel(t *testing.T) {
	provider, modelID, err := normalizeImageProviderAndModel("", "agy-cli")
	if err != nil {
		t.Fatalf("normalizeImageProviderAndModel returned error: %v", err)
	}
	if provider != "agy-cli" {
		t.Fatalf("provider = %q, want agy-cli", provider)
	}
	if modelID != "agy-cli" {
		t.Fatalf("modelID = %q, want agy-cli", modelID)
	}
}

func TestAgyImageGenerationAuthAndCostMetadata(t *testing.T) {
	if !hasImageProviderAuth("agy-cli", nil) {
		t.Fatal("hasImageProviderAuth(agy-cli) = false, want true for local CLI auth")
	}
	cost, note := imageGenerationCostMetadata("agy-cli", "agy-cli")
	if cost != nil {
		t.Fatalf("cost = %v, want nil fixed per-image cost", *cost)
	}
	if !strings.Contains(note, "Antigravity CLI") || !strings.Contains(note, "not free") {
		t.Fatalf("note = %q, want Antigravity non-free cost note", note)
	}
}
