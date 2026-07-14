package server

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReportHumanInputsUseWorkflowLocalDB(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	t.Setenv("WORKSPACE_DOCS_PATH", root)
	workspacePath := "Workflow/example"
	dbPath := filepath.Join(root, "Workflow", "example", "db", "db.sqlite")

	inputs, err := listReportHumanInputs(ctx, workspacePath, "", "")
	if err != nil {
		t.Fatalf("list before create: %v", err)
	}
	if len(inputs) != 0 {
		t.Fatalf("list before create returned %d inputs, want 0", len(inputs))
	}
	if _, err := os.Stat(dbPath); !os.IsNotExist(err) {
		t.Fatalf("read-only list created db unexpectedly: stat err=%v", err)
	}

	created, err := createReportHumanInput(ctx, workspacePath, ReportHumanInputCreateRequest{
		InputID:       "choose-cadence",
		Source:        "goal_advisor",
		Priority:      "high",
		Question:      "Should Goal Advisor run daily until recovery?",
		Context:       "The workflow missed the goal three times.",
		Evidence:      "builder/improve.html#latest",
		CreatedBy:     "test",
		AllowFreeText: true,
		Options: []ReportHumanInputOption{
			{ID: "daily", Title: "Run daily", Description: "Escalate until two clean runs."},
			{ID: "weekly", Title: "Keep weekly", Description: "Avoid extra cost for now."},
		},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.WorkspacePath != workspacePath || created.Source != "goal_advisor" || created.Status != "pending" {
		t.Fatalf("created input mismatch: %+v", created)
	}
	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("expected workflow-local db at %s: %v", dbPath, err)
	}

	pending, err := listReportHumanInputs(ctx, workspacePath, "pending", "")
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	if len(pending) != 1 || pending[0].ID != "choose-cadence" {
		t.Fatalf("pending mismatch: %+v", pending)
	}

	answered, err := answerReportHumanInput(ctx, workspacePath, "choose-cadence", ReportHumanInputAnswerRequest{
		SelectedOptionID: "daily",
		Note:             "Escalate for this week.",
		AnsweredBy:       "user",
	})
	if err != nil {
		t.Fatalf("answer: %v", err)
	}
	if answered.Status != "answered" || answered.SelectedOptionID != "daily" || !strings.Contains(answered.Note, "Escalate") {
		t.Fatalf("answered mismatch: %+v", answered)
	}

	contextBlock := formatAnsweredReportHumanInputsForAgent(ctx, workspacePath)
	if !strings.Contains(contextBlock, "choose-cadence") || !strings.Contains(contextBlock, "option=daily") || !strings.Contains(contextBlock, "The workflow missed the goal three times.") {
		t.Fatalf("answered context missing answer details:\n%s", contextBlock)
	}

	consumed, err := consumeReportHumanInput(ctx, workspacePath, "choose-cadence", ReportHumanInputConsumeRequest{
		OutcomeSummary: "Goal Advisor cadence kept daily until recovery.",
		ConsumedBy:     "pulse",
	})
	if err != nil {
		t.Fatalf("consume: %v", err)
	}
	if consumed.Status != "consumed" || !strings.Contains(consumed.OutcomeSummary, "daily") {
		t.Fatalf("consumed mismatch: %+v", consumed)
	}
	if block := formatAnsweredReportHumanInputsForAgent(ctx, workspacePath); block != "" {
		t.Fatalf("consumed answer should not be re-injected, got:\n%s", block)
	}
}

func TestAnsweredGoalAdvisorPlanProposalCarriesContext(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	t.Setenv("WORKSPACE_DOCS_PATH", root)
	workspacePath := "Workflow/proposal"
	proposalContext := "Proposal: add a validation step before delivery. Exact edits: add regular step validate-offer after draft-offer; update delivery dependency. Rationale: two clean runs still sent weak offers. Expected impact: fewer off-goal deliveries. Risk: extra runtime. Evidence: runs/iteration-0/group-1/evaluation_report.json"

	_, err := createReportHumanInput(ctx, workspacePath, ReportHumanInputCreateRequest{
		InputID:  "plan-proposal-validate-offer",
		Source:   "goal_advisor",
		Priority: "high",
		Question: "Approve adding an offer-validation step?",
		Context:  proposalContext,
		Evidence: "runs/iteration-0/group-1/evaluation_report.json",
		Options: []ReportHumanInputOption{
			{ID: "approve", Title: "Approve", Description: "Apply this plan change in the next Pulse pass."},
			{ID: "reject", Title: "Reject", Description: "Keep the current plan."},
			{ID: "defer", Title: "Defer", Description: "Wait for more evidence."},
		},
	})
	if err != nil {
		t.Fatalf("create proposal: %v", err)
	}
	_, err = answerReportHumanInput(ctx, workspacePath, "plan-proposal-validate-offer", ReportHumanInputAnswerRequest{
		SelectedOptionID: "approve",
		AnsweredBy:       "user",
	})
	if err != nil {
		t.Fatalf("answer proposal: %v", err)
	}

	contextBlock := formatAnsweredReportHumanInputsForAgent(ctx, workspacePath)
	for _, want := range []string{
		"plan-proposal-validate-offer",
		"option=approve",
		"add regular step validate-offer",
		"apply it only with normal plan modification/config/eval/report tools",
		"mark_human_input_consumed",
	} {
		if !strings.Contains(contextBlock, want) {
			t.Fatalf("answered proposal context missing %q:\n%s", want, contextBlock)
		}
	}
}

func TestAnsweredChiefOfStaffInputsAreAggregatedAcrossOrgAndWorkflowScopes(t *testing.T) {
	ctx := context.Background()
	t.Setenv("WORKSPACE_DOCS_PATH", t.TempDir())

	_, err := createReportHumanInput(ctx, "pulse", ReportHumanInputCreateRequest{
		InputID:  "org-budget-decision",
		Source:   "chief_of_staff",
		Priority: "high",
		Question: "Should the organization prioritize retention this week?",
		Options: []ReportHumanInputOption{
			{ID: "retention", Title: "Prioritize retention"},
			{ID: "acquisition", Title: "Prioritize acquisition"},
		},
	})
	if err != nil {
		t.Fatalf("create org question: %v", err)
	}
	_, err = answerReportHumanInput(ctx, "pulse", "org-budget-decision", ReportHumanInputAnswerRequest{
		SelectedOptionID: "retention",
		AnsweredBy:       "user",
	})
	if err != nil {
		t.Fatalf("answer org question: %v", err)
	}

	_, err = createReportHumanInput(ctx, "Workflow/sales", ReportHumanInputCreateRequest{
		InputID:       "sales-follow-up",
		Source:        "chief_of_staff",
		Question:      "What should Sales test next?",
		AllowFreeText: true,
	})
	if err != nil {
		t.Fatalf("create workflow question: %v", err)
	}
	_, err = answerReportHumanInput(ctx, "Workflow/sales", "sales-follow-up", ReportHumanInputAnswerRequest{
		Note:       "Test a shorter follow-up sequence.",
		AnsweredBy: "user",
	})
	if err != nil {
		t.Fatalf("answer workflow question: %v", err)
	}

	_, err = createReportHumanInput(ctx, "Workflow/sales", ReportHumanInputCreateRequest{
		InputID:       "pulse-only-question",
		Source:        "pulse",
		Question:      "Should workflow Pulse retry?",
		AllowFreeText: true,
	})
	if err != nil {
		t.Fatalf("create pulse question: %v", err)
	}
	_, err = answerReportHumanInput(ctx, "Workflow/sales", "pulse-only-question", ReportHumanInputAnswerRequest{
		Note:       "Retry once.",
		AnsweredBy: "user",
	})
	if err != nil {
		t.Fatalf("answer pulse question: %v", err)
	}

	contextBlock := claimAnsweredChiefOfStaffInputsForAgent(ctx, []string{"pulse", "Workflow/sales", "pulse"}, "test-chief-run")
	defer releaseChiefOfStaffInputClaims(context.Background(), []string{"pulse", "Workflow/sales"}, "test-chief-run")
	for _, want := range []string{
		"Answered Chief of Staff questions waiting for this run",
		"workspace_path=pulse input_id=org-budget-decision",
		"option=retention (Prioritize retention)",
		"workspace_path=Workflow/sales input_id=sales-follow-up",
		"Test a shorter follow-up sequence.",
		"mark_human_input_consumed",
		"Do not mark an answer consumed merely because you read it",
	} {
		if !strings.Contains(contextBlock, want) {
			t.Fatalf("Chief of Staff answer context missing %q:\n%s", want, contextBlock)
		}
	}
	if strings.Contains(contextBlock, "pulse-only-question") {
		t.Fatalf("Chief of Staff context included a workflow Pulse answer:\n%s", contextBlock)
	}
	if count := strings.Count(contextBlock, "input_id=org-budget-decision"); count != 1 {
		t.Fatalf("duplicate workspace scope produced %d copies, want 1:\n%s", count, contextBlock)
	}
}

func TestChiefOfStaffAnswersAreLeasedToOneSchedule(t *testing.T) {
	ctx := context.Background()
	t.Setenv("WORKSPACE_DOCS_PATH", t.TempDir())
	workspacePath := "Workflow/sales"
	_, err := createReportHumanInput(ctx, workspacePath, ReportHumanInputCreateRequest{
		InputID:       "sales-decision",
		Source:        "chief_of_staff",
		Question:      "Which sales experiment should run?",
		AllowFreeText: true,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := answerReportHumanInput(ctx, workspacePath, "sales-decision", ReportHumanInputAnswerRequest{Note: "Test referrals."}); err != nil {
		t.Fatalf("answer: %v", err)
	}

	first := claimAnsweredChiefOfStaffInputsForAgent(ctx, []string{workspacePath}, "schedule-run-1")
	if !strings.Contains(first, "sales-decision") {
		t.Fatalf("first schedule did not claim answer:\n%s", first)
	}
	if second := claimAnsweredChiefOfStaffInputsForAgent(ctx, []string{workspacePath}, "schedule-run-2"); second != "" {
		t.Fatalf("second schedule received an already claimed answer:\n%s", second)
	}

	releaseChiefOfStaffInputClaims(ctx, []string{workspacePath}, "schedule-run-1")
	third := claimAnsweredChiefOfStaffInputsForAgent(ctx, []string{workspacePath}, "schedule-run-3")
	if !strings.Contains(third, "sales-decision") {
		t.Fatalf("unhandled answer was not released for a later schedule:\n%s", third)
	}
	if _, err := consumeReportHumanInput(ctx, workspacePath, "sales-decision", ReportHumanInputConsumeRequest{
		OutcomeSummary: "Queued the referral experiment.",
		ConsumedBy:     "schedule-run-3",
	}); err != nil {
		t.Fatalf("consume claimed answer: %v", err)
	}
	releaseChiefOfStaffInputClaims(ctx, []string{workspacePath}, "schedule-run-3")
	if later := claimAnsweredChiefOfStaffInputsForAgent(ctx, []string{workspacePath}, "schedule-run-4"); later != "" {
		t.Fatalf("consumed answer was delivered again:\n%s", later)
	}
}

func TestReportHumanInputAllowsFreeTextInsteadOfOption(t *testing.T) {
	ctx := context.Background()
	t.Setenv("WORKSPACE_DOCS_PATH", t.TempDir())
	workspacePath := "Workflow/free-text-choice"

	_, err := createReportHumanInput(ctx, workspacePath, ReportHumanInputCreateRequest{
		InputID:       "custom-answer",
		Source:        "pulse",
		Question:      "Which backup policy should be used?",
		AllowFreeText: true,
		Options: []ReportHumanInputOption{
			{ID: "strict", Title: "Strict", Description: "Require secret migration first."},
			{ID: "partial", Title: "Partial", Description: "Continue excluding the config file."},
		},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if _, err := answerReportHumanInput(ctx, workspacePath, "custom-answer", ReportHumanInputAnswerRequest{}); err == nil {
		t.Fatal("empty answer should be rejected")
	}

	answered, err := answerReportHumanInput(ctx, workspacePath, "custom-answer", ReportHumanInputAnswerRequest{
		Note:       "Keep the full backup and do not treat the portal password as a workflow secret.",
		AnsweredBy: "user",
	})
	if err != nil {
		t.Fatalf("answer with free text: %v", err)
	}
	if answered.Status != "answered" || answered.SelectedOptionID != "" || !strings.Contains(answered.Note, "full backup") {
		t.Fatalf("note-only answer mismatch: %+v", answered)
	}
}

func TestReportHumanInputRejectsEscapingWorkspacePath(t *testing.T) {
	t.Setenv("WORKSPACE_DOCS_PATH", t.TempDir())
	if _, err := createReportHumanInput(context.Background(), "../outside", ReportHumanInputCreateRequest{
		Question: "Should this be rejected?",
	}); err == nil {
		t.Fatal("expected path traversal workspace_path to be rejected")
	}
}
