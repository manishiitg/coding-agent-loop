package main

import (
	"github.com/manishiitg/mcpagent/llm"
	llmproviders "github.com/manishiitg/multi-llm-provider-go"
)

// mediumTierModelID resolves the coding-agent model ID for a provider from
// multi-llm-provider-go's shared tier defaults (e.g. "claude-sonnet-5" for
// Claude Code), instead of leaving ModelID empty — which silently defers to
// whatever model the user's own coding-agent CLI happens to be set to via its
// own /model command, an ambient setting unrelated to this app. Falls back to
// "" (agentsession's own default) if the provider has no published tier
// defaults.
//
// Normally this is the "medium" tier. Cursor CLI is a deliberate exception:
// its medium tier defaults to composer-2.5, but this app wants Cursor's high
// tier (grok-4.5) instead — composer-2.5 wasn't strong enough for family
// tutoring use, so we pin the stronger model for Cursor specifically.
func mediumTierModelID(provider llm.Provider) string {
	tiers, ok := llmproviders.GetCodingAgentDefaultTierModels(llmproviders.Provider(provider))
	if !ok {
		return ""
	}
	if llmproviders.Provider(provider) == llmproviders.ProviderCursorCLI {
		return tiers.High.ModelID
	}
	return tiers.Medium.ModelID
}
