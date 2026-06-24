package server

import "testing"

func TestWorkflowBackupEffectiveState(t *testing.T) {
	sourceHash := "current-hash"

	tests := []struct {
		name   string
		config *WorkflowBackupConfig
		status *WorkflowBackupStatus
		want   string
	}{
		{
			name:   "missing config",
			config: nil,
			status: nil,
			want:   workflowBackupStateNotConfigured,
		},
		{
			name:   "disabled config",
			config: &WorkflowBackupConfig{Enabled: false},
			status: &WorkflowBackupStatus{State: workflowBackupStateHealthy, LastSourceHash: sourceHash},
			want:   workflowBackupStateNotConfigured,
		},
		{
			name:   "enabled without status",
			config: &WorkflowBackupConfig{Enabled: true},
			status: nil,
			want:   workflowBackupStateConfiguredNotVerified,
		},
		{
			name:   "healthy current hash",
			config: &WorkflowBackupConfig{Enabled: true},
			status: &WorkflowBackupStatus{State: workflowBackupStateHealthy, LastSourceHash: sourceHash},
			want:   workflowBackupStateHealthy,
		},
		{
			name:   "healthy stale hash",
			config: &WorkflowBackupConfig{Enabled: true},
			status: &WorkflowBackupStatus{State: workflowBackupStateHealthy, LastSourceHash: "old-hash"},
			want:   workflowBackupStateStale,
		},
		{
			name:   "failed stays failed",
			config: &WorkflowBackupConfig{Enabled: true},
			status: &WorkflowBackupStatus{State: workflowBackupStateFailed, LastSourceHash: "old-hash"},
			want:   workflowBackupStateFailed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := workflowBackupEffectiveState(tt.config, tt.status, sourceHash)
			if got != tt.want {
				t.Fatalf("workflowBackupEffectiveState() = %q, want %q", got, tt.want)
			}
		})
	}
}
