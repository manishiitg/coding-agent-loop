package sparkquillproduct

import "testing"

// Every provider option must declare a reasoning_effort — deliberate, not
// left to whatever a provider defaults to — and one of its own declared
// reasoning_efforts choices, so the composer's picker always starts on a
// value it can actually select. The specific defaults are chosen per
// profile and provider (the parent gets more deliberation than the child on
// Codex; Claude Code runs the child on a stronger model at a lower effort
// than the parent), not a single fixed value across the board.
func TestEveryProviderOptionDeclaresAnOwnReasoningEffort(t *testing.T) {
	profiles := BuiltinAgentProfiles()
	if len(profiles) == 0 {
		t.Fatal("no built-in profiles")
	}
	for _, p := range profiles {
		if len(p.Runtime.ProviderOptions) == 0 {
			t.Fatalf("profile %s declares no provider options", p.ID)
		}
		for _, o := range p.Runtime.ProviderOptions {
			effort, _ := o.Options["reasoning_effort"].(string)
			if effort == "" {
				t.Fatalf("profile %s option %s: no reasoning_effort declared", p.ID, o.ID)
			}
			found := false
			for _, allowed := range o.ReasoningEfforts {
				if allowed == effort {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("profile %s option %s: reasoning_effort %q is not one of its own declared reasoning_efforts %v", p.ID, o.ID, effort, o.ReasoningEfforts)
			}
		}
	}
}

// The specific defaults the family relies on: Claude Code runs both the
// parent and the child on Sonnet 5 only (no Fable 5.1 option) — the parent
// at high reasoning effort, the child at medium; Codex runs the parent on
// the steadier model at medium and the child on a newer one at high.
func TestSparkQuillDefaultModelsAndReasoningEfforts(t *testing.T) {
	profiles := BuiltinAgentProfiles()
	find := func(profileID, optionID string) (modelID, effort string) {
		for _, p := range profiles {
			if p.ID != profileID {
				continue
			}
			for _, o := range p.Runtime.ProviderOptions {
				if o.ID == optionID {
					e, _ := o.Options["reasoning_effort"].(string)
					return o.ModelID, e
				}
			}
		}
		t.Fatalf("no provider option %q on profile %q", optionID, profileID)
		return "", ""
	}
	cases := []struct {
		profileID, optionID, wantModel, wantEffort string
	}{
		{"sparkquill", "claude-code", "claude-sonnet-5", "high"},
		{"sparkquill", "codex-cli", "gpt-6-astra", "medium"},
		{"sparkquill-child", "claude-code", "claude-sonnet-5", "medium"},
		{"sparkquill-child", "codex-cli", "gpt-5.6-luna", "high"},
	}
	for _, c := range cases {
		gotModel, gotEffort := find(c.profileID, c.optionID)
		if gotModel != c.wantModel || gotEffort != c.wantEffort {
			t.Fatalf("%s/%s: model=%q effort=%q, want model=%q effort=%q", c.profileID, c.optionID, gotModel, gotEffort, c.wantModel, c.wantEffort)
		}
	}
}
