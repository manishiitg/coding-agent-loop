package workflowrun

import "testing"

func TestCanonicalStatus(t *testing.T) {
	tests := map[string]Status{
		"running":                          StatusRunning,
		"completed":                        StatusCompleted,
		"success":                          StatusCompleted,
		"completed_with_persistence_error": StatusCompleted,
		"failed":                           StatusFailed,
		"error":                            StatusFailed,
		"cancelled":                        StatusCanceled,
		"stopped":                          StatusCanceled,
		"unknown":                          "",
	}
	for raw, want := range tests {
		if got := CanonicalStatus(raw); got != want {
			t.Errorf("CanonicalStatus(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestStatusPredicates(t *testing.T) {
	if !IsSuccessful("completed_with_persistence_error") {
		t.Fatal("legacy successful-with-warning status must remain successful")
	}
	if IsSuccessful("failed") || IsTerminal("running") || !IsTerminal("canceled") {
		t.Fatal("status predicates disagree with the canonical lifecycle")
	}
}
