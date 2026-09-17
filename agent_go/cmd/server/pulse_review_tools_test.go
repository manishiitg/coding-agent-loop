package server

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	step_based_workflow "github.com/manishiitg/coding-agent-loop/agent_go/pkg/orchestrator/agents/workflow/step_based_workflow"
	mcpexecutor "github.com/manishiitg/mcpagent/executor"
)

func TestTypedPulseReviewerToolsPersistFindingWithoutCompletionHandshake(t *testing.T) {
	root := t.TempDir()
	t.Setenv("WORKSPACE_DOCS_PATH", root)
	workspacePath := "Workflow/typed-review-tools"
	const sessionID = "2026-08-28T12-00-00.000Z_typed-reviewer-session"
	pulseRunID := "schedule-manual--typed-review"
	ctx := mcpexecutor.WithSessionID(context.Background(), sessionID)

	_, executors, _ := createPulseWorklistTools()
	recordFinding := executors["record_pulse_finding"].(func(context.Context, map[string]interface{}) (string, error))
	raw, err := recordFinding(ctx, map[string]interface{}{
		"workspace_path": workspacePath, "pulse_run_id": pulseRunID,
		"module": pulseModuleTechnicalReview, "concern": "collector silently drops failed rows",
		"issue_kind": "workflow_issue", "classification": "correctness_bug", "severity": "high",
		"summary": "Failed rows disappear", "impact": "The workflow can report success on incomplete data.",
		"evidence": []interface{}{"runs/iteration-0/result.json"},
	})
	if err != nil {
		t.Fatalf("record_pulse_finding: %v", err)
	}
	var recorded step_based_workflow.PulseReviewFindingRecord
	if err := json.Unmarshal([]byte(raw), &recorded); err != nil || recorded.IssueID == "" || !strings.HasPrefix(recorded.IssueID, "PUL-") {
		t.Fatalf("recorded finding=%#v decode_err=%v raw=%s", recorded, err, raw)
	}
	if strings.Contains(raw, "fingerprint") || strings.Contains(raw, "finding_id") {
		t.Fatalf("public record_pulse_finding response leaked an internal lifecycle identity: %s", raw)
	}
	// A completion retry may replay the tool call. It must not manufacture a
	// second recurrence in the same review identity.
	if _, err := recordFinding(ctx, map[string]interface{}{
		"workspace_path": workspacePath, "pulse_run_id": pulseRunID,
		"module": pulseModuleTechnicalReview, "concern": "collector silently drops failed rows",
		"issue_kind": "workflow_issue", "classification": "correctness_bug", "severity": "high",
		"summary": "Failed rows disappear", "impact": "The workflow can report success on incomplete data.",
		"evidence": []interface{}{"runs/iteration-0/result.json"},
	}); err != nil {
		t.Fatalf("idempotent record_pulse_finding retry: %v", err)
	}

	findings, err := step_based_workflow.LoadPulseFindingLifecycles(context.Background(), workspacePath, pulseModuleTechnicalReview, -1)
	if err != nil || len(findings) != 1 || findings[0].SeenCount != 1 || findings[0].Details == nil || findings[0].Details.Summary != "Failed rows disappear" {
		t.Fatalf("unexpected lifecycle: %#v err=%v", findings, err)
	}
}

func TestPulseReviewFocusToolsPersistDurableAgenda(t *testing.T) {
	root := t.TempDir()
	t.Setenv("WORKSPACE_DOCS_PATH", root)
	const workspacePath = "Workflow/focus-history"
	const pulseRunID = "focus-session"
	ctx := mcpexecutor.WithSessionID(context.Background(), pulseRunID)
	_, executors, _ := createPulseWorklistTools()
	record := executors["record_pulse_result"].(func(context.Context, map[string]interface{}) (string, error))
	// get_pulse_review_focus_agenda was folded into get_pulse_state(view="focus_agenda").
	agenda := executors["get_pulse_state"].(func(context.Context, map[string]interface{}) (string, error))

	if _, err := record(ctx, map[string]interface{}{
		"workspace_path": workspacePath, "pulse_run_id": pulseRunID, "module": pulseModuleTechnicalReview,
		"result": "running", "reason": "manual technical review", "note_only": true, "manual": true,
	}); err != nil {
		t.Fatalf("start manual review: %v", err)
	}
	_, err := record(ctx, map[string]interface{}{
		"workspace_path": workspacePath, "pulse_run_id": pulseRunID, "module": pulseModuleTechnicalReview,
		"result": "done", "reason": "technical focus reviewed", "evidence": []interface{}{"runs/iteration-0/execution/attempt.json"},
		"focuses": []interface{}{map[string]interface{}{
			"route_scope": "daily-execution/small-route",
			"focus_key":   "execution_health", "priority_class": "overdue", "selection_reason": "This lens has not been reviewed since the timeout recurrence.",
			"verdict": "Observer now has a targeted recheck.", "evidence": []interface{}{"runs/iteration-0/execution/attempt.json"},
			"issue_ids": []interface{}{"PUL-AB12CD34"}, "deferred_focuses": []interface{}{"store_integrity"},
			"next_check_at": "2026-08-21T00:00:00Z", "next_check_reason": "next producing run",
		}},
	})
	if err != nil {
		t.Fatalf("record focus: %v", err)
	}
	if _, err := record(ctx, map[string]interface{}{
		"workspace_path": workspacePath, "pulse_run_id": pulseRunID, "module": pulseModuleArchitectureReview,
		"result": "running", "reason": "manual architecture review", "note_only": true, "manual": true,
	}); err != nil {
		t.Fatalf("start second manual review: %v", err)
	}
	if _, err := record(ctx, map[string]interface{}{
		"workspace_path": workspacePath, "pulse_run_id": pulseRunID, "module": pulseModuleArchitectureReview,
		"result": "done", "reason": "architecture focus reviewed",
		"focuses": []interface{}{map[string]interface{}{
			"route_scope": "daily-execution/large-route",
			"focus_key":   "orchestration_design", "priority_class": "new_or_changed", "selection_reason": "The larger route has distinct payload amplification evidence.",
			"verdict": "The large route needs a structural proposal.", "evidence": []interface{}{"runs/iteration-1/costs/execution.json"},
		}},
	}); err != nil {
		t.Fatalf("record second route focus: %v", err)
	}
	raw, err := agenda(ctx, map[string]interface{}{"workspace_path": workspacePath, "view": "focus_agenda", "module": pulseModuleTechnicalReview, "route_scope": "daily-execution/small-route"})
	if err != nil {
		t.Fatalf("read agenda: %v", err)
	}
	var response struct {
		Focuses []PulseReviewFocus `json:"focuses"`
	}
	if err := json.Unmarshal([]byte(raw), &response); err != nil || len(response.Focuses) != len(pulseReviewFocusCatalog[pulseModuleTechnicalReview]) {
		t.Fatalf("agenda=%#v decode_err=%v raw=%s", response, err, raw)
	}
	counts := map[string][2]int{}
	for _, focus := range response.Focuses {
		counts[focus.FocusKey] = [2]int{focus.ReviewCount, focus.RouteReviewCount}
	}
	if counts["execution_health"] != [2]int{1, 1} {
		t.Fatalf("small-route execution-health counts = %v", counts["execution_health"])
	}
	if counts["store_integrity"] != [2]int{0, 0} {
		t.Fatalf("small-route deferred store counts = %v", counts["store_integrity"])
	}
	selections, err := getPulseReviewFocusSelections(ctx, workspacePath, 10)
	if err != nil {
		t.Fatalf("load review focus selections: %v", err)
	}
	for _, selection := range selections {
		if selection.FocusKey == "execution_health" {
			if !slices.Contains(selection.IssueIDs, "PUL-AB12CD34") {
				t.Fatalf("execution-health issue links = %#v", selection.IssueIDs)
			}
			return
		}
	}
	t.Fatal("execution-health selection was not returned")
}

func TestTypedPulseReviewerToolsRequireCurrentConversation(t *testing.T) {
	_, executors, _ := createPulseWorklistTools()
	recordFinding := executors["record_pulse_finding"].(func(context.Context, map[string]interface{}) (string, error))
	if _, err := recordFinding(mcpexecutor.WithSessionID(context.Background(), "untrusted-reviewer"), map[string]interface{}{
		"workspace_path": "Workflow/demo", "pulse_run_id": "another-conversation", "module": pulseModuleWorkflowReview,
	}); err == nil {
		t.Fatal("untrusted reviewer write unexpectedly succeeded")
	}
}
