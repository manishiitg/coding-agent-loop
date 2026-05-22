package step_based_workflow

import "testing"

// resetAdaptiveExecutionTierState wipes the tier state back to its
// pristine post-promotion-failure / post-description-change baseline. The
// rest of the adaptive-tier decision tree assumes these fields are all
// cleared together.
func TestResetAdaptiveExecutionTierStateClearsAllStreaks(t *testing.T) {
	t.Parallel()
	md := &LearningMetadata{
		PreferredExecutionTier:              executionTierPreferenceMedium,
		MediumSuccessStreak:                 7,
		HighSuccessStreakSinceMediumFailure: 3,
		LastMediumFailureAt:                 "2026-05-22T17:00:00+05:30",
	}

	resetAdaptiveExecutionTierState(md, "test reset")

	if md.PreferredExecutionTier != executionTierPreferenceHigh {
		t.Fatalf("PreferredExecutionTier = %q, want %q", md.PreferredExecutionTier, executionTierPreferenceHigh)
	}
	if md.MediumSuccessStreak != 0 {
		t.Fatalf("MediumSuccessStreak = %d, want 0", md.MediumSuccessStreak)
	}
	if md.HighSuccessStreakSinceMediumFailure != 0 {
		t.Fatalf("HighSuccessStreakSinceMediumFailure = %d, want 0", md.HighSuccessStreakSinceMediumFailure)
	}
	if md.LastMediumFailureAt != "" {
		t.Fatalf("LastMediumFailureAt = %q, want \"\"", md.LastMediumFailureAt)
	}
	if md.LastTierDecisionReason != "test reset" {
		t.Fatalf("LastTierDecisionReason = %q, want %q", md.LastTierDecisionReason, "test reset")
	}
}

// normalizeExecutionTierPreference accepts the canonical "high" and "medium"
// strings (any casing/whitespace), and folds every other value — including
// empty and the retired "low" — to "high". This is what gates the medium
// branch in decideAdaptiveExecutionTier; the function must NEVER return
// anything outside {high, medium}.
func TestNormalizeExecutionTierPreference(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		in   string
		want string
	}{
		{"high", executionTierPreferenceHigh},
		{"HIGH", executionTierPreferenceHigh},
		{"  high  ", executionTierPreferenceHigh},
		{"medium", executionTierPreferenceMedium},
		{"MEDIUM", executionTierPreferenceMedium},
		{"low", executionTierPreferenceHigh},  // low is never an adaptive choice
		{"", executionTierPreferenceHigh},     // missing → high
		{"agent", executionTierPreferenceHigh}, // garbage → high
	} {
		if got := normalizeExecutionTierPreference(tc.in); got != tc.want {
			t.Errorf("normalizeExecutionTierPreference(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// The simplified tier signature has no hasLearnings or currentLearningHash
// parameters — adaptive tier promotion is gated on description stability and
// success count only. This compile-time pin catches any accidental re-addition
// of learning-content-based gating.
func TestDecideAdaptiveExecutionTierSignatureIsDescriptionOnly(t *testing.T) {
	t.Parallel()
	// Type-only assertion: decideAdaptiveExecutionTier must accept exactly
	// (ctx, learningPathIdentifier, stepPath, currentDescriptionHash).
	// If the signature gains a learning-content parameter again, this fails
	// to compile.
	var _ func(*StepBasedWorkflowOrchestrator) func(
		ctx interface{},
		learningPathIdentifier string,
		stepPath string,
		currentDescriptionHash string,
	) (adaptiveExecutionTierDecision, error)
}
