package server

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"mcp-agent-builder-go/agent_go/pkg/workflowtypes"
)

func TestBuildScheduleCronExpressionAlwaysSetsTimezone(t *testing.T) {
	tests := []struct {
		name     string
		cronExpr string
		timezone string
		want     string
	}{
		{
			name:     "utc timezone is explicit",
			cronExpr: "0 9 * * *",
			timezone: "UTC",
			want:     "CRON_TZ=UTC 0 9 * * *",
		},
		{
			name:     "empty timezone defaults to UTC",
			cronExpr: "0 9 * * *",
			timezone: "",
			want:     "CRON_TZ=UTC 0 9 * * *",
		},
		{
			name:     "named timezone is preserved",
			cronExpr: "0 18 * * *",
			timezone: "America/New_York",
			want:     "CRON_TZ=America/New_York 0 18 * * *",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := buildScheduleCronExpression(tt.cronExpr, tt.timezone); got != tt.want {
				t.Fatalf("buildScheduleCronExpression() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestValidateScheduleTimezone(t *testing.T) {
	valid := []string{"UTC", "Asia/Kolkata", "America/New_York"}
	for _, timezone := range valid {
		t.Run("valid "+timezone, func(t *testing.T) {
			if err := ValidateScheduleTimezone(timezone); err != nil {
				t.Fatalf("ValidateScheduleTimezone(%q) returned error: %v", timezone, err)
			}
		})
	}

	invalid := []string{"", "IST", "EST", "Not/AZone"}
	for _, timezone := range invalid {
		t.Run("invalid "+timezone, func(t *testing.T) {
			if err := ValidateScheduleTimezone(timezone); err == nil {
				t.Fatalf("ValidateScheduleTimezone(%q) returned nil error", timezone)
			}
		})
	}
}

func TestWorkflowScheduleShouldResumePreviousIsOptIn(t *testing.T) {
	trueValue := true
	falseValue := false

	tests := []struct {
		name           string
		resumePrevious *bool
		want           bool
	}{
		{
			name: "omitted starts fresh",
			want: false,
		},
		{
			name:           "explicit false starts fresh",
			resumePrevious: &falseValue,
			want:           false,
		},
		{
			name:           "explicit true resumes",
			resumePrevious: &trueValue,
			want:           true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sched := WorkflowSchedule{ResumePrevious: tt.resumePrevious}
			if got := sched.ShouldResumePrevious(); got != tt.want {
				t.Fatalf("ShouldResumePrevious() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMaybeResumeLatestWorkflowThreadUsesPreviousScheduledSessionOnly(t *testing.T) {
	root := t.TempDir()
	t.Setenv("WORKSPACE_DOCS_PATH", root)

	workspacePath := "Workflow/rtslatency"
	scheduleID := "schedule-1"
	writeWorkflowChatRuntime(t, root, workspacePath, "normal-user-chat", "claude-code", true)
	writeWorkflowChatRuntime(t, root, workspacePath, "previous-schedule-chat", "claude-code", true)
	writeScheduleRunsForTest(t, root, workspacePath, []ScheduleRunEntry{
		{
			ID:         "current-run",
			ScheduleID: scheduleID,
			SessionID:  "current-schedule-chat",
			Status:     "running",
			StartedAt:  time.Now().UTC(),
		},
		{
			ID:         "previous-run",
			ScheduleID: scheduleID,
			SessionID:  "previous-schedule-chat",
			Status:     "success",
			StartedAt:  time.Now().Add(-time.Hour).UTC(),
		},
	})

	reqMap := map[string]interface{}{}
	resumed := (&SchedulerService{}).maybeResumeLatestWorkflowThread(context.Background(), resumeTestScheduleContext(workspacePath, scheduleID), reqMap, "current-schedule-chat")
	if resumed != "previous-schedule-chat" {
		t.Fatalf("resumed session = %q, want previous scheduled session", resumed)
	}
	if got := reqMap["restored_conversation_session_id"]; got != "previous-schedule-chat" {
		t.Fatalf("restored_conversation_session_id = %#v, want previous scheduled session", got)
	}
}

func TestMaybeResumeLatestWorkflowThreadIgnoresNormalUserChat(t *testing.T) {
	root := t.TempDir()
	t.Setenv("WORKSPACE_DOCS_PATH", root)

	workspacePath := "Workflow/rtslatency"
	scheduleID := "schedule-1"
	writeWorkflowChatRuntime(t, root, workspacePath, "normal-user-chat", "claude-code", true)
	writeScheduleRunsForTest(t, root, workspacePath, []ScheduleRunEntry{
		{
			ID:         "current-run",
			ScheduleID: scheduleID,
			SessionID:  "current-schedule-chat",
			Status:     "running",
			StartedAt:  time.Now().UTC(),
		},
	})

	reqMap := map[string]interface{}{}
	resumed := (&SchedulerService{}).maybeResumeLatestWorkflowThread(context.Background(), resumeTestScheduleContext(workspacePath, scheduleID), reqMap, "current-schedule-chat")
	if resumed != "" {
		t.Fatalf("resumed session = %q, want empty because normal user chats are not schedule runs", resumed)
	}
	if _, ok := reqMap["restored_conversation_session_id"]; ok {
		t.Fatalf("restored_conversation_session_id was set for a normal user chat: %#v", reqMap)
	}
}

func resumeTestScheduleContext(workspacePath, scheduleID string) *ScheduleContext {
	resumePrevious := true
	return &ScheduleContext{
		WorkspacePath: workspacePath,
		UserID:        "default",
		Schedule: WorkflowSchedule{
			ID:             scheduleID,
			ResumePrevious: &resumePrevious,
		},
		Capabilities: WorkflowCapabilities{
			LLMConfig: &workflowtypes.PresetLLMConfig{
				Provider: "claude-code",
				ModelID:  "claude-opus-4-6",
			},
		},
	}
}

func writeScheduleRunsForTest(t *testing.T, root, workspacePath string, runs []ScheduleRunEntry) {
	t.Helper()
	dir := filepath.Join(root, filepath.FromSlash(workspacePath))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := json.MarshalIndent(runs, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "schedule-runs.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeWorkflowChatRuntime(t *testing.T, root, workspacePath, sessionID, provider string, resumeSupported bool) {
	t.Helper()
	convDir := filepath.Join(root, filepath.FromSlash(workspacePath), "builder", "conversation", "2026-05-20")
	if err := os.MkdirAll(convDir, 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := json.MarshalIndent(map[string]interface{}{
		"session_id":    sessionID,
		"agent_mode":    "workflow_phase",
		"workshop_mode": "workshop",
		"runtime": map[string]interface{}{
			"kind":                 "coding_agent",
			"provider":             provider,
			"model_id":             "claude-opus-4-6",
			"external_session_id":  "external-" + sessionID,
			"resume_supported":     resumeSupported,
			"resume_flag":          "--resume",
			"workspace_path":       workspacePath,
			"workshop_mode":        "workshop",
			"agent_session_handle": map[string]interface{}{},
		},
	}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(convDir, "session-"+sessionID+"-conversation.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}
