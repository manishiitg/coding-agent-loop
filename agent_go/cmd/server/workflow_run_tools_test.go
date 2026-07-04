package server

import "testing"

func TestCreateWorkflowRunToolsDisabledForChiefOfStaff(t *testing.T) {
	tools := createWorkflowRunTools()
	if len(tools) != 0 {
		t.Fatalf("Chief of Staff workflow execution tools must be disabled, got %#v", tools)
	}
	for _, toolName := range []string{"run_workflow", "run_step", "stop_workflow_run"} {
		if isPreRegisteredMultiAgentTool(toolName) {
			t.Fatalf("%s must not be treated as a pre-registered multi-agent tool", toolName)
		}
	}
}

func TestParseRouteSelectionsArg(t *testing.T) {
	got, err := parseRouteSelectionsArg(map[string]interface{}{
		" route-by-mode ": " search ",
	})
	if err != nil {
		t.Fatalf("parseRouteSelectionsArg returned error: %v", err)
	}
	if got["route-by-mode"] != "search" {
		t.Fatalf("route selection was not trimmed: %#v", got)
	}
}

func TestParseRouteSelectionsArgRejectsNonStringValue(t *testing.T) {
	_, err := parseRouteSelectionsArg(map[string]interface{}{
		"route-by-mode": 12,
	})
	if err == nil {
		t.Fatal("expected error for non-string route selection value")
	}
}
