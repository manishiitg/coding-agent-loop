package step_based_workflow

import (
	"fmt"
	"sort"
	"strings"

	browserinstructions "mcp-agent-builder-go/agent_go/pkg/instructions"
	"github.com/manishiitg/mcpagent/mcpclient"
)

// BuildPlanningCapabilitiesContext builds a capabilities summary for the planning agent.
// Tells the planner which MCP servers, browser tools, skills, and secrets are available
// so it can design steps that leverage actual capabilities rather than generic approaches.
func BuildPlanningCapabilitiesContext(hcpo *StepBasedWorkflowOrchestrator) string {
	var parts []string

	// Browser tools first — most impactful for plan design
	if section := buildBrowserCapabilitiesSection(hcpo); section != "" {
		parts = append(parts, section)
	}

	// Google Workspace (gws) — special CLI-based access
	if section := buildGWSCapabilitiesSection(hcpo); section != "" {
		parts = append(parts, section)
	}

	// Other MCP servers (non-browser, non-gws)
	if section := buildMCPServerCapabilitiesSection(hcpo); section != "" {
		parts = append(parts, section)
	}

	// Skills
	if section := buildSkillsCapabilitiesSection(hcpo); section != "" {
		parts = append(parts, section)
	}

	// Secrets — names only, no values
	if section := buildSecretsCapabilitiesSection(hcpo); section != "" {
		parts = append(parts, section)
	}

	// Image generation
	if section := buildImageGenerationCapabilitiesSection(hcpo); section != "" {
		parts = append(parts, section)
	}

	if len(parts) == 0 {
		return ""
	}

	return "## 🧰 AVAILABLE CAPABILITIES\n\nDesign steps to leverage these capabilities. Execution agents will have access to all of the below.\n\n" +
		strings.Join(parts, "\n")
}

func buildBrowserCapabilitiesSection(hcpo *StepBasedWorkflowOrchestrator) string {
	selectedServers := hcpo.GetSelectedServers()
	selectedSkills := hcpo.GetSelectedSkills()

	hasPlaywright := false
	hasCamofox := false
	hasAgentBrowser := false

	for _, s := range selectedServers {
		switch s {
		case "playwright":
			hasPlaywright = true
		case "camofox":
			hasCamofox = true
		}
	}
	for _, sk := range selectedSkills {
		if sk == "agent-browser" {
			hasAgentBrowser = true
		}
	}

	if !hasPlaywright && !hasCamofox && !hasAgentBrowser {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("### 🌐 Browser Automation\n\n")

	if hasPlaywright {
		sb.WriteString("**Playwright** (MCP server — Chromium):\n")
		sb.WriteString("- Standard Chromium browser automation\n")
		sb.WriteString("- Key tools: `browser_navigate`, `browser_click`, `browser_type`, `browser_fill`, `browser_snapshot`, `browser_screenshot`, `browser_file_upload`, `browser_select_option`, `browser_scroll`, `browser_wait_for`\n")
		sb.WriteString("- Prefer `browser_snapshot` (accessibility tree) over `browser_screenshot` — far more token-efficient\n")
		sb.WriteString("- File uploads: use workspace-relative paths (e.g. `execution/Downloads/file.pdf`)\n")
		sb.WriteString("- Best for: general web scraping, form submission, UI interaction on standard sites\n\n")
	}

	if hasCamofox {
		sb.WriteString("**Camofox** (MCP server — Stealth Firefox):\n")
		sb.WriteString("- Anti-detect Firefox fork — bypasses bot detection systems\n")
		sb.WriteString("- Key tools: `snapshot`, `navigate`, `click`, `type_text`, `create_tab`, `refresh`, `camofox_evaluate_js`\n")
		sb.WriteString("- Session persistence: `save_profile` / `load_profile` — save login sessions between steps or runs\n")
		sb.WriteString("- Downloads: managed internally — use `list_downloads` + `get_download(includeContent=true)` to retrieve files, then save to workspace via shell\n")
		sb.WriteString("- Batch resource extraction: `batch_download(tabId, selector, types)` — bulk-downloads matching resources from a page\n")
		sb.WriteString("- Best for: sites with bot detection, session reuse across steps, bulk resource downloading\n\n")
	}

	if hasAgentBrowser {
		sb.WriteString("**agent-browser** (Skill — Headless CDP):\n")
		sb.WriteString(browserinstructions.GetAgentBrowserQuickStartInstructions())
		sb.WriteString("\n- Use a unique session name per sub-agent to isolate browser state when agents run in parallel\n\n")
	}

	return sb.String()
}

func buildGWSCapabilitiesSection(hcpo *StepBasedWorkflowOrchestrator) string {
	hasGWS := false
	for _, s := range hcpo.GetSelectedServers() {
		if s == "gws" {
			hasGWS = true
			break
		}
	}
	if !hasGWS {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("### 📧 Google Workspace (GWS)\n\n")
	sb.WriteString("**Access method**: `gws` CLI via `execute_shell_command` (NOT MCP tools)\n\n")
	sb.WriteString("**Available services**: Drive, Gmail, Calendar, Sheets, Docs, Slides\n\n")
	sb.WriteString(browserinstructions.GetGWSQuickStartInstructions())
	sb.WriteString("\n\n")
	return sb.String()
}

func buildMCPServerCapabilitiesSection(hcpo *StepBasedWorkflowOrchestrator) string {
	selectedServers := hcpo.GetSelectedServers()
	if len(selectedServers) == 0 {
		return ""
	}

	specialServers := map[string]bool{"playwright": true, "camofox": true, "gws": true}
	var otherServers []string
	for _, s := range selectedServers {
		if !specialServers[s] && s != mcpclient.AllServers && s != mcpclient.NoServers {
			otherServers = append(otherServers, s)
		}
	}
	if len(otherServers) == 0 {
		return ""
	}
	sort.Strings(otherServers)

	// Try to load config for descriptions
	serverDescriptions := map[string]string{}
	if mcpConfigPath := hcpo.GetMCPConfigPath(); mcpConfigPath != "" {
		if config, err := mcpclient.LoadMergedConfig(mcpConfigPath, nil); err == nil {
			for _, name := range otherServers {
				if cfg, ok := config.MCPServers[name]; ok && cfg.Description != "" {
					serverDescriptions[name] = cfg.Description
				}
			}
		}
	}

	var sb strings.Builder
	sb.WriteString("### 🔧 MCP Servers\n\n")
	sb.WriteString("The following MCP servers provide tools available to all execution agents:\n\n")
	for _, s := range otherServers {
		if desc, ok := serverDescriptions[s]; ok {
			sb.WriteString(fmt.Sprintf("- **%s**: %s\n", s, desc))
		} else {
			sb.WriteString(fmt.Sprintf("- **%s**\n", s))
		}
	}
	sb.WriteString("\n")
	return sb.String()
}

func buildSkillsCapabilitiesSection(hcpo *StepBasedWorkflowOrchestrator) string {
	selectedSkills := hcpo.GetSelectedSkills()

	// Filter out agent-browser — already covered in browser section
	var otherSkills []string
	for _, sk := range selectedSkills {
		if sk != "agent-browser" {
			otherSkills = append(otherSkills, sk)
		}
	}
	if len(otherSkills) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("### 🎯 Skills\n\n")
	sb.WriteString("Active skills provide reusable methodologies that execution agents must read and follow:\n\n")
	for _, sk := range otherSkills {
		sb.WriteString(fmt.Sprintf("- **%s** — instructions at `skills/%s/SKILL.md`\n", sk, sk))
	}
	sb.WriteString("\n")
	return sb.String()
}

func buildSecretsCapabilitiesSection(hcpo *StepBasedWorkflowOrchestrator) string {
	secrets := hcpo.GetSecrets()
	if len(secrets) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("### 🔐 Secrets\n\n")
	sb.WriteString("The following named credentials are configured and injected into execution agents. Reference them by name in step descriptions:\n\n")
	for _, s := range secrets {
		sb.WriteString(fmt.Sprintf("- **%s**\n", s.Name))
	}
	sb.WriteString("\n")
	return sb.String()
}

func buildImageGenerationCapabilitiesSection(hcpo *StepBasedWorkflowOrchestrator) string {
	hasImageGen := false
	for _, t := range hcpo.WorkspaceTools {
		if t.Function != nil && t.Function.Name == "workspace_image_gen" {
			hasImageGen = true
			break
		}
	}
	if !hasImageGen {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("### 🎨 Image Generation\n\n")
	sb.WriteString("**workspace_image_gen** — Generate images from text prompts:\n")
	sb.WriteString("- Tool: `workspace_image_gen(prompt, output_filename, [aspect_ratio])`\n")
	sb.WriteString("- Saves to `Chats/generated-images/<filename>`\n\n")
	sb.WriteString("**workspace_image_edit** — Edit images using text instructions:\n")
	sb.WriteString("- Tool: `workspace_image_edit(prompt, input_image_path, output_filename)`\n")
	sb.WriteString("- Input: workspace-relative path (e.g. `Chats/generated-images/img.png`)\n\n")
	return sb.String()
}
