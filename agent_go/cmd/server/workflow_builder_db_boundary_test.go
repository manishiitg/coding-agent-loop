package server

import (
	"os"
	"strings"
	"testing"
)

func TestWorkflowBuilderSetupAppliesManagedDBBoundaryAfterEveryGuardReset(t *testing.T) {
	source, err := os.ReadFile("server.go")
	if err != nil {
		t.Fatalf("read server.go: %v", err)
	}

	// handleQuery configures Work once and the workflow session once in the common folder-
	// guard branch and once again in the workflow-phase setup/restore branch.
	// Both reset the base guard, so both must reapply the logical DB grant and
	// raw db.sqlite/WAL/SHM deny.
	const call = "todo_creation_human.ConfigureManagedWorkflowDBSession("
	if got := strings.Count(string(source), call); got != 3 {
		t.Fatalf("managed project DB boundary call count = %d, want 3", got)
	}
}
