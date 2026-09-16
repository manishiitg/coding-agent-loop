package agentworksproduct

import (
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/agentprofiles"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/uiuxpromax"
)

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

// RegisterProductSkills registers shared optional expertise that is selected by
// chat.<mode>.skills. Builder/Run core bundles are still materialized per
// session with capability and mode filtering in the workflow-phase setup path.
func RegisterProductSkills() error {
	return uiuxpromax.Register()
}
