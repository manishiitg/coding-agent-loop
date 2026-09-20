package server

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

// Before this the upgrade instructions were Go constants only the scheduler
// delivered, so a blocked workflow showed its owner one run-history line naming
// a version and nothing else. confida-login sat stuck for days and diagnosing
// it meant reading server logs and session transcripts by hand.
func TestContractUpgradeStatusShowsWhatIsOwedAndTheActualInstruction(t *testing.T) {
	const workspacePath = "Workflow/confida-login"
	manifestJSON, err := json.Marshal(map[string]interface{}{
		"schema_version": 1,
		"id":             "wf_confida",
		"version":        "1.0.20",
		"label":          "confida-qa-testing",
		"capabilities":   map[string]interface{}{},
		"schedules":      []interface{}{},
	})
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	workspace := &mockWorkspaceAPI{files: map[string]string{
		workspacePath + "/workflow.json": string(manifestJSON),
	}}
	server := httptest.NewServer(workspace)
	defer server.Close()
	t.Setenv("WORKSPACE_API_URL", server.URL)

	out, err := describeWorkflowContractUpgrades(context.Background(), workspacePath)
	if err != nil {
		t.Fatalf("describeWorkflowContractUpgrades: %v", err)
	}

	for _, want := range []string{
		"Current: `1.0.20`",
		"Pending migrations (19)",
		"upgrade-current-artifact-contract",
		"upgrade-direct-html-reports",
		"upgrade-schedule-execution-model",
		"upgrade-dedicated-pulse-schedule",
		"upgrade-schedule-prompt-contract",
		"upgrade-schedule-finalizer-ownership",
		"upgrade-report-activity-section",
		"upgrade-report-activity-tab",
		"upgrade-pulse-lifecycle-reconciliation",
		"upgrade-pulse-actionable-backlog",
		"upgrade-orchestrator-step-type",
		"upgrade-explicit-schedule-pulse",
		"upgrade-eval-retirement",
		// The full instruction text, not a summary of it — an owner judging
		// whether a stalled migration is safe needs the actual words.
		"NOTHING IS DELETED IN THIS MIGRATION",
		// A stalled rung blocks the workflow itself; say so.
		"blocking preflight",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("status output missing %q", want)
		}
	}
	if strings.Contains(out, "upgrade-run-scoped-routes") || strings.Contains(out, "migrate_run_scoped_routes") || strings.Contains(out, `set_workflow_contract_version(version="1.0.42")`) {
		t.Errorf("status output still exposes the retired route migration:\n%s", out)
	}
}

func TestContractUpgradeStatusIsQuietWhenNothingIsOwed(t *testing.T) {
	const workspacePath = "Workflow/current"
	manifestJSON, _ := json.Marshal(map[string]interface{}{
		"schema_version": 1,
		"id":             "wf_current",
		"version":        WorkflowContractCurrentVersion,
		"label":          "current",
		"capabilities":   map[string]interface{}{},
		"schedules":      []interface{}{},
	})
	workspace := &mockWorkspaceAPI{files: map[string]string{
		workspacePath + "/workflow.json": string(manifestJSON),
	}}
	server := httptest.NewServer(workspace)
	defer server.Close()
	t.Setenv("WORKSPACE_API_URL", server.URL)

	out, err := describeWorkflowContractUpgrades(context.Background(), workspacePath)
	if err != nil {
		t.Fatalf("describeWorkflowContractUpgrades: %v", err)
	}
	if !strings.Contains(out, "No pending migrations") {
		t.Errorf("a current workflow should report nothing owed:\n%s", out)
	}
	if strings.Contains(out, "WORKFLOW CONTRACT UPGRADE") {
		t.Errorf("a current workflow should not be shown migration instructions:\n%s", out)
	}
}

// A version this server does not know has no upgrade path at all, and every
// scheduled run refuses to start. That is the least self-evident failure in the
// subsystem, so it gets said plainly rather than rendered as an empty list.
func TestContractUpgradeStatusExplainsAnUnknownVersion(t *testing.T) {
	const workspacePath = "Workflow/newer"
	manifestJSON, _ := json.Marshal(map[string]interface{}{
		"schema_version": 1,
		"id":             "wf_newer",
		"version":        "9.9.9",
		"label":          "newer",
		"capabilities":   map[string]interface{}{},
		"schedules":      []interface{}{},
	})
	workspace := &mockWorkspaceAPI{files: map[string]string{
		workspacePath + "/workflow.json": string(manifestJSON),
	}}
	server := httptest.NewServer(workspace)
	defer server.Close()
	t.Setenv("WORKSPACE_API_URL", server.URL)

	out, err := describeWorkflowContractUpgrades(context.Background(), workspacePath)
	if err != nil {
		t.Fatalf("describeWorkflowContractUpgrades: %v", err)
	}
	for _, want := range []string{"not one this server knows", "no upgrade path", "refuse to start"} {
		if !strings.Contains(out, want) {
			t.Errorf("unknown-version status missing %q:\n%s", want, out)
		}
	}
}
