package step_based_workflow

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
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
	proposal := goalAdvisorFinalizerProposalToolAgentAllowedToolNames()
	approved := goalAdvisorFinalizerApprovedToolAgentAllowedToolNames()

	for _, tool := range []string{"get_workflow_command_guidance", "get_reference_doc", "execute_shell_command"} {
		assertToolListContains(t, readOnly, tool)
		assertToolListContains(t, proposal, tool)
		assertToolListContains(t, approved, tool)
	}

	for _, tool := range []string{"diff_patch_workspace_file", "create_human_input_request", "upsert_report_widget"} {
		assertToolListDoesNotContain(t, readOnly, tool)
		assertToolListContains(t, proposal, tool)
		assertToolListContains(t, approved, tool)
	}

	for _, tool := range []string{"mark_human_input_consumed", "update_regular_step", "update_step_config", "update_validation_schema"} {
		assertToolListDoesNotContain(t, readOnly, tool)
		assertToolListDoesNotContain(t, proposal, tool)
		assertToolListContains(t, approved, tool)
	}

	for _, tool := range []string{"harden_workflow", "improve_kb", "improve_learnings", "improve_db", "mark_pulse_module_result", "notify_user"} {
		assertToolListDoesNotContain(t, readOnly, tool)
		assertToolListDoesNotContain(t, proposal, tool)
		assertToolListDoesNotContain(t, approved, tool)
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
		"Review mode: recovery | headroom | active_experiment | approved_answer",
		"Exactly one experiment may be active",
		"10x counterfactual",
		"Current baseline and current strategy ceiling",
		"primary success metric, guardrails, review checkpoint, and rollback condition",
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
		"Is the 10x thesis materially different from incremental tuning",
		"preserve the current successful baseline",
		"reject any second active proposal",
		"Verdict: approve | revise | reject | needs_user | no_action",
		"What the Finalizer is allowed to do",
	)
}

func TestGoalAdvisorFinalizerInstructionOwnsDurableActions(t *testing.T) {
	prompt := buildGoalAdvisorFinalizerInstruction("pulse-123", "strategy gap", "advisor draft body", "critic verdict body", nil, false)

	assertGoalAdvisorPromptContains(t, prompt,
		"stage 3/3: FINALIZER",
		"Advisor draft",
		"advisor draft body",
		"Critic verdict",
		"critic verdict body",
		"only stage allowed to make durable changes",
		"plan/config/eval mutation tools: DISABLED",
		"create_human_input_request",
		"mark_human_input_consumed: DISABLED",
		"Do not call harden_workflow",
		"Do not call mark_pulse_module_result",
		"Advisor proposal/takeaway",
		"Critic verdict/objections",
		"Never leave more than one active .advisor-experiment",
		`data-advisor-experiment-id="advisor-exp-<stable-slug>"`,
		"Current baseline, Current strategy ceiling, 10x thesis",
		"Update the existing card in place",
	)
}

func TestGoalAdvisorFinalizerInstructionListsApprovedProposalGate(t *testing.T) {
	prompt := buildGoalAdvisorFinalizerInstruction(
		"pulse-123",
		"strategy gap",
		"advisor draft body",
		"Verdict: approve\nSafe to apply.",
		[]goalAdvisorApprovedPlanProposal{{
			ID:               "plan-proposal-new-segment",
			Context:          "Add a discovery segment.",
			Evidence:         "builder/improve.html",
			SelectedOptionID: "approve",
			Note:             "Do it.",
		}},
		true,
	)

	assertGoalAdvisorPromptContains(t, prompt,
		"plan/config/eval mutation tools: ENABLED",
		"mark_human_input_consumed: ENABLED",
		"input_id: plan-proposal-new-segment",
		"selected_option_id: approve",
		"apply only the verified approved plan-proposal ids listed above",
	)
}

func TestGoalAdvisorCriticApprovesPlanMutationRequiresApproveVerdict(t *testing.T) {
	tests := []struct {
		name string
		body string
		want bool
	}{
		{name: "approve", body: "- Verdict: approve\nLooks safe.", want: true},
		{name: "approve with detail", body: "Verdict: approve_with_limits", want: true},
		{name: "reject", body: "Verdict: reject", want: false},
		{name: "needs user", body: "Verdict: needs_user", want: false},
		{name: "missing", body: "Looks ok, but no structured verdict.", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := goalAdvisorCriticApprovesPlanMutation(tt.body); got != tt.want {
				t.Fatalf("goalAdvisorCriticApprovesPlanMutation() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestApprovedGoalAdvisorPlanProposalsReadsOnlyApprovedAnsweredRows(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	t.Setenv("WORKSPACE_DOCS_PATH", root)
	workspacePath := "Workflow/demo"
	dbDir := filepath.Join(root, "Workflow", "demo", "db")
	if err := os.MkdirAll(dbDir, 0o755); err != nil {
		t.Fatalf("mkdir db dir: %v", err)
	}
	db, err := sql.Open("sqlite", filepath.Join(dbDir, "db.sqlite"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()
	_, err = db.ExecContext(ctx, `CREATE TABLE report_human_inputs (
		id TEXT PRIMARY KEY,
		workspace_path TEXT NOT NULL,
		source TEXT NOT NULL,
		status TEXT NOT NULL,
		context TEXT NOT NULL DEFAULT '',
		evidence TEXT NOT NULL DEFAULT '',
		selected_option_id TEXT NOT NULL DEFAULT '',
		note TEXT NOT NULL DEFAULT '',
		answered_at TEXT NOT NULL DEFAULT '',
		updated_at TEXT NOT NULL DEFAULT ''
	)`)
	if err != nil {
		t.Fatalf("create table: %v", err)
	}
	rows := []struct {
		id, source, status, selected string
	}{
		{"plan-proposal-approved", "goal_advisor", "answered", "approve"},
		{"plan-proposal-deferred", "goal_advisor", "answered", "defer"},
		{"plan-proposal-pending", "goal_advisor", "pending", "approve"},
		{"input-other", "goal_advisor", "answered", "approve"},
		{"plan-proposal-pulse", "pulse", "answered", "approve"},
	}
	for _, row := range rows {
		if _, err := db.ExecContext(ctx, `INSERT INTO report_human_inputs
			(id, workspace_path, source, status, context, evidence, selected_option_id, note, answered_at, updated_at)
			VALUES (?, ?, ?, ?, 'context', 'evidence', ?, 'note', '2026-07-09T00:00:00Z', '2026-07-09T00:00:00Z')`,
			row.id, workspacePath, row.source, row.status, row.selected); err != nil {
			t.Fatalf("insert %s: %v", row.id, err)
		}
	}

	iwm := &InteractiveWorkshopManager{}
	got, err := iwm.approvedGoalAdvisorPlanProposals(ctx, workspacePath)
	if err != nil {
		t.Fatalf("approvedGoalAdvisorPlanProposals() error = %v", err)
	}
	if len(got) != 1 || got[0].ID != "plan-proposal-approved" {
		t.Fatalf("approved proposals = %#v, want only plan-proposal-approved", got)
	}
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
