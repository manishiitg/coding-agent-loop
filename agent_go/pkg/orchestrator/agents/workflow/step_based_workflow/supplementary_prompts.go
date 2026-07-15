package step_based_workflow

import (
	"context"
	"fmt"

	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/common"
	browserinstructions "github.com/manishiitg/coding-agent-loop/agent_go/pkg/instructions"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/skills"

	mcpagent "github.com/manishiitg/mcpagent/agent"

	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/orchestrator/agents"
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
	// 1. Skills — Phase 3 rewire. Load the step's selected skills as
	// first-class llmtypes.Skill values and attach to the agent.
	// mcpagent.ensureSystemPrompt injects the listing into the system
	// prompt; CLI adapters additionally project SKILL.md folders to
	// disk via the SkillProjector contract. No more manual
	// BuildWorkflowSkillPrompt + AppendSystemPrompt.
	if len(effectiveSkills) > 0 {
		if attached := skills.LoadAttachable(getWorkspaceAPIURL(), effectiveSkills); len(attached) > 0 {
			for _, s := range attached {
				mcpAgent.AttachSkill(s)
			}
			hcpo.GetLogger().Info(fmt.Sprintf("🎯 Attached %d step skill(s) to agent: %v", len(attached), effectiveSkills))
		}
	}

	// 1b. Workflow global skill (Phase 4): attach a small pointer
	// skill telling the agent to read learnings/_global/ in the
	// workflow folder when it needs the workflow's accumulated
	// know-how. We attach a pointer (not the full body + references/)
	// so the workspace files stay the single source of truth and the
	// projected skill doesn't drift from what the workflow has
	// learned mid-session.
	//
	// Same helper the workshop chat uses (server.go workshop-phase
	// setup) — both paths land the identical pointer skill.
	if globalSkill := skills.LoadGlobalSkill(getWorkspaceAPIURL(), hcpo.GetWorkspacePath()); globalSkill != nil {
		mcpAgent.AttachSkill(globalSkill)
		hcpo.GetLogger().Info("🌐 Attached workflow global skill pointer (_global → learnings/_global/)")
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
		hcpo.GetLogger().Info(fmt.Sprintf("🌐 Added browser instructions to agent (agent-browser=%v, cdp=%v)",
			browserCfg.HasAgentBrowser, browserCfg.CdpPort > 0))
	}

	// 4b. Workflow-specific browser downloads guidance.
	// Generic browser instructions mention logical Downloads/ for normal chat uploads, but
	// workflow runs must stay inside their run-scoped execution/Downloads folder.
	if browserDownloadsPath := hcpo.GetBrowserDownloadsPath(); browserDownloadsPath != "" {
		downloadsPrompt := fmt.Sprintf(
			"## Workflow Browser Downloads\nFor this workflow run, use the run-scoped downloads folder %q for browser downloads and file cleanup. Do not read from, write to, or delete files under the root workspace Downloads/ folder.",
			browserDownloadsPath,
		)
		if hostDownloads := common.CDPHostDownloadsReadPath(browserCfg.Mode); hostDownloads != "" {
			downloadsPrompt += fmt.Sprintf(" In CDP mode, Chrome-native downloads can land in the host Downloads folder %q. That host folder is read-only: copy needed files into %q first, then process the workspace copy. Never write, move, or delete files under the host Downloads folder.", hostDownloads, browserDownloadsPath)
		}
		mcpAgent.AppendSystemPrompt(downloadsPrompt)
		hcpo.GetLogger().Info(fmt.Sprintf("🌐 Added workflow browser downloads guidance to agent: %s", browserDownloadsPath))
	}

}

// resolveBrowserConfig resolves the browser configuration for prompt instructions.
// Uses orchestrator-level browserMode as primary, falls back to auto-detection from servers/skills.
func (hcpo *StepBasedWorkflowOrchestrator) resolveBrowserConfig(serverNames []string, skills []string) browserinstructions.BrowserConfig {
	cfg := browserinstructions.BrowserConfig{
		CdpPort:  hcpo.GetCdpPort(),
		CdpPorts: hcpo.GetCdpPorts(),
	}

	// Detect browser capabilities from server names and skills
	for _, s := range serverNames {
		switch s {
		case "workspace_browser":
			cfg.HasAgentBrowser = true
		}
	}
	for _, skill := range skills {
		switch skill {
		case "agent-browser":
			cfg.HasAgentBrowser = true
		}
	}

	// Resolve mode: explicit setting > auto-detect from capabilities
	if mode := hcpo.GetBrowserMode(); mode != "" {
		cfg.Mode = mode
	} else if cfg.CdpPort > 0 || len(cfg.CdpPorts) > 0 {
		cfg.Mode = "cdp"
	} else if cfg.HasAgentBrowser {
		cfg.Mode = "headless"
	}

	return cfg
}
