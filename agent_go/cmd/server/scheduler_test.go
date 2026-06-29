package server

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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

func TestPostRunMonitorUsesSeparateLLMCostTimeReportStep(t *testing.T) {
	steps := postRunMonitorSteps()
	if got := len(steps); got != 6 {
		t.Fatalf("postRunMonitorSteps() length = %d, want 6", got)
	}
	for i, want := range []string{"triage", "fix", "report", "backup", "publish", "notify"} {
		if got := steps[i].label; got != want {
			t.Fatalf("postRunMonitorSteps()[%d].label = %q, want %q", i, got, want)
		}
	}

	var triage string
	var fix string
	var report string
	var backup string
	for _, step := range steps {
		if step.label == "triage" {
			triage = step.query
		}
		if step.label == "fix" {
			fix = step.query
		}
		if step.label == "report" {
			report = step.query
		}
		if step.label == "backup" {
			backup = step.query
		}
	}
	if triage == "" {
		t.Fatal("triage step not found")
	}
	if fix == "" {
		t.Fatal("fix step not found")
	}
	if report == "" {
		t.Fatal("report step not found")
	}
	if backup == "" {
		t.Fatal("backup step not found")
	}
	if strings.Contains(triage, "LLM/COST/TIME REPORT") || strings.Contains(triage, "costs/execution") {
		t.Fatalf("triage step should not include report-only LLM/cost/time audit:\n%s", triage)
	}
	if !strings.Contains(triage, "Triage is diagnosis/verdict only") {
		t.Fatalf("triage step should clarify that hardening is separate:\n%s", triage)
	}
	if !strings.Contains(fix, "FIX / HARDEN") {
		t.Fatalf("fix step should be the harden step:\n%s", fix)
	}
	for _, want := range []string{
		`get_reference_doc(kind="optimize-playbook")`,
		"harden_workflow",
		"This turn does not improvise manual workflow edits",
	} {
		if !strings.Contains(fix, want) {
			t.Fatalf("fix step missing %q:\n%s", want, fix)
		}
	}

	for _, want := range []string{
		"LLM/COST/TIME REPORT",
		"after fix/harden",
		"costs/execution",
		"costs/evaluation",
		"costs/phase/token_usage.json",
		"timing summaries",
		"by plan step and by agent/sub-agent",
		"do NOT change model tiers",
	} {
		if !strings.Contains(report, want) {
			t.Fatalf("report step missing %q:\n%s", want, report)
		}
	}
	if !strings.Contains(backup, "BACK UP FINAL STATE") || !strings.Contains(backup, "before publish") {
		t.Fatalf("backup step should snapshot final state before publish:\n%s", backup)
	}
}

func TestOptimizerScheduleMessagesIgnoresStoredMessagesAndInjectsCanonicalTurns(t *testing.T) {
	stored := []string{`Do not ask for confirmation. Call get_workflow_command_guidance(kind="improve-workflow", focus="scheduled improve fire").`}

	got := optimizerScheduleMessages(stored, []string{"prod"})
	if len(got) != 5 {
		t.Fatalf("optimizerScheduleMessages() length = %d, want 5", len(got))
	}

	checks := []struct {
		idx  int
		want string
	}{
		{0, "STEP 1/5 - PRE-BACKUP"},
		{1, "STEP 2/5 - IMPROVE"},
		{1, "CANONICAL AUTO IMPROVE MESSAGE"},
		{1, "group_names=prod"},
		{1, `get_workflow_command_guidance(kind="improve-workflow"`},
		{1, "Do not call notify_user"},
		{2, "STEP 3/5 - BACKUP FINAL STATE"},
		{3, "STEP 4/5 - PUBLISH"},
		{4, "STEP 5/5 - NOTIFY"},
	}
	for _, check := range checks {
		if !strings.Contains(got[check.idx], check.want) {
			t.Fatalf("optimizerScheduleMessages()[%d] missing %q:\n%s", check.idx, check.want, got[check.idx])
		}
	}
	if strings.Contains(got[1], stored[0]) {
		t.Fatalf("optimizerScheduleMessages() should ignore stored optimizer messages, got:\n%s", got[1])
	}
}

func TestOptimizerScheduleMessagesDefaultsToImproveWhenNoStoredMessage(t *testing.T) {
	got := optimizerScheduleMessages(nil, []string{"group-a"})
	if len(got) != 5 {
		t.Fatalf("optimizerScheduleMessages(nil) length = %d, want 5", len(got))
	}
	for _, want := range []string{
		`get_workflow_command_guidance(kind="improve-workflow"`,
		"group_names=group-a",
		"do NOT call harden_workflow",
		"do NOT call notify_user",
	} {
		if !strings.Contains(got[1], want) {
			t.Fatalf("default improve message missing %q:\n%s", want, got[1])
		}
	}
}

func TestOptimizerScheduleMessagesReplacesLegacyExplicitQueue(t *testing.T) {
	legacy := []string{
		"STEP 1/5 — PRE-BACKUP",
		"STEP 2/5 — IMPROVE",
		"STEP 3/5 — BACKUP FINAL STATE",
		"STEP 4/5 — PUBLISH",
		"STEP 5/5 — NOTIFY",
	}

	got := optimizerScheduleMessages(legacy, nil)
	if len(got) != 5 {
		t.Fatalf("optimizerScheduleMessages() length = %d, want 5", len(got))
	}
	if strings.Contains(strings.Join(got, "\n"), strings.Join(legacy, "\n")) {
		t.Fatalf("optimizerScheduleMessages() should replace legacy stored queues, got:\n%s", strings.Join(got, "\n"))
	}
	for _, want := range []string{
		"STEP 1/5 - PRE-BACKUP",
		"STEP 2/5 - IMPROVE",
		"STEP 3/5 - BACKUP FINAL STATE",
		"STEP 4/5 - PUBLISH",
		"STEP 5/5 - NOTIFY",
	} {
		if !strings.Contains(strings.Join(got, "\n"), want) {
			t.Fatalf("optimizerScheduleMessages() missing %q:\n%s", want, strings.Join(got, "\n"))
		}
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

func TestMaybeResumeLatestMultiAgentThreadUsesPreviousScheduledSessionOnly(t *testing.T) {
	root := t.TempDir()
	t.Setenv("WORKSPACE_DOCS_PATH", root)

	userID := "default"
	scheduleID := "schedule-1"
	writeUserChatRuntime(t, root, userID, "normal-user-chat", "claude-code", true)
	writeUserChatRuntime(t, root, userID, "previous-schedule-chat", "claude-code", true)
	writeMultiAgentScheduleRunsForTest(t, root, userID, []ScheduleRunEntry{
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
	resumed := (&SchedulerService{}).maybeResumeLatestMultiAgentThread(context.Background(), resumeTestMultiAgentScheduleContext(userID, scheduleID), reqMap, "current-schedule-chat")
	if resumed != "previous-schedule-chat" {
		t.Fatalf("resumed session = %q, want previous scheduled session", resumed)
	}
	if got := reqMap["restored_conversation_session_id"]; got != "previous-schedule-chat" {
		t.Fatalf("restored_conversation_session_id = %#v, want previous scheduled session", got)
	}
}

func TestMaybeResumeLatestMultiAgentThreadIgnoresNormalUserChat(t *testing.T) {
	root := t.TempDir()
	t.Setenv("WORKSPACE_DOCS_PATH", root)

	userID := "default"
	scheduleID := "schedule-1"
	writeUserChatRuntime(t, root, userID, "normal-user-chat", "claude-code", true)
	writeMultiAgentScheduleRunsForTest(t, root, userID, []ScheduleRunEntry{
		{
			ID:         "current-run",
			ScheduleID: scheduleID,
			SessionID:  "current-schedule-chat",
			Status:     "running",
			StartedAt:  time.Now().UTC(),
		},
	})

	reqMap := map[string]interface{}{}
	resumed := (&SchedulerService{}).maybeResumeLatestMultiAgentThread(context.Background(), resumeTestMultiAgentScheduleContext(userID, scheduleID), reqMap, "current-schedule-chat")
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

func resumeTestMultiAgentScheduleContext(userID, scheduleID string) *ScheduleContext {
	resumePrevious := true
	return &ScheduleContext{
		UserID:     userID,
		SourceType: "multi-agent",
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

func writeMultiAgentScheduleRunsForTest(t *testing.T, root, userID string, runs []ScheduleRunEntry) {
	t.Helper()
	dir := filepath.Join(root, "_users", userID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := json.MarshalIndent(runs, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "multiagent-schedule-runs.json"), data, 0o644); err != nil {
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

func writeUserChatRuntime(t *testing.T, root, userID, sessionID, provider string, resumeSupported bool) {
	t.Helper()
	convDir := filepath.Join(root, "_users", userID, "chat_history", "2026-05-20")
	if err := os.MkdirAll(convDir, 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := json.MarshalIndent(map[string]interface{}{
		"session_id": sessionID,
		"agent_mode": "simple",
		"runtime": map[string]interface{}{
			"kind":                 "coding_agent",
			"provider":             provider,
			"model_id":             "claude-opus-4-6",
			"external_session_id":  "external-" + sessionID,
			"resume_supported":     resumeSupported,
			"resume_flag":          "--resume",
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
