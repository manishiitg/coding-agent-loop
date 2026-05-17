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
			wantModel: "gemini-3.1-flash-image-preview",
		},
		{
			name:      "minimax alias",
			provider:  "minimax-coding-plan",
			modelID:   "minimax-coding-plan",
			wantModel: "image-01",
		},
		{
			name:      "codex alias",
			provider:  "codex-cli",
			modelID:   "codex-cli",
			wantModel: "codex-cli",
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
	_, _, err := normalizeImageProviderAndModel("minimax-coding-plan", "gemini-3.1-flash-image-preview")
	if err == nil {
		t.Fatal("normalizeImageProviderAndModel returned nil error for unsupported provider/model pair")
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
