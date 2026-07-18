package step_based_workflow

import (
	"strings"
	"testing"

	"github.com/manishiitg/coding-agent-loop/agent_go/cmd/server/guidance"
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

	// The dedicated "Message sequence routes" inline section was folded
	// into the consolidated "Planning steps" section that lists
	// message-sequence as one of several per-step-type deep-dive skills.
	// The agent reaches the full pattern catalog by loading the skill.
	inlineMustContain := []string{
		"## Planning steps",
		"message-sequence",
		"Stateful Specialist",
		"Test/Fix Loop",
		"Maker+Reviewer",
		"Clean-Room Retry",
		"HITL Re-entry",
		`get_reference_doc(kind="plan-design")`,
	}
	for _, snippet := range inlineMustContain {
		if !strings.Contains(prompt, snippet) {
			t.Errorf("expected workshop prompt (builder) to contain inline snippet %q", snippet)
		}
	}

	// Detailed pattern content lives in the .md doc.
	doc := guidance.RenderSystemDoc("message-sequence")
	docMustContain := []string{
		"Conversational route sub-agents use `message_sequence`, including stateless one-turn work",
		"Normal repeated calls reuse the route conversation",
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

	// The pattern catalog now lives entirely in message-sequence.md.
	// The inline workshop prompt only carries the consolidated
	// "Planning steps" section that names message-sequence (and the
	// other per-step-type skills) as deep-dive entry points.
	inlineMustContain := []string{
		"## Planning steps",
		"message-sequence",
		"Stateful Specialist",
		"Test/Fix Loop",
		"Maker+Reviewer",
		"Clean-Room Retry",
		"HITL Re-entry",
		"Scripted Conversation",
	}
	for _, snippet := range inlineMustContain {
		if !strings.Contains(prompt, snippet) {
			t.Errorf("expected workshop prompt (optimizer) to contain inline snippet %q", snippet)
		}
	}

	doc := guidance.RenderSystemDoc("message-sequence")
	docMustContain := []string{
		"## MESSAGE SEQUENCE ROUTE PATTERNS",
		"Use these patterns when designing or repairing todo_task predefined routes",
		"For a todo_task route, use `message_sequence` when the orchestrator should preserve specialist memory",
		"restart only when the prior conversation is stale, wrong, or contaminated",
	}
	for _, snippet := range docMustContain {
		if !strings.Contains(doc, snippet) {
			t.Errorf("expected message-sequence.md to contain snippet %q", snippet)
		}
	}
}
