package step_based_workflow

import (
	"strings"
	"testing"

	"mcp-agent-builder-go/agent_go/cmd/server/guidance"
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

// After the message-sequence migration, the full pattern catalog lives in
// templates/system/message-sequence.md (loaded via get_reference_doc). The
// inline workshop prompt now carries only a brief mention of the seven
// pattern names plus the pointer; the detailed pattern descriptions are
// asserted against the rendered .md content.

func TestInteractiveWorkshopPromptDocumentsMessageSequenceRouteReuse(t *testing.T) {
	prompt := executeInteractiveWorkshopPromptForMode(t, "workshop")

	// Inline prompt should mention the patterns by name and point at the doc.
	inlineMustContain := []string{
		"## Message sequence route patterns",
		"**Stateful Specialist**",
		"**Test/Fix Loop**",
		"**Maker + Reviewer**",
		"**Panel of Specialists**",
		"**Clean-Room Retry**",
		"**Human-in-the-Loop Re-entry**",
		"**Top-Level Scripted Conversation**",
		"message_sequence_restart=true",
		"restart only when the prior conversation is stale",
		`get_reference_doc(kind="message-sequence")`,
	}
	for _, snippet := range inlineMustContain {
		if !strings.Contains(prompt, snippet) {
			t.Errorf("expected workshop prompt (builder) to contain inline snippet %q", snippet)
		}
	}

	// Detailed pattern content lives in the .md doc.
	doc := guidance.RenderSystemDoc("message-sequence")
	docMustContain := []string{
		"Route sub-agents can be `regular` for stateless one-off work, `message_sequence` for a stateful specialist conversation",
		"Normal repeated calls reuse the route session",
		"re-entry user message",
		"As a todo_task predefined route, a message_sequence behaves like a reusable specialist sub-agent",
		"restart only when the prior conversation is stale, wrong, or contaminated",
		"## MESSAGE SEQUENCE ROUTE PATTERNS",
	}
	for _, snippet := range docMustContain {
		if !strings.Contains(doc, snippet) {
			t.Errorf("expected message-sequence.md to contain snippet %q", snippet)
		}
	}
}

func TestOptimizerPromptDocumentsMessageSequenceRoutePatterns(t *testing.T) {
	prompt := executeInteractiveWorkshopPromptForMode(t, "workshop")

	inlineMustContain := []string{
		"## Message sequence route patterns",
		"**Stateful Specialist**",
		"**Test/Fix Loop**",
		"**Maker + Reviewer**",
		"**Panel of Specialists**",
		"**Clean-Room Retry**",
		"**Human-in-the-Loop Re-entry**",
		"**Top-Level Scripted Conversation**",
		`get_reference_doc(kind="message-sequence")`,
	}
	for _, snippet := range inlineMustContain {
		if !strings.Contains(prompt, snippet) {
			t.Errorf("expected workshop prompt (optimizer) to contain inline snippet %q", snippet)
		}
	}

	doc := guidance.RenderSystemDoc("message-sequence")
	docMustContain := []string{
		"## MESSAGE SEQUENCE ROUTE PATTERNS",
		"Use these patterns when designing or hardening todo_task predefined routes",
		"For a todo_task route, use `message_sequence` when the orchestrator should preserve specialist memory",
		"restart only when the prior conversation is stale, wrong, or contaminated",
	}
	for _, snippet := range docMustContain {
		if !strings.Contains(doc, snippet) {
			t.Errorf("expected message-sequence.md to contain snippet %q", snippet)
		}
	}
}
