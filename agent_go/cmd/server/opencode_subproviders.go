package server

import (
	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
	"github.com/manishiitg/multi-llm-provider-go/pkg/adapters/opencodecli"
)

// openCodeSubProviderEnvVarForID returns the env-var name that the
// sub-provider tile expects to authenticate with, or "" when the id is
// unknown or the sub-provider does not require a credential.
func openCodeSubProviderEnvVarForID(id string) string {
	if sp, ok := opencodecli.FindOpenCodeSubProvider(id); ok {
		return sp.APIKeyEnvVar
	}
	return ""
}

// openCodeSubProviderEnvVarsImpl returns every env-var name used by the
// OpenCode CLI sub-provider catalog. Lives in its own file (rather than
// inline in provider_keys_store.go) so that file stays free of the
// opencodecli import.
func openCodeSubProviderEnvVarsImpl() []string {
	sps := opencodecli.OpenCodeSubProviders()
	seen := make(map[string]struct{}, len(sps))
	out := make([]string, 0, len(sps))
	for _, sp := range sps {
		if sp.APIKeyEnvVar == "" {
			continue
		}
		if _, ok := seen[sp.APIKeyEnvVar]; ok {
			continue
		}
		seen[sp.APIKeyEnvVar] = struct{}{}
		out = append(out, sp.APIKeyEnvVar)
	}
	return out
}

// openCodeSubProviderManifestEntries returns one providerManifestEntry per
// OpenCode sub-provider tile. The frontend renders each as a top-level
// LLM-configuration tab; the routing layer ties them back to the OpenCode
// CLI adapter at runtime. integration_kind="coding_agent" because the
// underlying transport is still the OpenCode CLI subprocess.
func openCodeSubProviderManifestEntries(runtimeAvailable bool, mergedSubKeys map[string]string) []providerManifestEntry {
	sps := opencodecli.OpenCodeSubProviders()
	entries := make([]providerManifestEntry, 0, len(sps))
	for _, sp := range sps {
		models := opencodeSubProviderModelMetadata(sp)
		defaultModelID := sp.DefaultModelID
		// Frontend selectors look at ModelMetadata.ModelID, so default
		// must match an entry there. Fall back to first model if needed.
		if defaultModelID != "" && !containsModelID(models, defaultModelID) && len(models) > 0 {
			defaultModelID = models[0].ModelID
		} else if defaultModelID == "" && len(models) > 0 {
			defaultModelID = models[0].ModelID
		}

		authConfigured := !sp.RequiresAPIKey
		authSource := ""
		if sp.RequiresAPIKey && sp.APIKeyEnvVar != "" {
			if _, ok := mergedSubKeys[sp.APIKeyEnvVar]; ok {
				authConfigured = true
				authSource = "workspace_or_env"
			}
		}

		setupHint := ""
		if !runtimeAvailable {
			setupHint = "Install OpenCode CLI: `npm install -g opencode-ai`."
		} else if sp.RequiresAPIKey && !authConfigured {
			setupHint = "Enter a " + sp.DisplayName + " API key in this tab to enable models."
		}

		runtimeAvailableLocal := runtimeAvailable

		entries = append(entries, providerManifestEntry{
			ID:                    sp.ID,
			DisplayName:           sp.DisplayName,
			Description:           sp.Description,
			Kind:                  "local_cli",
			IntegrationKind:       "coding_agent",
			ModelSelectionMode:    "fixed_tier",
			AuthDescription:       authDescriptionForSubProvider(sp),
			RuntimeCommand:        "opencode",
			RuntimeAvailable:      &runtimeAvailableLocal,
			AuthConfigured:        authConfigured,
			AuthSource:            authSource,
			Usable:                runtimeAvailable && authConfigured,
			SetupHint:             setupHint,
			RequiresAPIKey:        sp.RequiresAPIKey,
			SupportsDynamicModels: false,
			DefaultModelID:        defaultModelID,
			Models:                models,
			Capabilities:          []string{"chat", "read_image"},
			APIKeyEnv:             sp.APIKeyEnvVar,
			APIKeyURL:             sp.APIKeyURL,
		})
	}
	return entries
}

func authDescriptionForSubProvider(sp opencodecli.OpenCodeSubProvider) string {
	if !sp.RequiresAPIKey {
		return "No API key required (rate-limited free tier)"
	}
	return sp.APIKeyEnvVar + " required"
}

// opencodeSubProviderModelMetadata converts a sub-provider's curated model
// list into the shared ModelMetadata shape the frontend already understands.
// ModelID is the bare id (without the OpenCode `<provider>/...` prefix) so
// the chat UI can pass it back unchanged; the adapter prepends the prefix
// at launch time.
func opencodeSubProviderModelMetadata(sp opencodecli.OpenCodeSubProvider) []*llmtypes.ModelMetadata {
	out := make([]*llmtypes.ModelMetadata, 0, len(sp.Models))
	for _, m := range sp.Models {
		out = append(out, &llmtypes.ModelMetadata{
			Provider:                sp.ID,
			ModelID:                 m.ID,
			ModelName:               m.DisplayName,
			ContextWindow:           m.ContextWindow,
			InputCostPer1MTokens:    m.CostInput,
			OutputCostPer1MTokens:   m.CostOutput,
			SupportsReasoningEffort: m.SupportsReasoning,
			ModelSelectionMode:      "fixed_tier",
		})
	}
	return out
}

func containsModelID(models []*llmtypes.ModelMetadata, id string) bool {
	for _, m := range models {
		if m != nil && m.ModelID == id {
			return true
		}
	}
	return false
}
