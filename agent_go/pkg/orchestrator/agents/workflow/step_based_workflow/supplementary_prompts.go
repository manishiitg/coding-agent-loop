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
		skillPrompt := BuildWorkflowSkillPrompt(ctx, effectiveSkills, hcpo.BaseOrchestrator, GetPromptDocsRoot())
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
	browserCfg.IsIsolated = isolatedSessionID != ""
	if browserPrompt := browserinstructions.BuildBrowserInstructions(browserCfg); browserPrompt != "" {
		mcpAgent.AppendSystemPrompt(browserPrompt)
		hcpo.GetLogger().Info(fmt.Sprintf("🌐 Added browser instructions to agent (playwright=%v, agent-browser=%v, cdp=%v)",
			browserCfg.HasPlaywright, browserCfg.HasAgentBrowser, browserCfg.CdpPort > 0))
	}

	// 4b. Workflow-specific browser downloads guidance.
	// Generic browser instructions mention logical Downloads/ for normal chat uploads, but
	// workflow runs must stay inside their run-scoped execution/Downloads folder.
	if browserDownloadsPath := hcpo.GetBrowserDownloadsPath(); browserDownloadsPath != "" {
		mcpAgent.AppendSystemPrompt(fmt.Sprintf(
			"## Workflow Browser Downloads\nFor this workflow run, only use the run-scoped downloads folder %q for browser downloads and file cleanup. Do not read from, write to, or delete files under the root workspace Downloads/ folder.",
			browserDownloadsPath,
		))
		hcpo.GetLogger().Info(fmt.Sprintf("🌐 Added workflow browser downloads guidance to agent: %s", browserDownloadsPath))
	}

}

// resolveBrowserConfig resolves the browser configuration for prompt instructions.
// Uses orchestrator-level browserMode as primary, falls back to auto-detection from servers/skills.
func (hcpo *StepBasedWorkflowOrchestrator) resolveBrowserConfig(serverNames []string, skills []string) browserinstructions.BrowserConfig {
	cfg := browserinstructions.BrowserConfig{
		CdpPort: hcpo.GetCdpPort(),
	}

	// Detect browser capabilities from server names and skills
	for _, s := range serverNames {
		switch s {
		case "playwright":
			cfg.HasPlaywright = true
		case "workspace_browser":
			cfg.HasAgentBrowser = true
		}
	}
	for _, skill := range skills {
		if skill == "agent-browser" {
			cfg.HasAgentBrowser = true
			break
		}
	}

	// Resolve mode: explicit setting > auto-detect from capabilities
	if mode := hcpo.GetBrowserMode(); mode != "" {
		cfg.Mode = mode
	} else if cfg.HasPlaywright {
		cfg.Mode = "playwright"
	} else if cfg.CdpPort > 0 {
		cfg.Mode = "cdp"
	} else if cfg.HasAgentBrowser {
		cfg.Mode = "headless"
	}

	// Safety net: if a dedicated browser MCP server is present, force its mode and
	// suppress agent_browser to prevent LLM tool confusion.
	if cfg.HasPlaywright {
		cfg.Mode = "playwright"
		cfg.HasAgentBrowser = false
	}

	return cfg
}
