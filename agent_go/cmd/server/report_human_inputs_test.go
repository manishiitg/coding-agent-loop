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
	if !strings.Contains(contextBlock, "choose-cadence") || !strings.Contains(contextBlock, "option=daily") {
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

func TestReportHumanInputRejectsEscapingWorkspacePath(t *testing.T) {
	t.Setenv("WORKSPACE_DOCS_PATH", t.TempDir())
	if _, err := createReportHumanInput(context.Background(), "../outside", ReportHumanInputCreateRequest{
		Question: "Should this be rejected?",
	}); err == nil {
		t.Fatal("expected path traversal workspace_path to be rejected")
	}
}
