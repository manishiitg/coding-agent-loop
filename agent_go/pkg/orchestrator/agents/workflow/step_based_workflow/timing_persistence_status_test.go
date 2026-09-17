package step_based_workflow

import (
	"testing"
	"time"
)

func TestRunPersistenceWarningDoesNotCreateAnotherOutcome(t *testing.T) {
	startedAt := time.Date(2026, 9, 17, 1, 0, 0, 0, time.UTC)
	completedAt := startedAt.Add(time.Minute)
	meta := map[string]interface{}{
		"persistence_errors": []interface{}{map[string]interface{}{"error": "disk unavailable"}},
	}
	applyRunFinalization(meta, "completed_with_persistence_error", startedAt, completedAt)
	applyRunFinalization(meta, "completed", startedAt, completedAt)
	if meta["status"] != "completed" {
		t.Fatalf("status = %v, want completed", meta["status"])
	}
	warnings, _ := meta["warnings"].([]interface{})
	if len(warnings) != 1 || warnings[0] != "persistence_error" {
		t.Fatalf("warnings = %#v, want one persistence_error", warnings)
	}
}
