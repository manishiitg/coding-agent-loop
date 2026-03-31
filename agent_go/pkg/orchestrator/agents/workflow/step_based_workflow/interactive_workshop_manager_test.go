package step_based_workflow

import (
	"strings"
	"testing"
)

func TestInteractiveWorkshopPromptPrefersToolSearchForBrowserHeavySteps(t *testing.T) {
	var prompt strings.Builder
	err := interactiveWorkshopSystemTemplate.Execute(&prompt, map[string]any{
		"WorkshopMode":          "builder",
		"WorkspacePath":         "Workflow/confida-oi",
		"AbsWorkspacePath":      "/app/workspace-docs/Workflow/confida-oi",
		"RunFolder":             "iteration-0/default",
		"WorkflowObjective":     "",
		"WorkflowSuccessCriteria": "",
		"StepConfigSummary":     "",
		"ProgressSummary":       "",
		"StepSummary":           "",
		"PlanJSON":              "",
		"ExecutionMode":         "",
		"UseKnowledgebase":      "true",
		"IsCodeExecutionMode":   "false",
		"AvailableGroups":       "",
		"AbsDocsRoot":           "/app/workspace-docs",
		"UnoptimizedSteps":      "",
	})
	if err != nil {
		t.Fatalf("execute interactive workshop template: %v", err)
	}

	requiredSnippets := []string{
		"2. **Tool search mode**: exact tools aren't known upfront, or the step is browser-heavy and likely to require many tool calls or repeated page-state inspection before deciding the next action → update_step_config(step_id, use_tool_search_mode=true)",
		"When in doubt: if the step is browser-heavy, depends on interactive page inspection, or will likely take many tool calls, start with tool_search.",
	}
	for _, snippet := range requiredSnippets {
		if !strings.Contains(prompt.String(), snippet) {
			t.Fatalf("expected prompt to contain %q\n\nPrompt:\n%s", snippet, prompt.String())
		}
	}
}
