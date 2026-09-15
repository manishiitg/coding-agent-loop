package server

import (
	"strings"
	"testing"
)

func TestWorkflowContextPromptUsesCompactReadOnlyReferences(t *testing.T) {
	prompt := buildWorkflowContextPrompt([]string{
		"Workflow/HDFC-Personal-Accounts",
		"Workflow/ICICI-BANK-PARSING-v2/",
	}, "http://workspace.invalid")

	for _, want := range []string{
		"## Workflow Context (Read-Only)",
		"`Workflow/HDFC-Personal-Accounts/`",
		"`Workflow/ICICI-BANK-PARSING-v2/`",
		"planning/plan.json",
		"Read only the files needed",
		"$WORKSPACE_DOCS_PATH/<listed-path>/...",
		"Do not list or probe the parent `Workflow/` directory",
		"failure to list that parent does not mean",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("compact workflow context missing %q:\n%s", want, prompt)
		}
	}
	if len(prompt) > 2200 {
		t.Fatalf("workflow references expanded to %d bytes; want a compact path-based prompt", len(prompt))
	}
}
