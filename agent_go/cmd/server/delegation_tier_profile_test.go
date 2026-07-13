package server

import (
	"testing"

	virtualtools "github.com/manishiitg/coding-agent-loop/agent_go/cmd/server/virtual-tools"
)

func TestResolveDelegationTierConfigExpandsProviderProfile(t *testing.T) {
	resolved := resolveDelegationTierConfig(&virtualtools.DelegationTierConfig{
		SchemaVersion: delegationTierConfigSchemaVersion,
		Mode:          "provider_profile",
		Provider:      "claude-code",
	})
	if resolved == nil {
		t.Fatal("resolveDelegationTierConfig() = nil")
	}
	if resolved.Main == nil || resolved.Main.ModelID != "claude-opus-4-8" {
		t.Fatalf("main = %+v, want claude-opus-4-8", resolved.Main)
	}
	if got := resolved.Main.Options["reasoning_effort"]; got != "high" {
		t.Fatalf("main reasoning_effort = %#v, want high", got)
	}
	if resolved.ChiefOfStaff == nil || resolved.ChiefOfStaff.ModelID != "claude-opus-4-8" {
		t.Fatalf("chief_of_staff = %+v, want claude-opus-4-8", resolved.ChiefOfStaff)
	}
	if resolved.High == nil || resolved.High.ModelID != "claude-opus-4-8" {
		t.Fatalf("high = %+v, want claude-opus-4-8", resolved.High)
	}
	if got := resolved.High.Options["reasoning_effort"]; got != "high" {
		t.Fatalf("high reasoning_effort = %#v, want high", got)
	}
}

func TestResolveDelegationTierConfigPreservesExplicitOptions(t *testing.T) {
	resolved := resolveDelegationTierConfig(&virtualtools.DelegationTierConfig{
		SchemaVersion: delegationTierConfigSchemaVersion,
		Mode:          "explicit",
		Main: &virtualtools.TierModel{
			Provider: "codex-cli",
			ModelID:  "gpt-5.5",
			Options:  map[string]interface{}{"reasoning_effort": "xhigh"},
		},
	})
	if resolved == nil || resolved.Main == nil {
		t.Fatalf("resolveDelegationTierConfig() = %+v", resolved)
	}
	if got := resolved.Main.Options["reasoning_effort"]; got != "xhigh" {
		t.Fatalf("main reasoning_effort = %#v, want xhigh", got)
	}
}
