package step_based_workflow

import (
	"strings"
	"testing"
)

func assertGoalAdvisorPromptContains(t *testing.T, prompt string, snippets ...string) {
	t.Helper()
	for _, snippet := range snippets {
		if !strings.Contains(prompt, snippet) {
			t.Fatalf("goal advisor prompt missing %q\n\nPrompt:\n%s", snippet, prompt)
		}
	}
}

func assertToolListContains(t *testing.T, tools []string, tool string) {
	t.Helper()
	for _, candidate := range tools {
		if candidate == tool {
			return
		}
	}
	t.Fatalf("tool list missing %q in %v", tool, tools)
}

func assertToolListDoesNotContain(t *testing.T, tools []string, tool string) {
	t.Helper()
	for _, candidate := range tools {
		if candidate == tool {
			t.Fatalf("tool list should not contain %q in %v", tool, tools)
		}
	}
}

func TestGoalAdvisorToolAllowlistsSeparateReadOnlyAndFinalizerActions(t *testing.T) {
	readOnly := goalAdvisorReadOnlyToolAgentAllowedToolNames()
	mutable := goalAdvisorToolAgentAllowedToolNames()

	for _, tool := range []string{"get_workflow_command_guidance", "get_reference_doc", "execute_shell_command"} {
		assertToolListContains(t, readOnly, tool)
		assertToolListContains(t, mutable, tool)
	}

	for _, tool := range []string{"diff_patch_workspace_file", "create_human_input_request", "mark_human_input_consumed", "update_regular_step", "upsert_report_widget"} {
		assertToolListDoesNotContain(t, readOnly, tool)
		assertToolListContains(t, mutable, tool)
	}

	for _, tool := range []string{"harden_workflow", "improve_kb", "improve_learnings", "improve_db", "mark_pulse_module_result", "notify_user"} {
		assertToolListDoesNotContain(t, readOnly, tool)
		assertToolListDoesNotContain(t, mutable, tool)
	}
}

func TestGoalAdvisorAdvisorInstructionIsReadOnlyDraft(t *testing.T) {
	prompt := buildGoalAdvisorAdvisorInstruction("pulse-123", "goals are flat")

	assertGoalAdvisorPromptContains(t, prompt,
		"stage 1/3: ADVISOR DRAFT",
		"Pulse run id: pulse-123",
		"Focus from Pulse Gate: goals are flat",
		"this stage is read-only",
		"Do NOT call harden_workflow",
		"Do NOT modify plan/config/eval/report/HTML files",
		"Evidence used",
		"Advisor hypothesis",
		"Routine-maintenance deferrals",
	)
}

func TestGoalAdvisorCriticInstructionChallengesAdvisorWithoutMutating(t *testing.T) {
	prompt := buildGoalAdvisorCriticInstruction("pulse-123", "conversion stalled", "advisor draft body")

	assertGoalAdvisorPromptContains(t, prompt,
		"stage 2/3: INDEPENDENT CRITIC",
		"Advisor draft to critique",
		"advisor draft body",
		"Do NOT modify plan/config/eval/report/HTML files",
		"Is every important claim backed by concrete run/eval/report/HTML/db evidence?",
		"Does it hallucinate unavailable data",
		"Verdict: approve | revise | reject | needs_user | no_action",
		"What the Finalizer is allowed to do",
	)
}

func TestGoalAdvisorFinalizerInstructionOwnsDurableActions(t *testing.T) {
	prompt := buildGoalAdvisorFinalizerInstruction("pulse-123", "strategy gap", "advisor draft body", "critic verdict body")

	assertGoalAdvisorPromptContains(t, prompt,
		"stage 3/3: FINALIZER",
		"Advisor draft",
		"advisor draft body",
		"Critic verdict",
		"critic verdict body",
		"only stage allowed to make durable changes",
		"create_human_input_request",
		"mark_human_input_consumed",
		"Do not call harden_workflow",
		"Do not call mark_pulse_module_result",
		"Advisor proposal/takeaway",
		"Critic verdict/objections",
	)
}

func TestTruncateGoalAdvisorStageOutputKeepsHeadAndTail(t *testing.T) {
	short := "short output"
	if got := truncateGoalAdvisorStageOutput(short); got != short {
		t.Fatalf("short output should not be changed: %q", got)
	}

	long := strings.Repeat("A", 11_000) + "MIDDLE" + strings.Repeat("Z", 11_000)
	got := truncateGoalAdvisorStageOutput(long)
	if len(got) >= len(long) {
		t.Fatalf("expected long output to be truncated; got len=%d want < %d", len(got), len(long))
	}
	assertGoalAdvisorPromptContains(t, got,
		strings.Repeat("A", 100),
		"[Goal Advisor stage output truncated for next-stage review]",
		strings.Repeat("Z", 100),
	)
	if strings.Contains(got, "MIDDLE") {
		t.Fatalf("expected middle of long output to be truncated")
	}
}
