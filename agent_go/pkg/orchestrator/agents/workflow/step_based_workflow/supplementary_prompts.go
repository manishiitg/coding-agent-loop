package step_based_workflow

import (
	"context"
	"fmt"

	browserinstructions "mcp-agent-builder-go/agent_go/pkg/instructions"

	mcpagent "github.com/manishiitg/mcpagent/agent"
	"mcp-agent-builder-go/agent_go/pkg/orchestrator/agents"
)

// appendSupplementaryPrompts injects skills, secrets, browser isolation,
// and browser instructions into the agent's system prompt.
// This is the standard post-setup injection used by all workflow agent types
// (execution, todo task orchestrator, conditional) to ensure consistent prompts.
func (hcpo *StepBasedWorkflowOrchestrator) appendSupplementaryPrompts(
	ctx context.Context,
	mcpAgent *mcpagent.Agent,
	config *agents.OrchestratorAgentConfig,
	effectiveSkills []string,
	isolatedSessionID string,
) {
	// 1. Skills
	if len(effectiveSkills) > 0 {
		skillPrompt := BuildWorkflowSkillPrompt(ctx, effectiveSkills, hcpo.BaseOrchestrator)
		if skillPrompt != "" {
			mcpAgent.AppendSystemPrompt(skillPrompt)
			hcpo.GetLogger().Info(fmt.Sprintf("🎯 Added skill prompt to agent (%d skills): %v", len(effectiveSkills), effectiveSkills))
		}
	}

	// 2. Browser isolation (agent-browser session override)
	if isolatedSessionID != "" {
		for _, skill := range effectiveSkills {
			if skill == "agent-browser" {
				mcpAgent.AppendSystemPrompt(fmt.Sprintf(
					"## Browser Isolation\nYou have an isolated browser session. When using the agent_browser tool, use session name %q instead of \"default\" to avoid sharing browser state with other agents.",
					isolatedSessionID,
				))
				hcpo.GetLogger().Info("Added browser isolation guidance to agent system prompt for agent-browser")
				break
			}
		}
	}

	// 3. Secrets
	effectiveSecrets := GetEffectiveSecrets(hcpo.BaseOrchestrator)
	if len(effectiveSecrets) > 0 {
		secretPrompt := BuildWorkflowSecretPrompt(effectiveSecrets)
		if secretPrompt != "" {
			mcpAgent.AppendSystemPrompt(secretPrompt)
			hcpo.GetLogger().Info(fmt.Sprintf("🔐 Added secret prompt to agent (%d secrets)", len(effectiveSecrets)))
		}
	}

	// 4. Browser instructions (mode-specific)
	browserCfg := hcpo.resolveBrowserConfig(config.ServerNames, effectiveSkills)
	if browserPrompt := browserinstructions.BuildBrowserInstructions(browserCfg); browserPrompt != "" {
		mcpAgent.AppendSystemPrompt(browserPrompt)
		hcpo.GetLogger().Info(fmt.Sprintf("🌐 Added browser instructions to agent (playwright=%v, camofox=%v, agent-browser=%v, cdp=%v)",
			browserCfg.HasPlaywright, browserCfg.HasCamofox, browserCfg.HasAgentBrowser, browserCfg.CdpPort > 0))
	}

	// 5. GWS instructions (if gws server is enabled)
	for _, s := range config.ServerNames {
		if s == "gws" {
			mcpAgent.AppendSystemPrompt(browserinstructions.GetGWSQuickStartInstructions())
			hcpo.GetLogger().Info("📧 Added GWS quick-start instructions to agent")
			break
		}
	}
}

// resolveBrowserConfig detects browser capabilities from server names and skills.
func (hcpo *StepBasedWorkflowOrchestrator) resolveBrowserConfig(serverNames []string, skills []string) browserinstructions.BrowserConfig {
	cfg := browserinstructions.BrowserConfig{
		CdpPort: hcpo.GetCdpPort(),
	}
	for _, s := range serverNames {
		switch s {
		case "playwright":
			cfg.HasPlaywright = true
		case "camofox":
			cfg.HasCamofox = true
		}
	}
	for _, skill := range skills {
		if skill == "agent-browser" {
			cfg.HasAgentBrowser = true
			break
		}
	}
	return cfg
}
