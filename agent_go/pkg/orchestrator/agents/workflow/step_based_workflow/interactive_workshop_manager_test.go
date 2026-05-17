package step_based_workflow

import (
	"strings"
	"testing"
)

func executeInteractiveWorkshopPromptForMode(t *testing.T, mode string) string {
	t.Helper()
	prompt, err := ExecuteTemplate("interactiveWorkshopSystem", map[string]string{
		"AbsDocsRoot":                       "/app/workspace-docs",
		"AbsWorkspacePath":                  "/app/workspace-docs/Workflow/example",
		"AvailableGroups":                   "group-1",
		"BrowserPrompt":                     "",
		"Focus":                             "",
		"GroupName":                         "",
		"Instruction":                       "",
		"IsCodeExecutionMode":               "false",
		"MainPyAuthoringRules":              "",
		"Mode":                              "",
		"PlanJSON":                          "{}",
		"ProgressSummary":                   "",
		"RunFolder":                         "",
		"SecretPrompt":                      "",
		"SkillPrompt":                       "",
		"SpecialWorkspaceToolsInstructions": "",
		"StepConfigSummary":                 "",
		"StepID":                            "",
		"StepSummary":                       "",
		"StepsToReview":                     "",
		"TargetRunFolder":                   "",
		"UseKnowledgebase":                  "false",
		"UserRequest":                       "",
		"WorkflowObjective":                 "Build a reliable workflow.",
		"WorkflowSuccessCriteria":           "It runs end to end.",
		"WorkshopMode":                      mode,
		"WorkspacePath":                     "Workflow/example",
	})
	if err != nil {
		t.Fatalf("ExecuteTemplate returned error: %v", err)
	}
	return prompt
}

func TestInteractiveWorkshopPromptDocumentsMessageSequenceRouteReuse(t *testing.T) {
	prompt := executeInteractiveWorkshopPromptForMode(t, "builder")

	requiredSnippets := []string{
		"Route sub-agents can be `regular` for stateless one-off work, `message_sequence` for a stateful specialist conversation",
		"Normal repeated calls reuse the route session",
		"re-entry user message",
		"message_sequence_restart=true",
		"As a todo_task predefined route, a message_sequence behaves like a reusable specialist sub-agent",
		"restart only when the prior conversation is stale, wrong, or contaminated",
		"## MESSAGE SEQUENCE ROUTE PATTERNS",
		"**Stateful Specialist**",
		"**Test/Fix Loop**",
		"**Maker + Reviewer**",
		"**Panel of Specialists**",
		"**Clean-Room Retry**",
		"**Human-in-the-Loop Re-entry**",
		"**Top-Level Scripted Conversation**",
	}
	for _, snippet := range requiredSnippets {
		if !strings.Contains(prompt, snippet) {
			t.Fatalf("expected workshop prompt to contain %q\n\nPrompt:\n%s", snippet, prompt)
		}
	}
}

func TestOptimizerPromptDocumentsMessageSequenceRoutePatterns(t *testing.T) {
	prompt := executeInteractiveWorkshopPromptForMode(t, "optimizer")

	requiredSnippets := []string{
		"## MESSAGE SEQUENCE ROUTE PATTERNS",
		"Use these patterns when designing or hardening todo_task predefined routes",
		"**Stateful Specialist**",
		"**Test/Fix Loop**",
		"**Maker + Reviewer**",
		"**Panel of Specialists**",
		"**Clean-Room Retry**",
		"**Human-in-the-Loop Re-entry**",
		"**Top-Level Scripted Conversation**",
		"For a todo_task route, use `message_sequence` when the orchestrator should preserve specialist memory",
		"restart only when the prior conversation is stale, wrong, or contaminated",
	}
	for _, snippet := range requiredSnippets {
		if !strings.Contains(prompt, snippet) {
			t.Fatalf("expected optimizer prompt to contain %q\n\nPrompt:\n%s", snippet, prompt)
		}
	}
}
