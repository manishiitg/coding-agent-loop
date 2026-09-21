package server

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func serveContractGuardManifest(t *testing.T, version string) string {
	t.Helper()
	const workspacePath = "Workflow/contract-guard"
	data, err := json.Marshal(map[string]interface{}{
		"schema_version": 1,
		"version":        version,
		"id":             "wf-contract-guard",
		"label":          "Contract guard",
		"capabilities":   map[string]interface{}{},
		"schedules":      []interface{}{},
	})
	if err != nil {
		t.Fatal(err)
	}
	workspace := &mockWorkspaceAPI{files: map[string]string{
		workspacePath + "/workflow.json": string(data),
	}}
	server := httptest.NewServer(workspace)
	t.Cleanup(server.Close)
	t.Setenv("WORKSPACE_API_URL", server.URL)
	return workspacePath
}

func TestRequireCurrentWorkflowContractForManualRunAllowsCurrent(t *testing.T) {
	workspacePath := serveContractGuardManifest(t, WorkflowContractCurrentVersion)
	if err := requireCurrentWorkflowContractForManualRun(context.Background(), workspacePath); err != nil {
		t.Fatalf("current contract blocked: %v", err)
	}
}

func TestRequireCurrentWorkflowContractForManualRunAllowsRetiredMarkers(t *testing.T) {
	for _, version := range []string{
		workflowContractExplicitSchedulePulseVersion,
		workflowContractRunScopedRoutesVersion,
		workflowContractEvalRetirementVersion,
	} {
		t.Run(version, func(t *testing.T) {
			workspacePath := serveContractGuardManifest(t, version)
			if err := requireCurrentWorkflowContractForManualRun(context.Background(), workspacePath); err != nil {
				t.Fatalf("execution-compatible retired marker %s was blocked: %v", version, err)
			}
		})
	}
}

func TestRequireCurrentWorkflowContractForManualRunAsksBeforeMigration(t *testing.T) {
	workspacePath := serveContractGuardManifest(t, workflowContractRouteSummariesVersion)
	err := requireCurrentWorkflowContractForManualRun(context.Background(), workspacePath)
	if err == nil {
		t.Fatal("stale contract was allowed")
	}
	message := err.Error()
	for _, want := range []string{
		"workflow_contract_migration_required",
		"no step was started",
		"Shall I migrate it now?",
		"get_contract_upgrades",
		WorkflowContractCurrentVersion,
	} {
		if !strings.Contains(message, want) {
			t.Fatalf("guard message missing %q: %s", want, message)
		}
	}
}

func TestRequireCurrentWorkflowContractForManualRunRejectsUnknownVersion(t *testing.T) {
	workspacePath := serveContractGuardManifest(t, "99.0.0")
	err := requireCurrentWorkflowContractForManualRun(context.Background(), workspacePath)
	if err == nil || !strings.Contains(err.Error(), "workflow_contract_migration_required") {
		t.Fatalf("unknown contract must block safely, got %v", err)
	}
}

func TestWorkflowContractExecutionGuardWrapsBothManualRunTools(t *testing.T) {
	workspacePath := serveContractGuardManifest(t, workflowContractRouteSummariesVersion)
	draft := &productSurfaceDraft{}
	guard := workflowContractExecutionGuardRegistrar{
		definitionRegistrar: draft,
		workspacePath:       workspacePath,
	}
	called := 0
	for _, name := range []string{"execute_step", "run_full_workflow"} {
		if err := guard.RegisterCustomTool(name, "test", map[string]interface{}{}, func(context.Context, map[string]interface{}) (string, error) {
			called++
			return "started", nil
		}, "workflow"); err != nil {
			t.Fatal(err)
		}
		_, err := draft.tools[name].exec(context.Background(), nil)
		if err == nil || !strings.Contains(err.Error(), "Shall I migrate it now?") {
			t.Fatalf("%s did not return migration prompt: %v", name, err)
		}
	}
	if called != 0 {
		t.Fatalf("guarded executors ran %d time(s)", called)
	}
}
