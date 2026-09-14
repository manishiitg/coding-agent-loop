package workproduct

import (
	"sync"

	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/agentprofiles"
)

// BuiltinAgentProfile returns the work profile. Brand new, so only one
// version is ever registered -- same reasoning as every other product's
// own BuiltinAgentProfile.
func BuiltinAgentProfile() agentprofiles.Profile {
	manifest := mustWorkManifest()
	profile := manifest.Profile
	profile.SystemPromptTemplate = renderProductPrompt()
	return profile
}

func BuiltinAgentProfiles() []agentprofiles.Profile {
	return []agentprofiles.Profile{BuiltinAgentProfile()}
}

var registerProductSkillsOnce sync.Once
var registerProductSkillsErr error

var productSkills = []agentprofiles.SkillFileBinding{
	{Name: "work-integrations", Description: "Connect and manage Work MCP servers, secrets, browser access, models, and administrator-authorized server folders.", Path: "skills/work-integrations/SKILL.md"},
	{Name: "work-skills", Description: "Discover, install, import, create, select, and remove reusable skills in Work.", Path: "skills/work-skills/SKILL.md"},
	{Name: "work-schedules-and-bots", Description: "Manage Work's message-only schedules, authenticated webhook triggers, and Slack or WhatsApp project-chat bots.", Path: "skills/work-schedules-and-bots/SKILL.md"},
	{Name: "work-dashboard", Description: "Create and maintain a general-purpose visual dashboard for a Work project.", Path: "skills/work-dashboard/SKILL.md"},
	{Name: "background-work", Description: "Run a bounded task asynchronously and rely on Work's automatic completion notification instead of polling.", Path: "skills/background-work/SKILL.md"},
}

// RegisterProductSkills adds Work's platform-operation contracts. General
// browser, coding, review, and skill-authoring skills still come from the
// existing AgentWorks registry.
func RegisterProductSkills() error {
	registerProductSkillsOnce.Do(func() {
		registerProductSkillsErr = agentprofiles.RegisterEmbeddedSkills(productConfigFiles, productSkills)
	})
	return registerProductSkillsErr
}

// Background execution is registered by the server because it reuses the
// server-owned background-agent registry; Work has no separate ToolFactory.
func RegisterAgentProfileRuntime(_ *agentprofiles.Registry, _ string) error {
	return nil
}
