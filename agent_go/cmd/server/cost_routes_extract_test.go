package server

import "testing"

// TestExtractCostAndEffectiveModelPullsBothFields locks in the contract
// that the cost-routes extractor sees the unified fields the
// multi-llm-provider-go CLI adapters now emit on
// GenerationInfo.Additional. If these keys ever drift in the
// adapters, this test breaks loudly.
func TestExtractCostAndEffectiveModelPullsBothFields(t *testing.T) {
	cases := []struct {
		name         string
		in           map[string]interface{}
		wantModel    string
		wantCostUSD  float64
	}{
		{
			name: "unified keys present",
			in: map[string]interface{}{
				"cost_usd_estimated": 0.0042,
				"cost_model_id":      "gpt-5.4",
			},
			wantModel:   "gpt-5.4",
			wantCostUSD: 0.0042,
		},
		{
			name: "fallback to claude_code_model",
			in: map[string]interface{}{
				"claude_code_model": "claude-opus-4-7",
			},
			wantModel:   "claude-opus-4-7",
			wantCostUSD: 0,
		},
		{
			name: "fallback to codex_effective_model",
			in: map[string]interface{}{
				"codex_effective_model": "gpt-5.4",
				"cost_usd_estimated":    0.01,
			},
			wantModel:   "gpt-5.4",
			wantCostUSD: 0.01,
		},
		{
			name: "fallback to gemini_effective_model",
			in: map[string]interface{}{
				"gemini_effective_model": "gemini-3.1-flash-lite",
			},
			wantModel: "gemini-3.1-flash-lite",
		},
		{
			name: "fallback to cursor_model",
			in: map[string]interface{}{
				"cursor_model": "composer-2.5",
			},
			wantModel: "composer-2.5",
		},
		{
			name: "cost_model_id wins over per-provider key",
			in: map[string]interface{}{
				"cost_model_id":      "explicit-id",
				"claude_code_model":  "different-id",
				"cost_usd_estimated": 0.5,
			},
			wantModel:   "explicit-id",
			wantCostUSD: 0.5,
		},
		{
			name: "missing keys → empties",
			in: map[string]interface{}{
				"unrelated_field": "x",
			},
		},
		{
			name: "nil input → empties",
			in:   nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotModel, gotCost := extractCostAndEffectiveModel(tc.in)
			if gotModel != tc.wantModel {
				t.Errorf("model = %q, want %q", gotModel, tc.wantModel)
			}
			if gotCost != tc.wantCostUSD {
				t.Errorf("cost = %v, want %v", gotCost, tc.wantCostUSD)
			}
		})
	}
}
