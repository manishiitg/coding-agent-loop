package agentworksproduct

import (
	"strings"
	"testing"
)

// The builder slash commands moved from hardcoded frontend builtins into
// product.yaml. This locks the manifest contract the workflow surface menu
// renders from: every command resolves a non-empty prompt carrying the
// {{context}} placeholder, kind-backed commands name their guidance kind,
// and the retained pulse-review focus shortcuts stay executable but hidden.
func TestAgentWorksCommandsResolvePrompts(t *testing.T) {
	manifest, err := AgentWorksManifest()
	if err != nil {
		t.Fatalf("AgentWorksManifest: %v", err)
	}
	byName := map[string]string{}
	hidden := map[string]bool{}
	aliases := map[string][]string{}
	for _, cmd := range manifest.Profile.Commands {
		if cmd.Name == "" {
			t.Fatal("command with empty name")
		}
		if strings.TrimSpace(cmd.Prompt) == "" {
			t.Fatalf("command %q resolved an empty prompt", cmd.Name)
		}
		if !strings.Contains(cmd.Prompt, "{{context}}") {
			t.Fatalf("command %q prompt does not carry the {{context}} placeholder", cmd.Name)
		}
		byName[cmd.Name] = cmd.Prompt
		hidden[cmd.Name] = cmd.MenuHidden
		aliases[cmd.Name] = cmd.Aliases
	}
	for name, wantKind := range map[string]string{
		"design-plan":           `kind="design-plan"`,
		"review-artifact-drift": `kind="review-artifact-drift"`,
		"design-dashboard":      `kind="design-reporting-ui"`,
		"setup-goals":           `kind="setup-goals"`,
		"strategy-auditor":      `kind="strategy-auditor"`,
		"pulse-review":          `kind="engineering-review"`,
		"pulse-fixer":           `kind="pulse-fixer"`,
		"review-code":           `kind="design-plan"`,
	} {
		prompt, ok := byName[name]
		if !ok {
			t.Fatalf("command %q missing from product.yaml", name)
		}
		if !strings.Contains(prompt, wantKind) {
			t.Fatalf("command %q prompt does not name its guidance kind %s", name, wantKind)
		}
	}
	for _, name := range []string{"pulse-merge", "backup", "publish", "notify"} {
		if _, ok := byName[name]; !ok {
			t.Fatalf("command %q missing from product.yaml", name)
		}
	}
	// /pulse stays a hardcoded builtin: it is an async frontend API call no
	// static prompt can express.
	if _, ok := byName["pulse"]; ok {
		t.Fatal("command pulse must stay a hardcoded builtin, not a yaml prompt")
	}
	for name, wantAliases := range map[string][]string{
		"design-dashboard": {"design-reporting-ui"},
		"setup-goals":      {"define-success"},
		"strategy-auditor": {"goal-advisor"},
	} {
		got := aliases[name]
		if len(got) != len(wantAliases) || (len(got) > 0 && got[0] != wantAliases[0]) {
			t.Fatalf("command %q aliases = %v, want %v", name, got, wantAliases)
		}
	}
	legacy := []string{
		"pulse-review-execution-health",
		"pulse-review-validation-contract",
		"pulse-review-report-quality",
		"pulse-review-evaluation-quality",
		"pulse-review-database",
		"pulse-review-knowledge",
		"pulse-review-learnings",
	}
	for _, name := range legacy {
		prompt, ok := byName[name]
		if !ok {
			t.Fatalf("retained shortcut %q missing from product.yaml", name)
		}
		if !hidden[name] {
			t.Fatalf("retained shortcut %q must stay menu-hidden", name)
		}
		if prompt != byName["pulse-review"] {
			t.Fatalf("retained shortcut %q must share the pulse-review prompt", name)
		}
	}
	if len(byName) != 12+len(legacy) {
		t.Fatalf("product.yaml carries %d commands, want %d", len(byName), 12+len(legacy))
	}
}
