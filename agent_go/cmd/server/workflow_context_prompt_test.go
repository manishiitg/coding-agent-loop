package server

import (
	"strings"
	"testing"
)

func TestWorkflowContextPromptUsesCompactReadOnlyReferences(t *testing.T) {
	prompt := buildWorkflowContextPrompt([]string{
		"Workflow/HDFC-Personal-Accounts",
		"Workflow/ICICI-BANK-PARSING-v2/",
		"Chats/Work/projects/company-ca-a1b2c3d4",
	}, "http://workspace.invalid")

	for _, want := range []string{
		"## Workflow Context (Read-Only)",
		"(workflow) `Workflow/HDFC-Personal-Accounts/`",
		"(workflow) `Workflow/ICICI-BANK-PARSING-v2/`",
		"(Crew) `Chats/Work/projects/company-ca-a1b2c3d4/`",
		"planning/plan.json",
		"Read only the files needed",
		"$WORKSPACE_DOCS_PATH/<listed-path>/...",
		"Do not list or probe the parent `Workflow/` directory",
		"failure to list that parent does not mean",
		"it is never a Slack channel",
		"never `#name`",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("compact workflow context missing %q:\n%s", want, prompt)
		}
	}
	if len(prompt) > 2600 {
		t.Fatalf("workflow references expanded to %d bytes; want a compact path-based prompt", len(prompt))
	}
}
