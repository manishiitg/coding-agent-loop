package step_based_workflow

import "testing"

func TestInferHasNewLearningFromResult(t *testing.T) {
	tests := []struct {
		name     string
		result   string
		expected bool
	}{
		{
			name:     "direct no changes summary",
			result:   "No learning changes were needed because existing SKILL.md already covers it.",
			expected: false,
		},
		{
			name:     "updated files summary",
			result:   "Learnings updated: files changed: SKILL.md, references/auth-flow.md",
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
