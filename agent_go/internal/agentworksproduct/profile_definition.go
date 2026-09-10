package agentworksproduct

import "github.com/manishiitg/coding-agent-loop/agent_go/pkg/agentprofiles"

// BuiltinAgentProfile returns the agentworks profile. Brand new, so only
// one version is ever registered -- same reasoning as every other product's
// own BuiltinAgentProfile.
func BuiltinAgentProfile() agentprofiles.Profile {
	manifest := mustAgentWorksManifest()
	profile := manifest.Profile
	profile.SystemPromptTemplate = renderProductPrompt()
	return profile
}

func BuiltinAgentProfiles() []agentprofiles.Profile {
	return []agentprofiles.Profile{BuiltinAgentProfile()}
}

// RegisterProductSkills is a no-op today -- this profile declares no
// skills or bespoke tools in product.yaml. Kept as a real function, not
// omitted, so server.go's registration call shape matches every other
// product and adding a skill later needs no server.go change.
func RegisterProductSkills() error {
	return nil
}
