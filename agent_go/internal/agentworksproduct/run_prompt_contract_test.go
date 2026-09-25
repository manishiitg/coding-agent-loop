package agentworksproduct

import (
	"strings"
	"testing"
)

// Run mode must be told to consult what the workflow knows and how to turn a
// request into a run (RTS 2026-09-25 review: learnings/KB were optional,
// route matching and per-run values were not described).
func TestRunPromptCoversKnowledgeAndRouteSelection(t *testing.T) {
	raw, err := productConfigFiles.ReadFile("prompts/run.md")
	if err != nil {
		t.Fatal(err)
	}
	prompt := string(raw)
	for _, want := range []string{
		"learnings/_global/SKILL.md",
		"learnings/<step-id>/",
		"Attached knowledge bases",
		"route_selections",
		"run_full_workflow` `variables`",
		"ask for exactly that value",
		"your reply is the answer",
		"Do not answer with \"see report.md\"",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("run.md no longer says %q", want)
		}
	}
}
