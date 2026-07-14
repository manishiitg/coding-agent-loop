package server

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
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
	if len(responses) != 1 || responses[0].Status != "pending" || responses[0].Revision != 0 {
		t.Fatalf("list before answer should instantiate one pending response: %+v", responses)
	}
	dbPath := filepath.Join(root, "Workflow", "linkedin", "db", "db.sqlite")
	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("render/list should instantiate the step-readable response table: %v", err)
	}

	if _, err := answerReportWidgetResponse(ctx, workspacePath, "linkedin-draft-review", ReportWidgetResponseAnswerRequest{
		InstanceKey:            "draft-123-v1",
		SelectedOptionID:       "not-configured",
		ExpectedSubjectID:      "draft-123",
		ExpectedSubjectVersion: "1",
		ExpectedSubjectHash:    "sha256:abc123",
	}); err == nil {
		t.Fatal("unconfigured option should be rejected")
	}

	answered, err := answerReportWidgetResponse(ctx, workspacePath, "linkedin-draft-review", ReportWidgetResponseAnswerRequest{
		InstanceKey:            "draft-123-v1",
		SelectedOptionID:       "request_changes",
		Note:                   "Make the opening more personal.",
		AnsweredBy:             "user",
		ExpectedSubjectID:      "draft-123",
		ExpectedSubjectVersion: "1",
		ExpectedSubjectHash:    "sha256:abc123",
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
		SelectedOptionID:       "approve",
		AnsweredBy:             "user",
		ExpectedSubjectID:      "draft-123",
		ExpectedSubjectVersion: "1",
		ExpectedSubjectHash:    "sha256:abc123",
	})
	if err != nil {
		t.Fatalf("update answer: %v", err)
	}
	if updated.Revision != 2 || updated.SelectedOptionID != "approve" {
		t.Fatalf("updated response mismatch: %+v", updated)
	}

	claimed, err := claimReportWidgetResponse(ctx, workspacePath, "linkedin-draft-review", ReportWidgetResponseClaimRequest{
		ExpectedRevision: updated.Revision,
		ExecutionKey:     "linkedin-draft-review:draft-123-v1:2:publish-step",
		ClaimedBy:        "publish-step",
	})
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if claimed.Status != "executing" || claimed.ExecutionRevision != updated.Revision {
		t.Fatalf("claimed response mismatch: %+v", claimed)
	}
	if _, err := claimReportWidgetResponse(ctx, workspacePath, "linkedin-draft-review", ReportWidgetResponseClaimRequest{
		ExpectedRevision: updated.Revision,
		ExecutionKey:     "different-execution",
		ClaimedBy:        "duplicate-step",
	}); err == nil {
		t.Fatal("a second execution must not claim the same response revision")
	}

	consumed, err := consumeReportWidgetResponse(ctx, workspacePath, "linkedin-draft-review", ReportWidgetResponseConsumeRequest{
		ExpectedRevision: updated.Revision,
		ExecutionKey:     claimed.ExecutionKey,
		OutcomeSummary:   "Published the approved draft.",
		ConsumedBy:       "publish-step",
	})
	if err != nil {
		t.Fatalf("consume: %v", err)
	}
	if consumed.Status != "completed" || consumed.OutcomeSummary == "" {
		t.Fatalf("consumed response mismatch: %+v", consumed)
	}
	var eventCount int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM report_widget_response_events
		WHERE widget_id=? AND instance_key=?`, "linkedin-draft-review", "draft-123-v1").Scan(&eventCount); err != nil {
		t.Fatalf("query response audit events: %v", err)
	}
	if eventCount != 4 {
		t.Fatalf("response audit event count = %d, want 4", eventCount)
	}
}

func TestConfiguredReportWidgetResponseRejectsStaleSubject(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	t.Setenv("WORKSPACE_DOCS_PATH", root)
	workspacePath := "Workflow/linkedin"
	writeInteractionReportPlan(t, root, workspacePath)
	if _, err := listReportWidgetResponses(ctx, workspacePath, "linkedin-draft-review", "draft-123-v1", ""); err != nil {
		t.Fatalf("instantiate response: %v", err)
	}

	planPath := filepath.Join(root, "Workflow", "linkedin", "reports", "report_plan.json")
	raw, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatalf("read report plan: %v", err)
	}
	updated := strings.Replace(string(raw), "sha256:abc123", "sha256:new-content", 1)
	if err := os.WriteFile(planPath, []byte(updated), 0o644); err != nil {
		t.Fatalf("update report plan: %v", err)
	}
	if _, err := answerReportWidgetResponse(ctx, workspacePath, "linkedin-draft-review", ReportWidgetResponseAnswerRequest{
		InstanceKey:            "draft-123-v1",
		SelectedOptionID:       "approve",
		ExpectedSubjectID:      "draft-123",
		ExpectedSubjectVersion: "1",
		ExpectedSubjectHash:    "sha256:abc123",
	}); !errors.Is(err, errStaleReportWidgetResponse) {
		t.Fatalf("stale displayed subject error = %v, want stale response", err)
	}
	if _, err := listReportWidgetResponses(ctx, workspacePath, "linkedin-draft-review", "draft-123-v1", ""); !errors.Is(err, errReportWidgetConflict) {
		t.Fatalf("reused instance key error = %v, want conflict", err)
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
