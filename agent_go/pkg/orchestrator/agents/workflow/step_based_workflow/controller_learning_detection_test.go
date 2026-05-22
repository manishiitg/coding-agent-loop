package step_based_workflow

import "testing"

func TestInferHasNewLearningFromResult(t *testing.T) {
	tests := []struct {
		name     string
		result   string
		expected bool
	}{
		{
			name:     "agent no-op summary",
			result:   "Updated: no-op; reason: existing references already cover this pattern",
			expected: false,
		},
		{
			name:     "direct no-op summary",
			result:   "Learnings updated: no-op; reason: existing SKILL.md already covers it",
			expected: false,
		},
		{
			name:     "updated files summary",
			result:   "Updated: /tmp/learnings/_global/ (files: SKILL.md, references/auth-flow.md)",
			expected: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, _, _ := inferHasNewLearningFromResult(tc.result)
			if got != tc.expected {
				t.Fatalf("inferHasNewLearningFromResult(%q) = %v, want %v", tc.result, got, tc.expected)
			}
		})
	}
}

func TestShouldAutoLockLearnings(t *testing.T) {
	tests := []struct {
		name                 string
		id                   string
		metadata             LearningMetadata
		requireNoNewLearning bool
		expected             bool
	}{
		{
			name: "locks after threshold and repeated no-op outcomes",
			id:   "step-a",
			metadata: LearningMetadata{
				LastDescriptionHash:          "1234567890abcdef",
				DescriptionHashRuns:          autoLockMinSuccessfulRuns,
				ConsecutiveNoNewLearningRuns: autoLockConsecutiveNoNewLearningRuns,
			},
			requireNoNewLearning: true,
			expected:             true,
		},
		{
			name: "legacy threshold-only lock path ignores no-op streak",
			id:   "step-agent",
			metadata: LearningMetadata{
				LastDescriptionHash:          "1234567890abcdef",
				DescriptionHashRuns:          autoLockMinSuccessfulRuns,
				ConsecutiveNoNewLearningRuns: 0,
			},
			requireNoNewLearning: false,
			expected:             true,
		},
		{
			name: "does not lock before minimum successful runs",
			id:   "step-b",
			metadata: LearningMetadata{
				LastDescriptionHash:          "1234567890abcdef",
				DescriptionHashRuns:          autoLockMinSuccessfulRuns - 1,
				ConsecutiveNoNewLearningRuns: autoLockConsecutiveNoNewLearningRuns,
			},
			requireNoNewLearning: true,
			expected:             false,
		},
		{
			name: "does not lock when recent learning is still new",
			id:   "step-c",
			metadata: LearningMetadata{
				LastDescriptionHash:          "1234567890abcdef",
				DescriptionHashRuns:          autoLockMinSuccessfulRuns + 2,
				ConsecutiveNoNewLearningRuns: autoLockConsecutiveNoNewLearningRuns - 1,
			},
			requireNoNewLearning: true,
			expected:             false,
		},
		{
			name: "global learnings never auto-lock",
			id:   GlobalLearningID,
			metadata: LearningMetadata{
				LastDescriptionHash:          "1234567890abcdef",
				DescriptionHashRuns:          autoLockMinSuccessfulRuns + 5,
				ConsecutiveNoNewLearningRuns: autoLockConsecutiveNoNewLearningRuns + 5,
			},
			requireNoNewLearning: true,
			expected:             false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, _ := shouldAutoLockLearnings(tc.metadata, tc.id, tc.requireNoNewLearning)
			if got != tc.expected {
				t.Fatalf("shouldAutoLockLearnings() = %v, want %v", got, tc.expected)
			}
		})
	}
}
