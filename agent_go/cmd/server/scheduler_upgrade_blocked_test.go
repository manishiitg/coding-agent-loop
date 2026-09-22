package server

import (
	"errors"
	"strings"
	"testing"
)

// A manifest whose version this server does not know has no upgrade path at
// all. The manual upgrade view must explain that instead of inventing turns.
func TestManualUpgradeRejectsUnknownVersion(t *testing.T) {
	_, err := manualWorkflowUpgradeTurns(&WorkflowManifest{Version: "9.9.9"}, []string{"run it"}, "Workflow/test")
	if err == nil {
		t.Fatal("a version with no upgrade path should not produce runnable turns")
	}
	if !errors.Is(err, errWorkflowContractMigrationRequired) {
		t.Fatalf("missing upgrade path is not marked as requiring manual resolution: %v", err)
	}
	if !strings.Contains(err.Error(), "no complete upgrade path") {
		t.Errorf("error lost its explanation: %v", err)
	}
}
