package server

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	orchEvents "github.com/manishiitg/coding-agent-loop/agent_go/pkg/orchestrator/events"
)

func TestLoadPulseReviewRunsForExecutionLogsKeepsOnlyPostRunReviewAgents(t *testing.T) {
	const workspacePath = "Workflow/pulse-logs"
	const sessionID = "schedule-cron--daily_1788273006728851000"
	started := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	workflowCompleted := started.Add(20 * time.Minute)
	pulseStarted := workflowCompleted.Add(time.Minute)
	runCompleted := pulseStarted.Add(5 * time.Minute)
	runs := []ScheduleRunEntry{{
		ID: "run-1", ScheduleID: "daily", TriggerSource: "cron",
		RunFolder: "iteration-0", GroupNames: []string{"default"}, SessionID: sessionID,
		Status: "success", StartedAt: started, CompletedAt: &runCompleted,
	}}
	runsJSON, err := json.Marshal(runs)
	if err != nil {
		t.Fatal(err)
	}
	transcriptPath := orchEvents.BackgroundAgentTranscriptPath(workspacePath, sessionID, "bg-pulse-review")
	transcript := orchEvents.NewBackgroundAgentTranscript(sessionID, "bg-pulse-review", "parent-review-fix", "Pulse Technical Maintenance", "sub_agent", pulseStarted)
	transcript.Provider = "codex-cli"
	transcript.ModelID = "gpt-6-sol"
	transcript.AppendEvent(orchEvents.BackgroundAgentTranscriptEvent{Timestamp: pulseStarted, Type: "user_message", Role: "user", Text: "Review the due technical backlog."})
	transcriptContent, err := transcript.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	workspace := httptest.NewServer(&mockWorkspaceAPI{files: map[string]string{
		workspacePath + "/schedule-runs.json": string(runsJSON),
		transcriptPath:                        transcriptContent,
	}})
	t.Cleanup(workspace.Close)
	t.Setenv("WORKSPACE_API_URL", workspace.URL)
	t.Setenv("WORKSPACE_DOCS_PATH", t.TempDir())

	api := &StreamingAPI{activeSessions: map[string]*ActiveSessionInfo{
		sessionID: {SessionID: sessionID, WorkspacePath: workspacePath},
	}}
	api.recordBackgroundAgentLogStarted(sessionID, "workflow-child", "Workflow child", orchEvents.ExecutionKindSubAgent, "full-run", started.Add(time.Minute))
	api.recordBackgroundAgentLogCompleted(sessionID, "workflow-child", "Workflow child", "sub_agent", "completed", "done", "", "1m", started.Add(2*time.Minute))
	api.recordBackgroundAgentLogStarted(sessionID, "full-run", "Full Workflow Execution", orchEvents.ExecutionKindFullRun, "", started)
	api.recordBackgroundAgentLogCompleted(sessionID, "full-run", "Full Workflow Execution", "full_run", "completed", "done", "", "20m", workflowCompleted)
	api.recordBackgroundAgentLogStarted(sessionID, "bg-pulse-review", "Pulse Technical Maintenance", orchEvents.ExecutionKindSubAgent, "parent-review-fix", pulseStarted)
	api.recordBackgroundAgentLogCompleted(sessionID, "bg-pulse-review", "Pulse Technical Maintenance", "sub_agent", "completed", "fixed two issues", "", "5m", runCompleted)
	api.recordBackgroundAgentTranscriptPath(sessionID, "bg-pulse-review", transcriptPath, "ok")

	got, err := loadPulseReviewRunsForExecutionLogs(context.Background(), workspacePath, "iteration-0/default")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || len(got[0].Agents) != 1 {
		t.Fatalf("Pulse review runs=%+v, want one pass with one post-run reviewer", got)
	}
	agent := got[0].Agents[0]
	if agent.AgentID != "bg-pulse-review" || agent.ParentExecutionID != "parent-review-fix" {
		t.Fatalf("unexpected Pulse agent: %+v", agent)
	}
	if agent.Provider != "codex-cli" || agent.ModelID != "gpt-6-sol" || len(agent.Events) != 1 || agent.Events[0].Text != "Review the due technical backlog." {
		t.Fatalf("transcript not loaded: %+v", agent)
	}
}

func TestScheduleRunMatchesExecutionFolderRespectsGroups(t *testing.T) {
	run := ScheduleRunEntry{RunFolder: "iteration-7", GroupNames: []string{"linkedin"}}
	if !scheduleRunMatchesExecutionFolder(run, "iteration-7/linkedin") {
		t.Fatal("iteration-only schedule folder did not match its explicit group")
	}
	if scheduleRunMatchesExecutionFolder(run, "iteration-7/email") {
		t.Fatal("iteration-only schedule folder matched an unrelated group")
	}
	if scheduleRunMatchesExecutionFolder(ScheduleRunEntry{RunFolder: "iteration-7/linkedin"}, "iteration-7/email") {
		t.Fatal("two explicit groups were treated as the same run")
	}
}

func TestManualPulseRunIncludesReviewWithoutFullRunMarker(t *testing.T) {
	run := ScheduleRunEntry{ScheduleID: manualWorkflowPulseScheduleID}
	entries := []BackgroundAgentLogEntry{
		{AgentID: "bg-review", Name: "Focused review", Kind: "sub_agent", StartedAt: "2026-09-01T10:00:00Z"},
		{AgentID: "step", Name: "Workflow step", Kind: "workflow_step", StartedAt: "2026-09-01T10:00:00Z"},
	}
	got := selectPulseBackgroundReviewEntries(run, entries)
	if len(got) != 1 || got[0].AgentID != "bg-review" {
		t.Fatalf("manual Pulse selection=%+v", got)
	}
}
