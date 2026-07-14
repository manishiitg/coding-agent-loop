package server

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

func writeInteractionReportPlan(t *testing.T, root, workspacePath string) {
	t.Helper()
	planPath := filepath.Join(root, filepath.FromSlash(workspacePath), "reports", "report_plan.json")
	if err := os.MkdirAll(filepath.Dir(planPath), 0o755); err != nil {
		t.Fatalf("mkdir report plan: %v", err)
	}
	plan := `{
	  "version": 1,
	  "sections": [{
	    "heading": "Review",
	    "entries": [{
	      "kind": "single",
	      "widget": {
	        "id": "linkedin-draft-review",
	        "kind": "interaction",
	        "question": "What should happen to this LinkedIn draft?",
	        "responseKind": "choice-with-text",
	        "options": [
	          {"id": "approve", "title": "Approve"},
	          {"id": "request_changes", "title": "Request changes"},
	          {"id": "reject", "title": "Reject"}
	        ],
	        "instanceKey": "draft-123-v1",
	        "subjectId": "draft-123",
	        "subjectVersion": "1",
	        "subjectHash": "sha256:abc123"
	      }
	    }]
	  }]
	}`
	if err := os.WriteFile(planPath, []byte(plan), 0o644); err != nil {
		t.Fatalf("write report plan: %v", err)
	}
}

func TestConfiguredReportWidgetResponsePersistsForWorkflowSteps(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	t.Setenv("WORKSPACE_DOCS_PATH", root)
	workspacePath := "Workflow/linkedin"
	writeInteractionReportPlan(t, root, workspacePath)

	responses, err := listReportWidgetResponses(ctx, workspacePath, "linkedin-draft-review", "draft-123-v1", "")
	if err != nil {
		t.Fatalf("list before answer: %v", err)
	}
	if len(responses) != 0 {
		t.Fatalf("list before answer returned %d responses", len(responses))
	}
	dbPath := filepath.Join(root, "Workflow", "linkedin", "db", "db.sqlite")
	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("render/list should instantiate the step-readable response table: %v", err)
	}

	if _, err := answerReportWidgetResponse(ctx, workspacePath, "linkedin-draft-review", ReportWidgetResponseAnswerRequest{
		InstanceKey:      "draft-123-v1",
		SelectedOptionID: "not-configured",
	}); err == nil {
		t.Fatal("unconfigured option should be rejected")
	}

	answered, err := answerReportWidgetResponse(ctx, workspacePath, "linkedin-draft-review", ReportWidgetResponseAnswerRequest{
		InstanceKey:      "draft-123-v1",
		SelectedOptionID: "request_changes",
		Note:             "Make the opening more personal.",
		AnsweredBy:       "user",
	})
	if err != nil {
		t.Fatalf("answer: %v", err)
	}
	if answered.Status != "answered" || answered.SelectedOptionID != "request_changes" || answered.Revision != 1 {
		t.Fatalf("answered response mismatch: %+v", answered)
	}
	if answered.SubjectID != "draft-123" || answered.SubjectVersion != "1" || answered.SubjectHash != "sha256:abc123" {
		t.Fatalf("subject binding mismatch: %+v", answered)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open workflow db: %v", err)
	}
	defer db.Close()
	var optionID, note, status string
	if err := db.QueryRowContext(ctx, `SELECT selected_option_id, note, status
		FROM report_widget_responses WHERE widget_id=? AND instance_key=?`,
		"linkedin-draft-review", "draft-123-v1").Scan(&optionID, &note, &status); err != nil {
		t.Fatalf("query step-readable response: %v", err)
	}
	if optionID != "request_changes" || note != "Make the opening more personal." || status != "answered" {
		t.Fatalf("step-readable row mismatch: option=%q note=%q status=%q", optionID, note, status)
	}

	updated, err := answerReportWidgetResponse(ctx, workspacePath, "linkedin-draft-review", ReportWidgetResponseAnswerRequest{
		SelectedOptionID: "approve",
		AnsweredBy:       "user",
	})
	if err != nil {
		t.Fatalf("update answer: %v", err)
	}
	if updated.Revision != 2 || updated.SelectedOptionID != "approve" {
		t.Fatalf("updated response mismatch: %+v", updated)
	}

	consumed, err := consumeReportWidgetResponse(ctx, workspacePath, "linkedin-draft-review", ReportWidgetResponseConsumeRequest{
		OutcomeSummary: "Published the approved draft.",
		ConsumedBy:     "publish-step",
	})
	if err != nil {
		t.Fatalf("consume: %v", err)
	}
	if consumed.Status != "consumed" || consumed.OutcomeSummary == "" {
		t.Fatalf("consumed response mismatch: %+v", consumed)
	}
}

func TestConfiguredReportWidgetResponseRejectsUnconfiguredWidget(t *testing.T) {
	root := t.TempDir()
	t.Setenv("WORKSPACE_DOCS_PATH", root)
	workspacePath := "Workflow/linkedin"
	writeInteractionReportPlan(t, root, workspacePath)

	if _, err := answerReportWidgetResponse(context.Background(), workspacePath, "missing-widget", ReportWidgetResponseAnswerRequest{
		SelectedOptionID: "approve",
	}); err == nil {
		t.Fatal("missing configured widget should be rejected")
	}
}
