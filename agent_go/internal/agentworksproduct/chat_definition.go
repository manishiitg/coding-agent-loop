package agentworksproduct

import (
	"crypto/sha256"
	"fmt"
	"io/fs"
	"strings"

	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/agentprofiles"
)

func validateChatDefinitions(fsys fs.FS, m ProductManifest) error {
	if len(m.Chat) != 2 {
		return fmt.Errorf("AgentWorks chat must declare builder and run")
	}
	for _, mode := range []string{"builder", "run"} {
		def, ok := m.Chat[mode]
		if !ok {
			return fmt.Errorf("missing AgentWorks chat %q", mode)
		}
		if _, err := agentprofiles.LoadChatPrompt(fsys, def.Prompt); err != nil {
			return fmt.Errorf("chat %s: %w", mode, err)
		}
		known := map[string]bool{"system-tools": true, "builder-reference": true, "workflow-commands": mode == "builder", "ui-ux-pro-max": mode == "builder"}
		seen := map[string]bool{}
		for _, name := range def.Skills {
			if !known[name] || seen[name] {
				return fmt.Errorf("chat %s: unknown or duplicate skill %q", mode, name)
			}
			seen[name] = true
		}
		if len(seen) == 0 {
			return fmt.Errorf("chat %s: skills are required", mode)
		}
		// The external CLI/MCP API is run-mode access: only the run mode
		// admits external tools, and it must admit at least one.
		if mode == "run" {
			if len(def.ExternalTools) == 0 {
				return fmt.Errorf("chat run: external_tools are required")
			}
		} else if len(def.ExternalTools) != 0 {
			return fmt.Errorf("chat %s: external_tools belong on the run mode only", mode)
		}
	}
	return nil
}

// RunExternalTools returns the external CLI/MCP tool admission list declared
// by the run chat mode. This is the single source of truth for the external
// API surface: the server exposes exactly these tools.
func RunExternalTools() []string {
	def, ok := mustAgentWorksManifest().Chat["run"]
	if !ok {
		panic(fmt.Errorf("unknown AgentWorks chat mode %q", "run"))
	}
	return append([]string(nil), def.ExternalTools...)
}

// ChatPromptTemplate returns trusted template source; callers inject runtime
// context with their existing strict template renderer.
func ChatPromptTemplate(mode string) string {
	def, ok := mustAgentWorksManifest().Chat[mode]
	if !ok {
		panic(fmt.Errorf("unknown AgentWorks chat mode %q", mode))
	}
	text, err := agentprofiles.LoadChatPrompt(productConfigFiles, def.Prompt)
	if err != nil {
		panic(err)
	}
	return text
}

// ChatSkills returns a copy of the configured core bundles. Workflow-selected
// skills remain additive and use the existing workflow skill loader.
func ChatSkills(mode string) []string {
	def, ok := mustAgentWorksManifest().Chat[mode]
	if !ok {
		panic(fmt.Errorf("unknown AgentWorks chat mode %q", mode))
	}
	return append([]string(nil), def.Skills...)
}

// ChatDefinitionKey refreshes retained coding-CLI instructions when the selected
// prompt (including shared files) or skill list changes across a deployment.
func ChatDefinitionKey(mode string) string {
	sum := sha256.Sum256([]byte(ChatPromptTemplate(mode) + "\x00" + strings.Join(ChatSkills(mode), "\x00")))
	return fmt.Sprintf("%x", sum[:])
}
