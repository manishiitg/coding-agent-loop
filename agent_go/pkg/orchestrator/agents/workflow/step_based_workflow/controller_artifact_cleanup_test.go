package step_based_workflow

import "testing"

func TestGenericAgentArtifactFolderForStepMatchesCurrentAndLegacyNames(t *testing.T) {
	tests := []struct {
		name       string
		folderName string
		stepNumber int
		want       bool
	}{
		{
			name:       "current control-flow folder",
			folderName: "step-2-generic-download-report",
			stepNumber: 2,
			want:       true,
		},
		{
			name:       "current control-flow folder with old generic-agent spelling",
			folderName: "step-2-generic-agent-download-report",
			stepNumber: 2,
			want:       true,
		},
		{
			name:       "legacy stable id folder",
			folderName: "generic-step-2-download-report",
			stepNumber: 2,
			want:       true,
		},
		{
			name:       "different step",
			folderName: "step-20-generic-download-report",
			stepNumber: 2,
			want:       false,
		},
		{
			name:       "not generic",
			folderName: "step-2-sub-download-report",
			stepNumber: 2,
			want:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isGenericAgentArtifactFolderForStep(tt.folderName, tt.stepNumber); got != tt.want {
				t.Fatalf("isGenericAgentArtifactFolderForStep(%q, %d) = %v, want %v", tt.folderName, tt.stepNumber, got, tt.want)
			}
		})
	}
}

func TestMessageSequenceStepPathForStepMatchesFreshRerunPaths(t *testing.T) {
	tests := []struct {
		name       string
		stepPath   string
		stepNumber int
		want       bool
	}{
		{
			name:       "top-level message sequence",
			stepPath:   "step-3",
			stepNumber: 3,
			want:       true,
		},
		{
			name:       "sub-agent message sequence route",
			stepPath:   "step-3-sub-login",
			stepNumber: 3,
			want:       true,
		},
		{
			name:       "generic route path",
			stepPath:   "step-3-generic-fetch-context",
			stepNumber: 3,
			want:       true,
		},
		{
			name:       "different step",
			stepPath:   "step-30-sub-login",
			stepNumber: 3,
			want:       false,
		},
		{
			name:       "branch folder is not a message sequence route root",
			stepPath:   "step-3-if-true-0",
			stepNumber: 3,
			want:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isMessageSequenceStepPathForStep(tt.stepPath, tt.stepNumber); got != tt.want {
				t.Fatalf("isMessageSequenceStepPathForStep(%q, %d) = %v, want %v", tt.stepPath, tt.stepNumber, got, tt.want)
			}
		})
	}
}

func TestTodoSubAgentArtifactFolderNameIncludesTodoID(t *testing.T) {
	got := todoSubAgentArtifactFolderName("step-2", "research/route", "Task 001: Check API")
	want := "step-2-sub-research-route-task-001-check-api"
	if got != want {
		t.Fatalf("todoSubAgentArtifactFolderName() = %q, want %q", got, want)
	}
}
