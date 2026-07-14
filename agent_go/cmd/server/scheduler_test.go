package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	virtualtools "github.com/manishiitg/coding-agent-loop/agent_go/cmd/server/virtual-tools"
	"github.com/manishiitg/coding-agent-loop/agent_go/internal/terminals"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/schedulerstate"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/workflowtypes"
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

func TestMergeRuntimeStatePreservesRunningPulseForSameSession(t *testing.T) {
	runtimeStarted := time.Now().Add(-time.Minute).UTC()
	historyStarted := runtimeStarted.Add(time.Millisecond)
	duration := int64(1200)

	got := mergeRuntimeStateWithRuns(ScheduleRuntimeState{
		LastStatus:    "running",
		LastRunAt:     &runtimeStarted,
		LastSessionID: "session-1",
	}, "schedule-1", []ScheduleRunEntry{{
		ID:         "run-1",
		ScheduleID: "schedule-1",
		SessionID:  "session-1",
		Status:     "success",
		StartedAt:  historyStarted,
		DurationMs: &duration,
	}})

	if got.LastStatus != "running" {
		t.Fatalf("LastStatus = %q, want running while Pulse owns the session", got.LastStatus)
	}
	if got.LastSessionID != "session-1" {
		t.Fatalf("LastSessionID = %q, want session-1", got.LastSessionID)
	}
}

func TestMergeRuntimeStateAdoptsGenuinelyNewerRun(t *testing.T) {
	runtimeStarted := time.Now().Add(-time.Hour).UTC()
	historyStarted := runtimeStarted.Add(30 * time.Minute)

	got := mergeRuntimeStateWithRuns(ScheduleRuntimeState{
		LastStatus:    "running",
		LastRunAt:     &runtimeStarted,
		LastSessionID: "old-session",
	}, "schedule-1", []ScheduleRunEntry{{
		ID:         "run-2",
		ScheduleID: "schedule-1",
		SessionID:  "new-session",
		Status:     "success",
		StartedAt:  historyStarted,
	}})

	if got.LastStatus != "success" || got.LastSessionID != "new-session" {
		t.Fatalf("merged state = %+v, want newer persisted run", got)
	}
}

func TestUpdateRuntimeStateSerializesMutations(t *testing.T) {
	svc := NewSchedulerService(nil)
	const workers = 32
	const increments = 250

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < increments; j++ {
				svc.updateRuntimeState("schedule-1", func(state *ScheduleRuntimeState) {
					state.RunCount++
				})
			}
		}()
	}
	wg.Wait()

	got := svc.GetRuntimeState("schedule-1")
	if got.RunCount != workers*increments {
		t.Fatalf("RunCount = %d, want %d", got.RunCount, workers*increments)
	}
}

func TestRuntimeStateIsScopedAcrossUsersAndWorkflows(t *testing.T) {
	svc := NewSchedulerService(nil)
	userOneKey := multiAgentScheduleRuntimeKey("user-1", "builtin-org-pulse")
	userTwoKey := multiAgentScheduleRuntimeKey("user-2", "builtin-org-pulse")
	workflowOneKey := workflowScheduleRuntimeKey("Workflow/one", "copied-schedule")
	workflowTwoKey := workflowScheduleRuntimeKey("Workflow/two", "copied-schedule")

	svc.updateRuntimeState(userOneKey, func(state *ScheduleRuntimeState) {
		state.LastStatus = "running"
		state.LastSessionID = "user-1-session"
	})
	svc.updateRuntimeState(userTwoKey, func(state *ScheduleRuntimeState) {
		state.LastStatus = "success"
		state.LastSessionID = "user-2-session"
	})
	svc.updateRuntimeState(workflowOneKey, func(state *ScheduleRuntimeState) {
		state.LastStatus = "running"
	})
	svc.updateRuntimeState(workflowTwoKey, func(state *ScheduleRuntimeState) {
		state.LastStatus = "stopped"
	})

	if got := svc.getRuntimeStateByKey(userOneKey); got.LastSessionID != "user-1-session" || got.LastStatus != "running" {
		t.Fatalf("user 1 state = %+v", got)
	}
	if got := svc.getRuntimeStateByKey(userTwoKey); got.LastSessionID != "user-2-session" || got.LastStatus != "success" {
		t.Fatalf("user 2 state = %+v", got)
	}
	if got := svc.getRuntimeStateByKey(workflowOneKey); got.LastStatus != "running" {
		t.Fatalf("workflow one state = %+v", got)
	}
	if got := svc.getRuntimeStateByKey(workflowTwoKey); got.LastStatus != "stopped" {
		t.Fatalf("workflow two state = %+v", got)
	}
	if got := svc.GetRuntimeState("builtin-org-pulse"); got.LastStatus != "" {
		t.Fatalf("ambiguous unscoped state = %+v, want empty", got)
	}
}

func TestScheduleStateLockKeyFromRuntimeKey(t *testing.T) {
	workflowKey := workflowScheduleRuntimeKey("/tmp/Workflow/demo", "daily")
	wantWorkflow := strings.Join([]string{"workflow", "/tmp/Workflow/demo"}, scheduleScopeSeparator)
	if got := scheduleStateLockKeyFromRuntimeKey(workflowKey); got != wantWorkflow {
		t.Fatalf("workflow lock key = %q, want %q", got, wantWorkflow)
	}
	multiAgentKey := multiAgentScheduleRuntimeKey("user-1", "daily")
	if got := scheduleStateLockKeyFromRuntimeKey(multiAgentKey); got != multiAgentKey {
		t.Fatalf("multi-agent lock key = %q, want %q", got, multiAgentKey)
	}
}

func TestStopRunningJobCancelsBeforeSessionStarts(t *testing.T) {
	store, err := schedulerstate.Open(filepath.Join(t.TempDir(), "schedule-state.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	svc := NewSchedulerService(nil)
	svc.stateStore = store
	sctx := buildScheduleContext("Workflow/demo", &WorkflowManifest{ID: "demo"}, WorkflowSchedule{ID: "daily"})
	runID := "run-before-session"
	if err := svc.claimScheduleRun(context.Background(), sctx, runID, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	runCtx := svc.registerScheduleRunContext(runID)
	runtimeKey := scheduleRuntimeKey(sctx)
	svc.updateRuntimeState(runtimeKey, func(state *ScheduleRuntimeState) {
		state.ActiveRunID = runID
		state.LastStatus = "running"
	})

	svc.stopRunningJob(runtimeKey, sctx.Schedule.ID)
	select {
	case <-runCtx.Done():
	case <-time.After(time.Second):
		t.Fatal("run context was not canceled")
	}
	run, err := store.GetRun(context.Background(), runID)
	if err != nil {
		t.Fatal(err)
	}
	if run.State != schedulerstate.StateStopped {
		t.Fatalf("durable state = %s, want stopped", run.State)
	}
	state := svc.getRuntimeStateByKey(runtimeKey)
	if state.LastStatus != "stopped" || state.ActiveRunID != "" {
		t.Fatalf("runtime state after stop = %+v", state)
	}
}

func TestScheduleRunPublicationRegistersCancelBeforeUnlock(t *testing.T) {
	store, err := schedulerstate.Open(filepath.Join(t.TempDir(), "schedule-state.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	svc := NewSchedulerService(nil)
	svc.stateStore = store
	sctx := buildScheduleContext("Workflow/demo", &WorkflowManifest{ID: "demo"}, WorkflowSchedule{ID: "daily"})
	runID := "atomic-publication"
	startedAt := time.Now().UTC()
	if err := svc.claimScheduleRun(context.Background(), sctx, runID, startedAt); err != nil {
		t.Fatal(err)
	}

	runtimeKey := scheduleRuntimeKey(sctx)
	svc.runtimeStatesMu.Lock()
	state := svc.getRuntimeStateLocked(runtimeKey)
	runCtx := svc.activateScheduleRunLocked(state, runID, startedAt)
	stopStarted := make(chan struct{})
	stopDone := make(chan struct{})
	go func() {
		close(stopStarted)
		svc.stopRunningJob(runtimeKey, sctx.Schedule.ID)
		close(stopDone)
	}()
	<-stopStarted
	select {
	case <-stopDone:
		t.Fatal("stop completed before active run publication released its lock")
	case <-time.After(20 * time.Millisecond):
	}
	svc.runtimeStatesMu.Unlock()

	select {
	case <-runCtx.Done():
	case <-time.After(time.Second):
		t.Fatal("stop did not cancel the atomically published run context")
	}
	select {
	case <-stopDone:
	case <-time.After(time.Second):
		t.Fatal("stop did not complete")
	}
}

func TestStopBetweenReservationAndDurableClaimPreventsExecution(t *testing.T) {
	store, err := schedulerstate.Open(filepath.Join(t.TempDir(), "schedule-state.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	svc := NewSchedulerService(nil)
	svc.stateStore = store
	sctx := buildScheduleContext("Workflow/demo", &WorkflowManifest{ID: "demo"}, WorkflowSchedule{ID: "daily"})
	runtimeKey := scheduleRuntimeKey(sctx)
	runID := "stop-during-claim"
	startedAt := time.Now().UTC()

	svc.runtimeStatesMu.Lock()
	state := svc.getRuntimeStateLocked(runtimeKey)
	runCtx := svc.activateScheduleRunLocked(state, runID, startedAt)
	svc.runtimeStatesMu.Unlock()

	// This is the historical race window: Stop sees the reservation before the
	// SQLite run row exists.
	svc.stopRunningJob(runtimeKey, sctx.Schedule.ID)
	if err := svc.claimScheduleRun(context.Background(), sctx, runID, startedAt); err != nil {
		t.Fatalf("durable claim after stop: %v", err)
	}
	if !svc.abortCanceledScheduleRunBeforeStart(runCtx, sctx, runtimeKey, runID) {
		t.Fatal("canceled reservation was allowed to start")
	}

	run, err := store.GetRun(context.Background(), runID)
	if err != nil {
		t.Fatal(err)
	}
	if run.State != schedulerstate.StateStopped {
		t.Fatalf("durable state = %s, want stopped", run.State)
	}
}

func TestTriggerMultiAgentNowFindsBuiltinWithoutScheduleFile(t *testing.T) {
	workspace := httptest.NewServer(&mockWorkspaceAPI{files: map[string]string{}})
	defer workspace.Close()
	t.Setenv("WORKSPACE_API_URL", workspace.URL)

	svc := NewSchedulerService(nil)
	userID := "user-without-schedule-file"
	svc.updateRuntimeState(multiAgentScheduleRuntimeKey(userID, builtinOrgPulseID), func(state *ScheduleRuntimeState) {
		state.LastStatus = "running"
		state.LastSessionID = "existing-session"
	})

	_, err := svc.TriggerMultiAgentNow(userID, builtinOrgPulseID)
	if err == nil || !strings.Contains(err.Error(), "job is already running") {
		t.Fatalf("TriggerMultiAgentNow() error = %v, want builtin resolution followed by running guard", err)
	}
}

func TestGetRuntimeStateForUserReconcilesStaleRun(t *testing.T) {
	workspace := httptest.NewServer(&mockWorkspaceAPI{files: map[string]string{}})
	defer workspace.Close()
	t.Setenv("WORKSPACE_API_URL", workspace.URL)

	userID := "user-1"
	scheduleID := "daily"
	if err := AppendMultiAgentScheduleRun(context.Background(), userID, &ScheduleRunEntry{
		ID:         "stale-run",
		ScheduleID: scheduleID,
		SessionID:  "missing-session",
		Status:     "running",
		StartedAt:  time.Now().Add(-time.Minute).UTC(),
	}); err != nil {
		t.Fatal(err)
	}

	svc := NewSchedulerService(&StreamingAPI{})
	state := svc.GetRuntimeStateForUser(userID, scheduleID)
	if state.LastStatus != "error" || !strings.Contains(state.LastError, "session not active") {
		t.Fatalf("reconciled state = %+v, want stale run finalized as error", state)
	}
	runs, err := ReadMultiAgentScheduleRuns(context.Background(), userID)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 || runs[0].Status != "error" || runs[0].CompletedAt == nil {
		t.Fatalf("persisted runs = %+v, want finalized stale run", runs)
	}
}

func TestScheduleConfigFingerprintChangesWithCapabilities(t *testing.T) {
	sctx := buildMultiAgentScheduleContext("user-1", WorkflowSchedule{ID: "daily", Enabled: true, CronExpression: "0 9 * * *", Timezone: "UTC"}, WorkflowCapabilities{})
	first := scheduleConfigFingerprint(sctx)
	second := scheduleConfigFingerprint(sctx)
	if first == "" || first != second {
		t.Fatalf("fingerprint should be stable and non-empty: %q %q", first, second)
	}
	sctx.Capabilities.SelectedSkills = []string{"research"}
	if changed := scheduleConfigFingerprint(sctx); changed == first {
		t.Fatal("capability change did not change schedule fingerprint")
	}
}

func TestScheduleWithReloadedCalendarItemUsesLatestOverrides(t *testing.T) {
	requested := &CalendarScheduleItem{ID: "item-1", Date: "2030-01-01", Time: "09:00", Messages: []string{"old"}}
	sched := WorkflowSchedule{
		ID: "calendar", ScheduleType: "calendar", Timezone: "UTC",
		Messages:      []string{"base"},
		CalendarItems: []CalendarScheduleItem{{ID: "item-1", Date: requested.Date, Time: requested.Time, Messages: []string{"new"}}},
	}
	resolved, item, ok := scheduleWithReloadedCalendarItem(sched, requested)
	if !ok || item == nil {
		t.Fatal("expected calendar item to resolve")
	}
	if got := strings.Join(resolved.Messages, ","); got != "new" {
		t.Fatalf("resolved messages = %q, want latest override", got)
	}
}

func TestLoadScheduleReplacesCalendarRegistrations(t *testing.T) {
	svc := NewSchedulerService(nil)
	when := time.Now().UTC().Add(24 * time.Hour)
	item := CalendarScheduleItem{ID: "item-1", Date: when.Format("2006-01-02"), Time: when.Format("15:04"), Messages: []string{"old"}}
	sched := WorkflowSchedule{ID: "calendar", Name: "Calendar", Enabled: true, ScheduleType: "calendar", Timezone: "UTC", CalendarItems: []CalendarScheduleItem{item}}
	if err := svc.LoadSchedule(buildMultiAgentScheduleContext("user-1", sched, WorkflowCapabilities{})); err != nil {
		t.Fatalf("initial load: %v", err)
	}

	sched.CalendarItems[0].Messages = []string{"new"}
	if err := svc.LoadSchedule(buildMultiAgentScheduleContext("user-1", sched, WorkflowCapabilities{})); err != nil {
		t.Fatalf("reload: %v", err)
	}

	keyPrefix := multiAgentScheduleRuntimeKey("user-1", sched.ID) + "__cal__"
	matching := 0
	for key, job := range svc.jobs {
		if !strings.HasPrefix(key, keyPrefix) {
			continue
		}
		matching++
		if job.sctx.CalendarItem == nil || strings.Join(job.sctx.Schedule.Messages, ",") != "new" {
			t.Fatalf("calendar registration did not retain latest item override: %+v", job.sctx)
		}
	}
	if matching != 1 {
		t.Fatalf("calendar registrations = %d, want 1", matching)
	}
}

func TestLoadScheduleDoesNotRememberInvalidCronFingerprint(t *testing.T) {
	svc := NewSchedulerService(nil)
	sctx := buildMultiAgentScheduleContext("user-1", WorkflowSchedule{
		ID: "daily", Enabled: true, CronExpression: "not-a-cron", Timezone: "UTC",
	}, WorkflowCapabilities{})
	runtimeKey := scheduleRuntimeKey(sctx)
	if err := svc.LoadSchedule(sctx); err == nil {
		t.Fatal("invalid cron unexpectedly loaded")
	}
	if _, ok := svc.scheduleFingerprints[runtimeKey]; ok {
		t.Fatal("invalid cron fingerprint should not suppress the next rescan")
	}

	sctx.Schedule.CronExpression = "0 9 * * *"
	if err := svc.LoadSchedule(sctx); err != nil {
		t.Fatalf("corrected cron was not retried: %v", err)
	}
	if _, ok := svc.scheduleFingerprints[runtimeKey]; !ok {
		t.Fatal("valid schedule fingerprint was not recorded")
	}
}

func TestRemoveJobDropsInactiveRuntimeState(t *testing.T) {
	svc := NewSchedulerService(nil)
	runtimeKey := workflowScheduleRuntimeKey("Workflow/demo", "daily")
	svc.updateRuntimeState(runtimeKey, func(state *ScheduleRuntimeState) {
		state.LastStatus = "success"
	})
	if err := svc.removeJobByKey(runtimeKey); err != nil {
		t.Fatal(err)
	}
	svc.runtimeStatesMu.RLock()
	_, exists := svc.runtimeStates[runtimeKey]
	svc.runtimeStatesMu.RUnlock()
	if exists {
		t.Fatal("removed schedule retained inactive runtime state")
	}
}

func TestLoadScheduleKeepsSameIDForDifferentUsers(t *testing.T) {
	svc := NewSchedulerService(nil)
	for _, userID := range []string{"user-1", "user-2"} {
		sctx := buildMultiAgentScheduleContext(userID, WorkflowSchedule{
			ID:             "builtin-org-pulse",
			Name:           "Org Pulse",
			Enabled:        true,
			CronExpression: "0 9 * * *",
			Timezone:       "UTC",
			Query:          "Run Org Pulse",
		}, WorkflowCapabilities{})
		if err := svc.LoadSchedule(sctx); err != nil {
			t.Fatalf("LoadSchedule(%s): %v", userID, err)
		}
	}

	svc.mu.Lock()
	defer svc.mu.Unlock()
	if len(svc.jobs) != 2 {
		t.Fatalf("len(jobs) = %d, want 2 scoped jobs", len(svc.jobs))
	}
	for _, userID := range []string{"user-1", "user-2"} {
		if _, ok := svc.jobs[multiAgentScheduleRuntimeKey(userID, "builtin-org-pulse")]; !ok {
			t.Fatalf("missing scoped job for %s", userID)
		}
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

func TestWorkflowScheduleListExposesWorkshopMode(t *testing.T) {
	workspacePath := "Workflow/social-media"
	manifest := &WorkflowManifest{
		SchemaVersion: WorkflowManifestSchemaVersion,
		ID:            "social-media",
		Label:         "Social Media",
		Schedules: []WorkflowSchedule{
			{
				ID:             "run-schedule",
				Name:           "Daily publish",
				CronExpression: "0 9 * * *",
				Timezone:       "Asia/Kolkata",
				Enabled:        true,
				GroupNames:     []string{"group-1"},
				Mode:           "workshop",
				WorkshopMode:   "run",
			},
			{
				ID:             "optimizer-schedule",
				Name:           "Goal Advisor",
				CronExpression: "0 23 * * 1,4",
				Timezone:       "Asia/Kolkata",
				Enabled:        true,
				GroupNames:     []string{"group-1"},
				Mode:           "workshop",
				WorkshopMode:   "optimizer",
			},
		},
	}
	manifestJSON, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	workspace := httptest.NewServer(&mockWorkspaceAPI{files: map[string]string{
		workspacePath + "/workflow.json": string(manifestJSON),
	}})
	defer workspace.Close()
	t.Setenv("WORKSPACE_API_URL", workspace.URL)

	callbacks := (&StreamingAPI{}).buildSchedulerCallbacks()
	out, err := callbacks.ListSchedules(context.Background(), workspacePath)
	if err != nil {
		t.Fatalf("ListSchedules() error = %v", err)
	}
	for _, want := range []string{
		"## Schedules (2 found)",
		"### Daily publish",
		"- **Mode**: `workshop`",
		"- **Workshop Mode**: `run`",
		"### Goal Advisor",
		"- **Workshop Mode**: `optimizer`",
		"- **Type**: cron",
		"- **Cron**: `0 23 * * 1,4`",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("schedule list missing %q:\n%s", want, out)
		}
	}
}

func TestShouldUpdateChiefTaskReport(t *testing.T) {
	tests := []struct {
		name string
		sctx *ScheduleContext
		want bool
	}{
		{
			name: "normal chief schedule updates task report",
			sctx: &ScheduleContext{
				SourceType: "multi-agent",
				Schedule: WorkflowSchedule{
					ID:          "weekly-market-review",
					Name:        "Weekly market review",
					Description: "Review three workflows and recommend changes",
					Query:       "Prepare a cross-workflow recommendation report.",
				},
			},
			want: true,
		},
		{
			name: "workflow schedule does not update chief task report",
			sctx: &ScheduleContext{
				SourceType: "workflow",
				Schedule:   WorkflowSchedule{ID: "daily-run", Name: "Daily run"},
			},
			want: false,
		},
		{
			name: "builtin org pulse is excluded",
			sctx: &ScheduleContext{
				SourceType: "multi-agent",
				Schedule:   WorkflowSchedule{ID: builtinOrgPulseID, Name: "Daily Org Pulse"},
			},
			want: false,
		},
		{
			name: "org pulse duplicate is excluded",
			sctx: &ScheduleContext{
				SourceType: "multi-agent",
				Schedule:   WorkflowSchedule{ID: "custom-pulse", Name: "Daily Org Pulse scan"},
			},
			want: false,
		},
		{
			name: "deprecated builtin memory schedule is excluded",
			sctx: &ScheduleContext{
				SourceType: "multi-agent",
				Schedule:   WorkflowSchedule{ID: deprecatedAutoEnrichMemoryID, Name: "Auto-enrich memory"},
			},
			want: false,
		},
		{
			name: "memory-like schedule is excluded",
			sctx: &ScheduleContext{
				SourceType: "multi-agent",
				Schedule: WorkflowSchedule{
					ID:    "custom-memory",
					Name:  "Memory enrichment",
					Query: "Run enrich_memory for recent conversations.",
				},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldUpdateChiefTaskReport(tt.sctx); got != tt.want {
				t.Fatalf("shouldUpdateChiefTaskReport() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBuildChiefTaskReportUpdateMessageUsesSingleSharedTaskHTML(t *testing.T) {
	startedAt := time.Date(2026, 7, 4, 10, 15, 0, 0, time.UTC)
	completedAt := startedAt.Add(2 * time.Minute)
	sctx := &ScheduleContext{
		SourceType: "multi-agent",
		Schedule: WorkflowSchedule{
			ID:             "weekly-market-review",
			Name:           "Weekly market review",
			Description:    "Review three workflows and recommend changes",
			Query:          "Prepare a cross-workflow recommendation report.",
			CronExpression: "0 9 * * 1",
			Timezone:       "Asia/Kolkata",
		},
	}

	msg := buildChiefTaskReportUpdateMessage(sctx, "run-123", "success", "", 120000, startedAt, completedAt, "session-abc")
	for _, want := range []string{
		`get_reference_doc(kind="chief-task-report")`,
		"Update the single shared Tasks page at pulse/task.html",
		"Do not create per-task files",
		"Do not edit pulse/org-pulse.html, pulse/goals.html",
		"schedule_id: weekly-market-review",
		"schedule_name: Weekly market review",
		"run_id: run-123",
		"session_id: session-abc",
		"status: success",
		"Prepare a cross-workflow recommendation report.",
		"Prepend one .task-entry",
		"key findings to reuse",
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("task report update message missing %q:\n%s", want, msg)
		}
	}
}

func TestWithChiefTaskRunContextAddsPriorTaskReportInstruction(t *testing.T) {
	sctx := &ScheduleContext{
		SourceType: "multi-agent",
		Schedule: WorkflowSchedule{
			ID:    "weekly-market-review",
			Name:  "Weekly market review",
			Query: "Prepare a cross-workflow recommendation report.",
		},
	}

	msg := withChiefTaskRunContext(sctx, sctx.Schedule.Query)
	for _, want := range []string{
		"NORMAL CHIEF OF STAFF TASK RUN",
		"read pulse/task.html if it exists",
		`data-schedule-id="weekly-market-review"`,
		"key findings",
		"durable context",
		"Do not use or update Chief of Staff memory tools/files",
		"Prepare a cross-workflow recommendation report.",
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("task run context missing %q:\n%s", want, msg)
		}
	}
}

func TestWithChiefTaskRunContextSkipsOrgPulse(t *testing.T) {
	sctx := &ScheduleContext{
		SourceType: "multi-agent",
		Schedule:   WorkflowSchedule{ID: builtinOrgPulseID, Name: "Daily Org Pulse"},
	}

	const query = "Run Org Pulse."
	if got := withChiefTaskRunContext(sctx, query); got != query {
		t.Fatalf("withChiefTaskRunContext() = %q, want original query", got)
	}
}

func TestPostRunMonitorUsesDynamicModulesAndSingleFinalizer(t *testing.T) {
	steps := postRunMonitorSteps()
	if got := len(steps); got != 12 {
		t.Fatalf("postRunMonitorSteps() length = %d, want 12", got)
	}
	for i, want := range []string{"gate", "bug-review", "artifact", "learning-health", "knowledgebase-health", "db-health", "eval-health", "report-health", "cost-llm-time", "llm-ops-review", "goal-advisor", "finalize"} {
		if got := steps[i].label; got != want {
			t.Fatalf("postRunMonitorSteps()[%d].label = %q, want %q", i, got, want)
		}
	}

	var gate string
	var bugReview string
	var artifact string
	var reportHealth string
	var evalHealth string
	var learningHealth string
	var kbHealth string
	var dbHealth string
	var cost string
	var llmOps string
	var goalAdvisor string
	var finalizer string
	for _, step := range steps {
		if step.label == "gate" {
			gate = step.query
		}
		if step.label == "bug-review" {
			bugReview = step.query
		}
		if step.label == "artifact" {
			artifact = step.query
		}
		if step.label == "report-health" {
			reportHealth = step.query
		}
		if step.label == "eval-health" {
			evalHealth = step.query
		}
		if step.label == "learning-health" {
			learningHealth = step.query
		}
		if step.label == "knowledgebase-health" {
			kbHealth = step.query
		}
		if step.label == "db-health" {
			dbHealth = step.query
		}
		if step.label == "cost-llm-time" {
			cost = step.query
		}
		if step.label == "llm-ops-review" {
			llmOps = step.query
		}
		if step.label == "goal-advisor" {
			goalAdvisor = step.query
		}
		if step.label == "finalize" {
			finalizer = step.query
		}
	}
	if gate == "" {
		t.Fatal("gate step not found")
	}
	if bugReview == "" {
		t.Fatal("bug-review step not found")
	}
	if artifact == "" {
		t.Fatal("artifact step not found")
	}
	if reportHealth == "" {
		t.Fatal("report-health step not found")
	}
	if evalHealth == "" {
		t.Fatal("eval-health step not found")
	}
	if learningHealth == "" {
		t.Fatal("learning-health step not found")
	}
	if kbHealth == "" {
		t.Fatal("knowledgebase-health step not found")
	}
	if dbHealth == "" {
		t.Fatal("db-health step not found")
	}
	if cost == "" {
		t.Fatal("cost step not found")
	}
	if llmOps == "" {
		t.Fatal("llm-ops-review step not found")
	}
	if goalAdvisor == "" {
		t.Fatal("goal-advisor step not found")
	}
	if finalizer == "" {
		t.Fatal("finalizer step not found")
	}
	modulePrompts := map[string]string{
		"bug_review":           bugReview,
		"artifact_review":      artifact,
		"report_health":        reportHealth,
		"eval_health":          evalHealth,
		"learning_health":      learningHealth,
		"knowledgebase_health": kbHealth,
		"db_health":            dbHealth,
		"cost_llm_time":        cost,
		"llm_ops_review":       llmOps,
		"goal_advisor":         goalAdvisor,
	}
	for module, prompt := range modulePrompts {
		for _, want := range []string{
			"PULSE CONSOLIDATED REVIEW PROTOCOL",
			`get_reference_doc(kind="post-run-monitor")`,
			"READ-ONLY REVIEW",
			"one parallel tool-call batch",
			"Pulse Fixer",
			"only writer",
			"improve-learnings, improve-knowledge, improve-database, improve-report, and improve-evaluation",
			"Do not use run_in_background",
			"overrides any legacy nested-agent launcher wording",
		} {
			if !strings.Contains(prompt, want) {
				t.Fatalf("%s module missing consolidated review protocol %q:\n%s", module, want, prompt)
			}
		}
	}
	if strings.Contains(gate, "call harden_workflow(") || strings.Contains(gate, "call improve_learnings(") {
		t.Fatalf("gate step should not run selected modules directly:\n%s", gate)
	}
	for _, want := range []string{
		"PULSE GATE / WORKLIST",
		"get_pulse_module_state",
		"record_pulse_worklist exactly once",
		"High-frequency workflows should normally roll up cost/time checks",
		"Treat llm_ops_review as a low-frequency coaching pass",
		"A correct abstention or green eval is execution evidence, not goal progress",
		"mark both bug_review and goal_advisor due when appropriate",
		"Do not use 'wait for a clean run' as an indefinite strategy cooldown",
		"retained cross-run goal evidence",
		"Meeting a target is not a permanent skip",
		"optimization-headroom review",
		"meaningful outcome-bearing runs",
		"Once that headroom checkpoint arrives",
		"Never allow more than one active advisor experiment",
		"data-review-after",
	} {
		if !strings.Contains(gate, want) {
			t.Fatalf("gate step missing %q:\n%s", want, gate)
		}
	}
	for _, want := range []string{
		"bug_review",
		"artifact_review",
		"report_health",
		"eval_health",
		"learning_health",
		"knowledgebase_health",
		"db_health",
		"cost_llm_time",
		"llm_ops_review",
		"goal_advisor",
		"lock/unlock decisions",
		"Goal Advisor does not do routine Bug Review/KB/learnings/DB cleanup",
	} {
		if !strings.Contains(gate, want) {
			t.Fatalf("gate step missing module/gating text %q:\n%s", want, gate)
		}
	}
	if !strings.Contains(bugReview, "PULSE MODULE — BUG REVIEW") {
		t.Fatalf("bug-review step should be the Bug Review module:\n%s", bugReview)
	}
	for _, want := range []string{
		"read-only reliability review",
		"Pulse Fixer",
		"applies safe fixes sequentially",
		"Bug fix",
		`module="bug_review"`,
		"mark_pulse_module_result",
	} {
		if !strings.Contains(bugReview, want) {
			t.Fatalf("bug-review step missing %q:\n%s", want, bugReview)
		}
	}
	if strings.Contains(bugReview, "harden_workflow") {
		t.Fatalf("bug-review step should not expose the removed harden tool:\n%s", bugReview)
	}
	for _, want := range []string{
		"PULSE MODULE — ARTIFACT REVIEW",
		`get_workflow_command_guidance(kind="review-artifact-drift"`,
		"read-only review separate from Bug Review",
		"mark_changelog_artifact_reviewed",
		"artifact drift",
		"mark_pulse_module_result",
	} {
		if !strings.Contains(artifact, want) {
			t.Fatalf("artifact step missing %q:\n%s", want, artifact)
		}
	}
	for _, want := range []string{
		"PULSE MODULE — REPORT HEALTH",
		"improve-report checklist",
		"generic READ-ONLY REVIEW agent",
		"must not edit files",
		"parent Pulse Fixer applies and verifies",
		"Report fix",
		"mark_pulse_module_result",
	} {
		if !strings.Contains(reportHealth, want) {
			t.Fatalf("report health step missing %q:\n%s", want, reportHealth)
		}
	}
	for _, want := range []string{
		"PULSE MODULE — EVAL HEALTH",
		"improve-evaluation checklist",
		"generic READ-ONLY REVIEW agent",
		"TARGET_RUN_PATH",
		"must not edit files",
		"correctness-preserving",
		"stale-evidence rejection",
		"existing human-input flow before changing goal meaning",
		"Eval fix",
		"mark_pulse_module_result",
	} {
		if !strings.Contains(evalHealth, want) {
			t.Fatalf("eval health step missing %q:\n%s", want, evalHealth)
		}
	}
	for _, want := range []string{
		"PULSE MODULE — LEARNING HEALTH",
		"generic READ-ONLY REVIEW agent",
		"improve-learnings",
		"lock/unlock recommendations",
		"parent Pulse Fixer",
		"mark_pulse_module_result",
	} {
		if !strings.Contains(learningHealth, want) {
			t.Fatalf("learning health step missing %q:\n%s", want, learningHealth)
		}
	}
	for _, want := range []string{
		"PULSE MODULE — KNOWLEDGEBASE HEALTH",
		"generic READ-ONLY REVIEW agent",
		"improve-knowledge",
		"never rewrite knowledgebase/context",
		"parent Pulse Fixer",
		"mark_pulse_module_result",
	} {
		if !strings.Contains(kbHealth, want) {
			t.Fatalf("knowledgebase health step missing %q:\n%s", want, kbHealth)
		}
	}
	for _, want := range []string{
		"PULSE MODULE — DB HEALTH",
		"generic READ-ONLY REVIEW agent",
		"improve-database",
		"db/README.md",
		"parent Pulse Fixer",
		"mark_pulse_module_result",
	} {
		if !strings.Contains(dbHealth, want) {
			t.Fatalf("db health step missing %q:\n%s", want, dbHealth)
		}
	}
	for _, removed := range []string{"improve_learnings", "improve_kb", "improve_db"} {
		if strings.Contains(learningHealth, removed) || strings.Contains(kbHealth, removed) || strings.Contains(dbHealth, removed) {
			t.Fatalf("Pulse module prompts must not reference removed dedicated tool %q", removed)
		}
	}
	for _, want := range []string{
		"PULSE MODULE — COST / LLM / TIME",
		"generic READ-ONLY REVIEW agent",
		"costs/execution",
		"costs/evaluation",
		"costs/phase/token_usage.json",
		"timing summaries",
		"builder/card.cost.html",
		"must not edit files",
		"parent Pulse Fixer updates",
		"mark_pulse_module_result",
	} {
		if !strings.Contains(cost, want) {
			t.Fatalf("cost step missing %q:\n%s", want, cost)
		}
	}
	for _, want := range []string{
		"PULSE MODULE — LLM + OPERATIONS REVIEW",
		"get_reference_doc(kind=\"llm-selection\")",
		"generic READ-ONLY REVIEW agent",
		"tier coverage/use",
		"publish/password",
		"must not edit configuration/files",
		"parent Pulse Fixer processes existing answered `llm-ops-` requests",
		"at most two material structured decision requests",
		"no configuration changes without exact approval",
		"module=\"llm_ops_review\"",
	} {
		if !strings.Contains(llmOps, want) {
			t.Fatalf("LLM/Ops review step missing %q:\n%s", want, llmOps)
		}
	}
	for _, want := range []string{
		"PULSE MODULE — GOAL ADVISOR",
		"read-only strategy advisor",
		"separate read-only critic",
		"healthy 10x/headroom",
		"one active advisor experiment",
		"never turn operational correctness issues",
		"route those findings to Bug Review or Eval Health",
		"parent Pulse Fixer consolidates advisor and critic results",
		"mark_pulse_module_result",
	} {
		if !strings.Contains(goalAdvisor, want) {
			t.Fatalf("goal advisor step missing %q:\n%s", want, goalAdvisor)
		}
	}
	for _, want := range []string{
		"PULSE FINALIZER",
		"confirm every module marked due",
		"consolidated READ-ONLY REVIEW plus single-fixer protocol",
		"Never treat a missing result as skipped or successful",
		"ONE turn",
		"mark_pulse_final_command_result",
		"DASHBOARD + QUESTIONS",
		"builder/card.health.html",
		"create_human_input_request",
		"BACKUP",
		"PUBLISH",
		"source hash matches last_source_hash",
		"NOTIFY",
		"once every run",
		"Bug/Goal state",
		"backup/publish status",
		"dashboard URL when live",
		"notify_user",
	} {
		if !strings.Contains(finalizer, want) {
			t.Fatalf("finalizer step missing %q:\n%s", want, finalizer)
		}
	}
}

func TestPulseEvalGuidanceSeparatesCorrectnessRepairsFromSemanticApproval(t *testing.T) {
	repoRoot := findRepoRoot(t)
	read := func(rel string) string {
		t.Helper()
		raw, err := os.ReadFile(filepath.Join(repoRoot, rel))
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		return string(raw)
	}

	evalGuidance := read("agent_go/cmd/server/guidance/templates/improve/improve-evaluation.md")
	for _, want := range []string{
		"CORRECTNESS REPAIR — recommend automatic application by the Pulse Fixer; no user question",
		"binding evidence to the current run/group instead of accepting an older receipt",
		"making missing, null, empty, stale, malformed, or provider-unconfirmed evidence fail closed",
		"SEMANTIC CHANGE — require user/business approval",
		"changing a success criterion, threshold, weight, rubric interpretation",
	} {
		if !strings.Contains(evalGuidance, want) {
			t.Fatalf("improve-evaluation guidance missing %q", want)
		}
	}
	if strings.Contains(evalGuidance, "Do not edit `evaluation/evaluation_plan.json` until the user confirms.") {
		t.Fatal("improve-evaluation guidance still contains blanket approval gate")
	}

	advisorGuidance := read("agent_go/cmd/server/guidance/templates/improve/goal-advisor.md")
	for _, want := range []string{
		"older receipt/artifact for the current run",
		"never turn it into a Goal Advisor proposal or human-input question",
		"Operational correctness and deterministic eval wiring are never advisor proposals",
	} {
		if !strings.Contains(advisorGuidance, want) {
			t.Fatalf("goal-advisor guidance missing %q", want)
		}
	}
}

func TestPostRunMonitorModuleStepsDiscouragePollingLoops(t *testing.T) {
	steps := postRunMonitorModuleSteps("pulse-test")
	for _, step := range steps {
		query := step.step.query
		for _, forbidden := range []string{
			"wait with query_step",
			"until complete",
			"until it completes",
			"sleep 30",
		} {
			if strings.Contains(query, forbidden) {
				t.Fatalf("%s step should not encourage polling loop phrase %q:\n%s", step.step.label, forbidden, query)
			}
		}
		if strings.Contains(query, "query_step") && !strings.Contains(query, "at most once") {
			t.Fatalf("%s step uses query_step without one-off limit:\n%s", step.step.label, query)
		}
	}
	if !strings.Contains(scheduledBackgroundNoPollingInstruction, "do not babysit") ||
		!strings.Contains(scheduledBackgroundNoPollingInstruction, "[AUTO-NOTIFICATION]") {
		t.Fatalf("scheduled no-polling instruction should explain yielding to auto-notification: %q", scheduledBackgroundNoPollingInstruction)
	}
}

func TestPulseStoreHealthModulesLoadAssumptionAudit(t *testing.T) {
	steps := postRunMonitorModuleSteps("pulse-test")
	wantModules := map[string]bool{
		pulseModuleLearningHealth:      false,
		pulseModuleKnowledgebaseHealth: false,
		pulseModuleDBHealth:            false,
	}
	for _, step := range steps {
		if _, ok := wantModules[step.module]; !ok {
			continue
		}
		if !strings.Contains(step.step.query, `get_reference_doc(kind="assumption-audit")`) {
			t.Fatalf("%s module does not load assumption-audit:\n%s", step.module, step.step.query)
		}
		if !strings.Contains(step.step.query, "Assumptions challenged") {
			t.Fatalf("%s module does not preserve unresolved assumption handoff", step.module)
		}
		wantModules[step.module] = true
	}
	for module, found := range wantModules {
		if !found {
			t.Fatalf("did not inspect expected module %s", module)
		}
	}
}

func TestPostRunMonitorPrependsWorkflowVersionUpgradeForOldManifest(t *testing.T) {
	steps := postRunMonitorStepsForManifest(&WorkflowManifest{Version: "1.0.0"})
	if got := len(steps); got != 21 {
		t.Fatalf("postRunMonitorStepsForManifest(old) length = %d, want 21", got)
	}
	if got := steps[0].label; got != "upgrade-1.0.1" {
		t.Fatalf("first step label = %q, want upgrade-1.0.1", got)
	}
	for _, want := range []string{
		"WORKFLOW VERSION UPGRADE v1.0.0 -> v1.0.1",
		`workflow.json "version" to "1.0.1"`,
		`get_reference_doc(kind="review-improve-log")`,
		`get_reference_doc(kind="publish-strategy")`,
		"password-protected static publish contract",
		"named secret only",
		"StatiCrypt",
		"Runloop dark password-gate styling",
	} {
		if !strings.Contains(steps[0].query, want) {
			t.Fatalf("upgrade step missing %q:\n%s", want, steps[0].query)
		}
	}
	if got := steps[1].label; got != "upgrade-1.0.2" {
		t.Fatalf("second step label = %q, want upgrade-1.0.2", got)
	}
	if got := steps[2].label; got != "upgrade-1.0.3" {
		t.Fatalf("third step label = %q, want upgrade-1.0.3", got)
	}
	if got := steps[3].label; got != "upgrade-1.0.4" {
		t.Fatalf("fourth step label = %q, want upgrade-1.0.4", got)
	}
	if got := steps[4].label; got != "upgrade-1.0.5" {
		t.Fatalf("fifth step label = %q, want upgrade-1.0.5", got)
	}
	if got := steps[5].label; got != "upgrade-1.0.6" {
		t.Fatalf("sixth step label = %q, want upgrade-1.0.6", got)
	}
	if got := steps[6].label; got != "upgrade-1.0.7" {
		t.Fatalf("seventh step label = %q, want upgrade-1.0.7", got)
	}
	if got := steps[7].label; got != "upgrade-1.0.8" {
		t.Fatalf("eighth step label = %q, want upgrade-1.0.8", got)
	}
	if got := steps[8].label; got != "upgrade-1.0.9" {
		t.Fatalf("ninth step label = %q, want upgrade-1.0.9", got)
	}
	if got := steps[9].label; got != "gate" {
		t.Fatalf("tenth step label = %q, want gate", got)
	}
}

func TestPostRunMonitorPrependsWorkflowVersionUpgradeForMissingVersion(t *testing.T) {
	steps := postRunMonitorStepsForManifest(&WorkflowManifest{})
	if got := len(steps); got != 21 {
		t.Fatalf("postRunMonitorStepsForManifest(missing version) length = %d, want 21", got)
	}
	if !strings.Contains(steps[0].query, `Current workflow.json version seen by scheduler: "1.0.0"`) {
		t.Fatalf("missing version should be treated as 1.0.0:\n%s", steps[0].query)
	}
}

func TestPostRunMonitorPrependsPublishGateUpgradeForVersion101Manifest(t *testing.T) {
	steps := postRunMonitorStepsForManifest(&WorkflowManifest{Version: "1.0.1"})
	if got := len(steps); got != 20 {
		t.Fatalf("postRunMonitorStepsForManifest(1.0.1) length = %d, want 20", got)
	}
	if got := steps[0].label; got != "upgrade-1.0.2" {
		t.Fatalf("first step label = %q, want upgrade-1.0.2", got)
	}
	for _, want := range []string{
		"WORKFLOW VERSION UPGRADE v1.0.1 -> v1.0.2",
		`workflow.json "version" to "1.0.2"`,
		`get_reference_doc(kind="publish-strategy")`,
		"Runloop dark password-gate contract",
		"default green/white StatiCrypt page",
		"normal verified publish turn will republish with the new gate",
	} {
		if !strings.Contains(steps[0].query, want) {
			t.Fatalf("publish gate upgrade step missing %q:\n%s", want, steps[0].query)
		}
	}
	if got := steps[1].label; got != "upgrade-1.0.3" {
		t.Fatalf("second step label = %q, want upgrade-1.0.3", got)
	}
	if got := steps[2].label; got != "upgrade-1.0.4" {
		t.Fatalf("third step label = %q, want upgrade-1.0.4", got)
	}
	if got := steps[3].label; got != "upgrade-1.0.5" {
		t.Fatalf("fourth step label = %q, want upgrade-1.0.5", got)
	}
	if got := steps[4].label; got != "upgrade-1.0.6" {
		t.Fatalf("fifth step label = %q, want upgrade-1.0.6", got)
	}
	if got := steps[5].label; got != "upgrade-1.0.7" {
		t.Fatalf("sixth step label = %q, want upgrade-1.0.7", got)
	}
	if got := steps[6].label; got != "upgrade-1.0.8" {
		t.Fatalf("seventh step label = %q, want upgrade-1.0.8", got)
	}
	if got := steps[7].label; got != "upgrade-1.0.9" {
		t.Fatalf("eighth step label = %q, want upgrade-1.0.9", got)
	}
	if got := steps[8].label; got != "gate" {
		t.Fatalf("ninth step label = %q, want gate", got)
	}
}

func TestPostRunMonitorPrependsHTMLReportUpgradeForVersion102Manifest(t *testing.T) {
	steps := postRunMonitorStepsForManifest(&WorkflowManifest{Version: "1.0.2"})
	if got := len(steps); got != 19 {
		t.Fatalf("postRunMonitorStepsForManifest(1.0.2) length = %d, want 19", got)
	}
	if got := steps[0].label; got != "upgrade-1.0.3" {
		t.Fatalf("first step label = %q, want upgrade-1.0.3", got)
	}
	for _, want := range []string{
		"WORKFLOW VERSION UPGRADE v1.0.2 -> v1.0.3",
		`reports/report_plan.json`,
		`db/reports/`,
		`window.report.query(sql)`,
		`kind "file"`,
		`renderFormat "html"`,
		"Remove legacy widget kinds",
		`workflow.json "version" to "1.0.3"`,
	} {
		if !strings.Contains(steps[0].query, want) {
			t.Fatalf("html report upgrade step missing %q:\n%s", want, steps[0].query)
		}
	}
	if got := steps[1].label; got != "upgrade-1.0.4" {
		t.Fatalf("second step label = %q, want upgrade-1.0.4", got)
	}
	if got := steps[2].label; got != "upgrade-1.0.5" {
		t.Fatalf("third step label = %q, want upgrade-1.0.5", got)
	}
	if got := steps[3].label; got != "upgrade-1.0.6" {
		t.Fatalf("fourth step label = %q, want upgrade-1.0.6", got)
	}
	if got := steps[4].label; got != "upgrade-1.0.7" {
		t.Fatalf("fifth step label = %q, want upgrade-1.0.7", got)
	}
	if got := steps[5].label; got != "upgrade-1.0.8" {
		t.Fatalf("sixth step label = %q, want upgrade-1.0.8", got)
	}
	if got := steps[6].label; got != "upgrade-1.0.9" {
		t.Fatalf("seventh step label = %q, want upgrade-1.0.9", got)
	}
	if got := steps[7].label; got != "gate" {
		t.Fatalf("eighth step label = %q, want gate", got)
	}
}

func TestPostRunMonitorPrependsPulseReadabilityUpgradeForVersion103Manifest(t *testing.T) {
	steps := postRunMonitorStepsForManifest(&WorkflowManifest{Version: "1.0.3"})
	if got := len(steps); got != 18 {
		t.Fatalf("postRunMonitorStepsForManifest(1.0.3) length = %d, want 18", got)
	}
	if got := steps[0].label; got != "upgrade-1.0.4" {
		t.Fatalf("first step label = %q, want upgrade-1.0.4", got)
	}
	for _, want := range []string{
		"WORKFLOW VERSION UPGRADE v1.0.3 -> v1.0.4",
		`builder/improve.html`,
		`get_reference_doc(kind="review-improve-log")`,
		"What matters now",
		"recent runs: metadata row first",
		"full-width second row",
		`<!-- LOG ENTRIES: newest first -->`,
		`workflow.json "version" to "1.0.4"`,
	} {
		if !strings.Contains(steps[0].query, want) {
			t.Fatalf("pulse readability upgrade step missing %q:\n%s", want, steps[0].query)
		}
	}
	if got := steps[1].label; got != "upgrade-1.0.5" {
		t.Fatalf("second step label = %q, want upgrade-1.0.5", got)
	}
	if got := steps[2].label; got != "upgrade-1.0.6" {
		t.Fatalf("third step label = %q, want upgrade-1.0.6", got)
	}
	if got := steps[3].label; got != "upgrade-1.0.7" {
		t.Fatalf("fourth step label = %q, want upgrade-1.0.7", got)
	}
	if got := steps[4].label; got != "upgrade-1.0.8" {
		t.Fatalf("fifth step label = %q, want upgrade-1.0.8", got)
	}
	if got := steps[5].label; got != "upgrade-1.0.9" {
		t.Fatalf("sixth step label = %q, want upgrade-1.0.9", got)
	}
	if got := steps[6].label; got != "gate" {
		t.Fatalf("seventh step label = %q, want gate", got)
	}
}

func TestPostRunMonitorPrependsPulseFilterUpgradeForVersion104Manifest(t *testing.T) {
	steps := postRunMonitorStepsForManifest(&WorkflowManifest{Version: "1.0.4"})
	if got := len(steps); got != 17 {
		t.Fatalf("postRunMonitorStepsForManifest(1.0.4) length = %d, want 17", got)
	}
	if got := steps[0].label; got != "upgrade-1.0.5" {
		t.Fatalf("first step label = %q, want upgrade-1.0.5", got)
	}
	for _, want := range []string{
		"WORKFLOW VERSION UPGRADE v1.0.4 -> v1.0.5",
		`builder/improve.html`,
		`get_reference_doc(kind="review-improve-log")`,
		"Kind, Search, Reset",
		"do not add a date picker",
		`data-date="YYYY-MM-DD"`,
		`data-kind="run|monitor|artifact|decision|advisor|cos|open|user|note"`,
		`<!-- LOG ENTRIES: newest first -->`,
		`workflow.json "version" to "1.0.5"`,
	} {
		if !strings.Contains(steps[0].query, want) {
			t.Fatalf("pulse filter upgrade step missing %q:\n%s", want, steps[0].query)
		}
	}
	if got := steps[1].label; got != "upgrade-1.0.6" {
		t.Fatalf("second step label = %q, want upgrade-1.0.6", got)
	}
	if got := steps[2].label; got != "upgrade-1.0.7" {
		t.Fatalf("third step label = %q, want upgrade-1.0.7", got)
	}
	if got := steps[3].label; got != "upgrade-1.0.8" {
		t.Fatalf("fourth step label = %q, want upgrade-1.0.8", got)
	}
	if got := steps[4].label; got != "upgrade-1.0.9" {
		t.Fatalf("fifth step label = %q, want upgrade-1.0.9", got)
	}
	if got := steps[5].label; got != "gate" {
		t.Fatalf("sixth step label = %q, want gate", got)
	}
}

func TestPostRunMonitorPrependsRichPulseWidgetUpgradeForVersion105Manifest(t *testing.T) {
	steps := postRunMonitorStepsForManifest(&WorkflowManifest{Version: "1.0.5"})
	if got := len(steps); got != 16 {
		t.Fatalf("postRunMonitorStepsForManifest(1.0.5) length = %d, want 16", got)
	}
	if got := steps[0].label; got != "upgrade-1.0.6" {
		t.Fatalf("first step label = %q, want upgrade-1.0.6", got)
	}
	for _, want := range []string{
		"WORKFLOW VERSION UPGRADE v1.0.5 -> v1.0.6",
		`builder/improve.html`,
		`get_reference_doc(kind="review-improve-log")`,
		"What matters now widget cards",
		"color-coded signal tiles",
		".tile.ok",
		`<!-- LOG ENTRIES: newest first -->`,
		`workflow.json "version" to "1.0.6"`,
	} {
		if !strings.Contains(steps[0].query, want) {
			t.Fatalf("rich pulse widget upgrade step missing %q:\n%s", want, steps[0].query)
		}
	}
	if got := steps[1].label; got != "upgrade-1.0.7" {
		t.Fatalf("second step label = %q, want upgrade-1.0.7", got)
	}
	if got := steps[2].label; got != "upgrade-1.0.8" {
		t.Fatalf("third step label = %q, want upgrade-1.0.8", got)
	}
	if got := steps[3].label; got != "upgrade-1.0.9" {
		t.Fatalf("fourth step label = %q, want upgrade-1.0.9", got)
	}
	if got := steps[4].label; got != "gate" {
		t.Fatalf("fifth step label = %q, want gate", got)
	}
}

func TestPostRunMonitorPrependsLegacyOptimizerCleanupUpgradeForVersion106Manifest(t *testing.T) {
	steps := postRunMonitorStepsForManifest(&WorkflowManifest{Version: "1.0.6"})
	if got := len(steps); got != 15 {
		t.Fatalf("postRunMonitorStepsForManifest(1.0.6) length = %d, want 15", got)
	}
	if got := steps[0].label; got != "upgrade-1.0.7" {
		t.Fatalf("first step label = %q, want upgrade-1.0.7", got)
	}
	for _, want := range []string{
		"WORKFLOW VERSION UPGRADE v1.0.6 -> v1.0.7",
		"remove old separate Auto Improve / Goal Advisor optimizer schedules",
		`workshop_mode is "optimizer"`,
		"messages is missing/empty",
		"STEP 1/5 PRE-BACKUP",
		"Do not remove a schedule by name alone",
		"Preserve explicit custom optimizer jobs",
		"remove it from workflow.json schedules",
		"schedule-runs.json history",
		"post_run_monitor=true",
		`workflow.json "version" to "1.0.7"`,
		"do not publish",
	} {
		if !strings.Contains(steps[0].query, want) {
			t.Fatalf("legacy optimizer cleanup step missing %q:\n%s", want, steps[0].query)
		}
	}
	if got := steps[1].label; got != "upgrade-1.0.8" {
		t.Fatalf("second step label = %q, want upgrade-1.0.8", got)
	}
	if got := steps[2].label; got != "upgrade-1.0.9" {
		t.Fatalf("third step label = %q, want upgrade-1.0.9", got)
	}
	if got := steps[3].label; got != "gate" {
		t.Fatalf("fourth step label = %q, want gate", got)
	}
}

func TestPostRunMonitorPrependsPulseDatePickerCleanupForVersion107Manifest(t *testing.T) {
	steps := postRunMonitorStepsForManifest(&WorkflowManifest{Version: "1.0.7"})
	if got := len(steps); got != 14 {
		t.Fatalf("postRunMonitorStepsForManifest(1.0.7) length = %d, want 14", got)
	}
	if got := steps[0].label; got != "upgrade-1.0.8" {
		t.Fatalf("first step label = %q, want upgrade-1.0.8", got)
	}
	for _, want := range []string{
		"WORKFLOW VERSION UPGRADE v1.0.7 -> v1.0.8",
		"remove the date picker",
		`id="filter-date"`,
		"keep Kind, Search, Reset",
		"keep visible dates and data-date attributes",
		`workflow.json "version" to "1.0.8"`,
	} {
		if !strings.Contains(steps[0].query, want) {
			t.Fatalf("Pulse date-picker cleanup step missing %q:\n%s", want, steps[0].query)
		}
	}
	if got := steps[1].label; got != "upgrade-1.0.9" {
		t.Fatalf("second step label = %q, want upgrade-1.0.9", got)
	}
	if got := steps[2].label; got != "gate" {
		t.Fatalf("third step label = %q, want gate", got)
	}
}

func TestPostRunMonitorPrependsStableSoulAndPulseHierarchyUpgradeForVersion108Manifest(t *testing.T) {
	steps := postRunMonitorStepsForManifest(&WorkflowManifest{Version: "1.0.8"})
	if got := len(steps); got != 13 {
		t.Fatalf("postRunMonitorStepsForManifest(1.0.8) length = %d, want 13", got)
	}
	if got := steps[0].label; got != "upgrade-1.0.9" {
		t.Fatalf("first step label = %q, want upgrade-1.0.9", got)
	}
	for _, want := range []string{
		"WORKFLOW VERSION UPGRADE v1.0.8 -> v1.0.9",
		"keep soul/soul.md limited to stable intent",
		"explicit user-approved constraints",
		"remove architecture",
		"Assumptions challenged",
		"Today's outcome",
		`<details class="technical">`,
		"#pulse-agent-handoff",
		"must not repeat the user-facing report",
		"Needs your decision",
		`workflow.json "version" to "1.0.9"`,
	} {
		if !strings.Contains(steps[0].query, want) {
			t.Fatalf("stable soul/Pulse hierarchy upgrade missing %q:\n%s", want, steps[0].query)
		}
	}
	if got := steps[1].label; got != "gate" {
		t.Fatalf("second step label = %q, want gate", got)
	}
}

func TestPostRunMonitorDoesNotPrependWorkflowVersionUpgradeForCurrentManifest(t *testing.T) {
	steps := postRunMonitorStepsForManifest(&WorkflowManifest{Version: WorkflowContractCurrentVersion})
	if got := len(steps); got != 12 {
		t.Fatalf("postRunMonitorStepsForManifest(current) length = %d, want 12", got)
	}
	if got := steps[0].label; got != "gate" {
		t.Fatalf("first step label = %q, want gate", got)
	}
	if got := steps[2].label; got != "artifact" {
		t.Fatalf("third step label = %q, want artifact", got)
	}
	for _, want := range []string{
		"ARTIFACT REVIEW",
		`get_workflow_command_guidance(kind="review-artifact-drift"`,
		"read-only review separate from Bug Review",
		`mark_changelog_artifact_reviewed`,
		"artifact drift",
	} {
		if !strings.Contains(steps[2].query, want) {
			t.Fatalf("artifact step missing %q:\n%s", want, steps[2].query)
		}
	}
}

func TestWorkflowHasPendingPlanChangelogArtifactReview(t *testing.T) {
	tests := []struct {
		name      string
		files     map[string]string
		want      bool
		wantError bool
	}{
		{
			name:  "missing changelog folder",
			files: map[string]string{},
			want:  false,
		},
		{
			name: "unreviewed changelog entry",
			files: map[string]string{
				"Workflow/demo/planning/changelog/changelog-2026-07-02-06-12-46.json": `{"entries":[{"timestamp":"2026-07-02T06:12:46Z","tool":"update_regular_step","reason":"test","step_ids":["step-a"]}]}`,
			},
			want: true,
		},
		{
			name: "reviewed changelog entries",
			files: map[string]string{
				"Workflow/demo/planning/changelog/changelog-2026-07-02-06-12-46.json": `{"entries":[{"timestamp":"2026-07-02T06:12:46Z","tool":"update_regular_step","reason":"test","step_ids":["step-a"],"artifact_review":{"done":true,"reviewed_at":"2026-07-02T06:20:00Z","reviewed_by":"review_artifact_sync","result":"clean"}}]}`,
			},
			want: false,
		},
		{
			name: "one unreviewed entry keeps review pending",
			files: map[string]string{
				"Workflow/demo/planning/changelog/changelog-2026-07-02-06-12-46.json": `{"entries":[{"timestamp":"2026-07-02T06:12:46Z","tool":"update_regular_step","reason":"old","artifact_review":{"done":true}},{"timestamp":"2026-07-02T06:13:46Z","tool":"update_step_config","reason":"new"}]}`,
			},
			want: true,
		},
		{
			name: "malformed changelog is pending",
			files: map[string]string{
				"Workflow/demo/planning/changelog/changelog-2026-07-02-06-12-46.json": `{`,
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workspace := httptest.NewServer(&mockWorkspaceAPI{files: tt.files})
			defer workspace.Close()
			t.Setenv("WORKSPACE_API_URL", workspace.URL)

			got, err := workflowHasPendingPlanChangelogArtifactReview(context.Background(), "Workflow/demo")
			if tt.wantError && err == nil {
				t.Fatal("expected error, got nil")
			}
			if !tt.wantError && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("workflowHasPendingPlanChangelogArtifactReview() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSelectedPostRunMonitorModuleStepsUsesGateWorklist(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	t.Setenv("WORKSPACE_DOCS_PATH", root)
	workspacePath := "Workflow/demo"
	pulseRunID := "pulse-run-1"

	if _, err := recordPulseWorklist(ctx, workspacePath, pulseRunID, completePulseWorklistDecisions(map[string]PulseWorklistDecision{
		pulseModuleBugReview:    {Module: pulseModuleBugReview, Due: true, Reason: "A step failed.", Evidence: []string{"runs/latest"}},
		pulseModuleCostLLMTime:  {Module: pulseModuleCostLLMTime, Due: true, Reason: "The planned cost rollup is due."},
		pulseModuleLLMOpsReview: {Module: pulseModuleLLMOpsReview, Due: true, Reason: "The resolved tier configuration changed."},
		pulseModuleGoalAdvisor:  {Module: pulseModuleGoalAdvisor, Due: true, Reason: "Goal drift persisted across runs."},
	})); err != nil {
		t.Fatalf("record worklist: %v", err)
	}

	s := NewSchedulerService(nil)
	steps := s.selectedPostRunMonitorModuleSteps(ctx, &ScheduleContext{WorkspacePath: workspacePath}, pulseRunID)
	got := postRunStepLabels(steps)
	want := []string{"pre-backup", "bug-review", "cost-llm-time", "llm-ops-review", "goal-advisor", "finalize"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("selected labels = %#v, want %#v", got, want)
	}
}

func TestPulseBackupRunsOnlyInParentTurn(t *testing.T) {
	preBackup := postRunMonitorPreBackupStep("pulse-run-1").query
	finalizer := postRunMonitorFinalSteps("pulse-run-1")[0].query
	for name, message := range map[string]string{"pre-backup": preBackup, "finalizer": finalizer} {
		for _, required := range []string{"THIS parent", "Never delegate", "run_in_background", "call_generic_agent", ".git directory"} {
			if !strings.Contains(message, required) {
				t.Fatalf("%s message missing parent-only backup guard %q", name, required)
			}
		}
	}
}

func TestSelectedPostRunMonitorModuleStepsFallsBackConservatively(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	t.Setenv("WORKSPACE_DOCS_PATH", root)
	workspacePath := "Workflow/demo"
	pulseRunID := "pulse-run-missing-worklist"
	workspace := httptest.NewServer(&mockWorkspaceAPI{files: map[string]string{}})
	defer workspace.Close()
	t.Setenv("WORKSPACE_API_URL", workspace.URL)

	s := NewSchedulerService(nil)
	steps := s.selectedPostRunMonitorModuleSteps(ctx, &ScheduleContext{WorkspacePath: workspacePath}, pulseRunID)
	got := postRunStepLabels(steps)
	want := []string{"finalize"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("fallback labels = %#v, want %#v", got, want)
	}
}

func TestSelectedPostRunMonitorModuleStepsFallsBackForPartialWorklist(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	t.Setenv("WORKSPACE_DOCS_PATH", root)
	workspacePath := "Workflow/demo"
	pulseRunID := "pulse-run-partial-worklist"
	workspace := httptest.NewServer(&mockWorkspaceAPI{files: map[string]string{}})
	defer workspace.Close()
	t.Setenv("WORKSPACE_API_URL", workspace.URL)

	normalized, db, err := openPulseModuleStateDB(ctx, workspacePath, true)
	if err != nil {
		t.Fatalf("open pulse db: %v", err)
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx, `INSERT INTO pulse_module_state (
			module, workspace_path, last_pulse_run_id, last_checked_at,
			last_decision, last_reason, last_gate_decision, updated_at
		) VALUES (?, ?, ?, 'now', 'due', 'Partial stale row.', 'due', 'now')`,
		pulseModuleBugReview, normalized, pulseRunID); err != nil {
		t.Fatalf("insert partial worklist: %v", err)
	}

	s := NewSchedulerService(nil)
	steps := s.selectedPostRunMonitorModuleSteps(ctx, &ScheduleContext{WorkspacePath: workspacePath}, pulseRunID)
	got := postRunStepLabels(steps)
	want := []string{"pre-backup", "bug-review", "finalize"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("partial-worklist fallback labels = %#v, want %#v", got, want)
	}
}

func TestSelectedPostRunMonitorModuleStepsPartialWorklistKeepsDueGoalAdvisor(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	t.Setenv("WORKSPACE_DOCS_PATH", root)
	workspacePath := "Workflow/demo"
	pulseRunID := "pulse-run-partial-goal-advisor"
	workspace := httptest.NewServer(&mockWorkspaceAPI{files: map[string]string{}})
	defer workspace.Close()
	t.Setenv("WORKSPACE_API_URL", workspace.URL)

	normalized, db, err := openPulseModuleStateDB(ctx, workspacePath, true)
	if err != nil {
		t.Fatalf("open pulse db: %v", err)
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx, `INSERT INTO pulse_module_state (
			module, workspace_path, last_pulse_run_id, last_checked_at,
			last_decision, last_reason, last_gate_decision, updated_at
		) VALUES (?, ?, ?, 'now', 'due', 'Goal drift persisted.', 'due', 'now')`,
		pulseModuleGoalAdvisor, normalized, pulseRunID); err != nil {
		t.Fatalf("insert partial worklist: %v", err)
	}

	s := NewSchedulerService(nil)
	steps := s.selectedPostRunMonitorModuleSteps(ctx, &ScheduleContext{WorkspacePath: workspacePath}, pulseRunID)
	got := postRunStepLabels(steps)
	want := []string{"pre-backup", "goal-advisor", "finalize"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("partial-worklist fallback labels = %#v, want %#v", got, want)
	}
}

func postRunStepLabels(steps []postRunMonitorStep) []string {
	labels := make([]string, 0, len(steps))
	for _, step := range steps {
		labels = append(labels, step.label)
	}
	return labels
}

func TestOptimizerScheduleMessagesKeepsCustomMessages(t *testing.T) {
	stored := []string{`Do not ask for confirmation. Run this custom optimizer audit and stop. Compare with any Auto Improve history already logged.`}

	got := optimizerScheduleMessages(context.Background(), "Workflow/test", stored, []string{"prod"})
	if len(got) != 1 {
		t.Fatalf("optimizerScheduleMessages() length = %d, want 1", len(got))
	}
	if got[0] != stored[0] {
		t.Fatalf("optimizerScheduleMessages() = %#v, want stored custom message", got)
	}
}

func TestLegacyGoalAdvisorMessageQueueIgnoresCustomTopicMentions(t *testing.T) {
	stored := []string{`Run a custom optimizer audit for this workflow. Compare the result with Goal Advisor and Auto Improve history, then stop.`}
	if isLegacyGoalAdvisorMessageQueue(stored) {
		t.Fatalf("custom optimizer prompt was incorrectly classified as legacy: %q", stored[0])
	}
}

func TestOptimizerScheduleMessagesNoopsWhenNoStoredMessage(t *testing.T) {
	got := optimizerScheduleMessages(context.Background(), "Workflow/test", nil, []string{"group-a"})
	if len(got) != 1 {
		t.Fatalf("optimizerScheduleMessages(nil) length = %d, want 1", len(got))
	}
	for _, want := range []string{
		"optimizer schedule is no longer the product Goal Advisor loop",
		"Goal Advisor now runs as a Pulse-selected module",
		"legacy optimizer schedule should be disabled",
	} {
		if !strings.Contains(got[0], want) {
			t.Fatalf("optimizer no-op message missing %q:\n%s", want, got[0])
		}
	}
}

func TestExecuteWorkshopJobDisablesLegacyOptimizerBeforeStartingSession(t *testing.T) {
	ctx := context.Background()
	workspacePath := "Workflow/demo"
	manifest := &WorkflowManifest{
		SchemaVersion: WorkflowManifestSchemaVersion,
		ID:            "demo",
		Label:         "Demo",
		Schedules: []WorkflowSchedule{
			{
				ID:             "legacy-optimizer",
				Name:           "Goal Advisor",
				CronExpression: "0 23 * * *",
				Timezone:       "UTC",
				Enabled:        true,
				GroupNames:     []string{"group-1"},
				Mode:           "workshop",
				WorkshopMode:   "optimizer",
				Messages: []string{
					"STEP 1/5 — PRE-BACKUP",
					"STEP 2/5 — IMPROVE",
				},
			},
		},
	}
	manifestJSON, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	workspace := httptest.NewServer(&mockWorkspaceAPI{files: map[string]string{
		workspacePath + "/workflow.json": string(manifestJSON),
	}})
	defer workspace.Close()
	t.Setenv("WORKSPACE_API_URL", workspace.URL)

	s := NewSchedulerService(nil)
	_, _, err = s.executeWorkshopJob(ctx, &ScheduleContext{
		WorkspacePath: workspacePath,
		Schedule:      manifest.Schedules[0],
		SourceType:    "workflow",
	}, "")
	if err != nil {
		t.Fatalf("executeWorkshopJob() error = %v", err)
	}

	updated, found, err := ReadWorkflowManifest(ctx, workspacePath)
	if err != nil || !found {
		t.Fatalf("read updated manifest: found=%v err=%v", found, err)
	}
	if len(updated.Schedules) != 1 || updated.Schedules[0].Enabled {
		t.Fatalf("legacy optimizer schedule was not disabled: %+v", updated.Schedules)
	}
	if !updated.MonitorEnabled() {
		t.Fatal("legacy optimizer disable should enable Pulse/post_run_monitor")
	}
}

func TestOptimizerScheduleMessagesReplacesLegacyGoalAdvisorQueue(t *testing.T) {
	legacy := []string{
		"STEP 1/5 — PRE-BACKUP",
		"STEP 2/5 — IMPROVE",
		"STEP 3/5 — BACKUP FINAL STATE",
		"STEP 4/5 — PUBLISH",
		"STEP 5/5 — NOTIFY",
	}

	got := optimizerScheduleMessages(context.Background(), "Workflow/test", legacy, nil)
	if len(got) != 1 {
		t.Fatalf("optimizerScheduleMessages() length = %d, want 1", len(got))
	}
	if strings.Contains(strings.Join(got, "\n"), strings.Join(legacy, "\n")) {
		t.Fatalf("optimizerScheduleMessages() should replace legacy stored queues, got:\n%s", strings.Join(got, "\n"))
	}
	for _, want := range []string{
		"optimizer schedule is no longer the product Goal Advisor loop",
		"Pulse-selected module",
	} {
		if !strings.Contains(strings.Join(got, "\n"), want) {
			t.Fatalf("optimizerScheduleMessages() missing %q:\n%s", want, strings.Join(got, "\n"))
		}
	}
}

func TestApplyLLMAndSecretsToReqMapUsesAutoImproveOverrideOnlyForOptimizer(t *testing.T) {
	builder := &workflowtypes.AgentLLMConfig{Provider: "claude-code", ModelID: "claude-opus-4-6"}
	maintenance := &workflowtypes.AgentLLMConfig{Provider: "vertex", ModelID: "gemini-2.5-pro"}
	baseConfig := &workflowtypes.PresetLLMConfig{
		SchemaVersion:  workflowtypes.LLMConfigSchemaVersion,
		Mode:           workflowtypes.LLMConfigModeExplicit,
		BuilderLLM:     builder,
		MaintenanceLLM: maintenance,
		PulseLLM:       builder,
		TieredConfig:   &workflowtypes.TieredLLMConfig{Tier1: maintenance, Tier2: builder, Tier3: builder},
	}

	tests := []struct {
		name         string
		workshopMode string
		wantProvider string
		wantModelID  string
	}{
		{
			name:         "normal schedule uses workflow model",
			workshopMode: "run",
			wantProvider: "claude-code",
			wantModelID:  "claude-opus-4-6",
		},
		{
			name:         "optimizer schedule uses Goal Advisor override",
			workshopMode: "optimizer",
			wantProvider: "vertex",
			wantModelID:  "gemini-2.5-pro",
		},
		{
			name:         "optimizer mode is case insensitive",
			workshopMode: " OPTIMIZER ",
			wantProvider: "vertex",
			wantModelID:  "gemini-2.5-pro",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reqMap := map[string]interface{}{}
			(&SchedulerService{}).applyLLMAndSecretsToReqMap(context.Background(), reqMap, &ScheduleContext{
				Schedule: WorkflowSchedule{WorkshopMode: tt.workshopMode},
				Capabilities: WorkflowCapabilities{
					LLMConfig: baseConfig,
				},
			})

			llmConfig, ok := reqMap["llm_config"].(map[string]interface{})
			if !ok {
				t.Fatalf("llm_config missing or wrong type: %#v", reqMap["llm_config"])
			}
			primary, ok := llmConfig["primary"].(map[string]interface{})
			if !ok {
				t.Fatalf("llm_config.primary missing or wrong type: %#v", llmConfig["primary"])
			}
			if got := primary["provider"]; got != tt.wantProvider {
				t.Fatalf("provider = %#v, want %q", got, tt.wantProvider)
			}
			if got := primary["model_id"]; got != tt.wantModelID {
				t.Fatalf("model_id = %#v, want %q", got, tt.wantModelID)
			}
			if tt.workshopMode == "run" {
				if _, ok := reqMap["llm_config_source"]; ok {
					t.Fatalf("normal run set llm_config_source: %#v", reqMap["llm_config_source"])
				}
			} else if got := reqMap["llm_config_source"]; got != llmConfigSourceScheduledAutoImprove {
				t.Fatalf("llm_config_source = %#v, want %q", got, llmConfigSourceScheduledAutoImprove)
			}
		})
	}
}

func TestApplyLLMAndSecretsToReqMapUsesCodingAgentAutoImproveDefaultForOptimizer(t *testing.T) {
	reqMap := map[string]interface{}{}
	(&SchedulerService{}).applyLLMAndSecretsToReqMap(context.Background(), reqMap, &ScheduleContext{
		Schedule: WorkflowSchedule{WorkshopMode: "optimizer"},
		Capabilities: WorkflowCapabilities{
			LLMConfig: &workflowtypes.PresetLLMConfig{
				SchemaVersion: workflowtypes.LLMConfigSchemaVersion,
				Mode:          workflowtypes.LLMConfigModeProviderProfile,
				Provider:      "claude-code",
			},
		},
	})

	llmConfig, ok := reqMap["llm_config"].(map[string]interface{})
	if !ok {
		t.Fatalf("llm_config missing or wrong type: %#v", reqMap["llm_config"])
	}
	primary, ok := llmConfig["primary"].(map[string]interface{})
	if !ok {
		t.Fatalf("llm_config.primary missing or wrong type: %#v", llmConfig["primary"])
	}
	if got := primary["provider"]; got != "claude-code" {
		t.Fatalf("provider = %#v, want claude-code", got)
	}
	if got := primary["model_id"]; got != "claude-opus-4-8" {
		t.Fatalf("model_id = %#v, want claude-opus-4-8", got)
	}
	if got := reqMap["llm_config_source"]; got != llmConfigSourceScheduledAutoImprove {
		t.Fatalf("llm_config_source = %#v, want %q", got, llmConfigSourceScheduledAutoImprove)
	}
}

func TestApplyLLMAndSecretsToReqMapPreservesAutoImproveDefaultOptions(t *testing.T) {
	reqMap := map[string]interface{}{}
	(&SchedulerService{}).applyLLMAndSecretsToReqMap(context.Background(), reqMap, &ScheduleContext{
		Schedule: WorkflowSchedule{WorkshopMode: "optimizer"},
		Capabilities: WorkflowCapabilities{
			LLMConfig: &workflowtypes.PresetLLMConfig{
				SchemaVersion: workflowtypes.LLMConfigSchemaVersion,
				Mode:          workflowtypes.LLMConfigModeProviderProfile,
				Provider:      "codex-cli",
			},
		},
	})

	llmConfig, ok := reqMap["llm_config"].(map[string]interface{})
	if !ok {
		t.Fatalf("llm_config missing or wrong type: %#v", reqMap["llm_config"])
	}
	primary, ok := llmConfig["primary"].(map[string]interface{})
	if !ok {
		t.Fatalf("llm_config.primary missing or wrong type: %#v", llmConfig["primary"])
	}
	if got := primary["provider"]; got != "codex-cli" {
		t.Fatalf("provider = %#v, want codex-cli", got)
	}
	if got := primary["model_id"]; got != "gpt-5.6-sol" {
		t.Fatalf("model_id = %#v, want gpt-5.6-sol", got)
	}
	options, ok := primary["options"].(map[string]interface{})
	if !ok {
		t.Fatalf("options missing or wrong type: %#v", primary["options"])
	}
	if got := options["reasoning_effort"]; got != "xhigh" {
		t.Fatalf("reasoning_effort = %#v, want xhigh", got)
	}
	if got := reqMap["llm_config_source"]; got != llmConfigSourceScheduledAutoImprove {
		t.Fatalf("llm_config_source = %#v, want %q", got, llmConfigSourceScheduledAutoImprove)
	}
}

func TestApplyPulseLLMToReqMapUsesPulseOverrideWhenConfigured(t *testing.T) {
	reqMap := map[string]interface{}{}
	builder := &workflowtypes.AgentLLMConfig{Provider: "claude-code", ModelID: "claude-opus-4-6"}
	sctx := &ScheduleContext{
		Schedule: WorkflowSchedule{WorkshopMode: "run"},
		Capabilities: WorkflowCapabilities{
			LLMConfig: &workflowtypes.PresetLLMConfig{
				SchemaVersion:  workflowtypes.LLMConfigSchemaVersion,
				Mode:           workflowtypes.LLMConfigModeExplicit,
				BuilderLLM:     builder,
				MaintenanceLLM: builder,
				TieredConfig:   &workflowtypes.TieredLLMConfig{Tier1: builder, Tier2: builder, Tier3: builder},
				PulseLLM: &workflowtypes.AgentLLMConfig{
					Provider: "codex-cli",
					ModelID:  "gpt-5.5",
					Options:  map[string]interface{}{"reasoning_effort": "high"},
				},
			},
		},
	}

	svc := &SchedulerService{}
	svc.applyLLMAndSecretsToReqMap(context.Background(), reqMap, sctx)
	svc.applyPulseLLMToReqMap(reqMap, sctx, "test-session")

	llmConfig, ok := reqMap["llm_config"].(map[string]interface{})
	if !ok {
		t.Fatalf("llm_config missing or wrong type: %#v", reqMap["llm_config"])
	}
	primary, ok := llmConfig["primary"].(map[string]interface{})
	if !ok {
		t.Fatalf("llm_config.primary missing or wrong type: %#v", llmConfig["primary"])
	}
	if got := primary["provider"]; got != "codex-cli" {
		t.Fatalf("provider = %#v, want codex-cli", got)
	}
	if got := primary["model_id"]; got != "gpt-5.5" {
		t.Fatalf("model_id = %#v, want gpt-5.5", got)
	}
	if got := reqMap["llm_config_source"]; got != llmConfigSourceScheduledPulse {
		t.Fatalf("llm_config_source = %#v, want %q", got, llmConfigSourceScheduledPulse)
	}
	options, ok := primary["options"].(map[string]interface{})
	if !ok {
		t.Fatalf("options missing or wrong type: %#v", primary["options"])
	}
	if got := options["reasoning_effort"]; got != "high" {
		t.Fatalf("reasoning_effort = %#v, want high", got)
	}
	if got := reqMap["llm_config_source"]; got != llmConfigSourceScheduledPulse {
		t.Fatalf("llm_config_source = %#v, want %q", got, llmConfigSourceScheduledPulse)
	}
}

func TestApplyGoalAdvisorLLMToReqMapUsesAdvisorOverrideWhenConfigured(t *testing.T) {
	reqMap := map[string]interface{}{}
	builder := &workflowtypes.AgentLLMConfig{Provider: "claude-code", ModelID: "claude-sonnet-5"}
	sctx := &ScheduleContext{
		Schedule: WorkflowSchedule{WorkshopMode: "run"},
		Capabilities: WorkflowCapabilities{
			LLMConfig: &workflowtypes.PresetLLMConfig{
				SchemaVersion: workflowtypes.LLMConfigSchemaVersion,
				Mode:          workflowtypes.LLMConfigModeExplicit,
				BuilderLLM:    builder,
				MaintenanceLLM: &workflowtypes.AgentLLMConfig{
					Provider: "claude-code",
					ModelID:  "claude-opus-4-8",
					Options:  map[string]interface{}{"reasoning_effort": "high"},
				},
				PulseLLM: &workflowtypes.AgentLLMConfig{
					Provider: "claude-code",
					ModelID:  "claude-sonnet-5",
					Options:  map[string]interface{}{"reasoning_effort": "high"},
				},
				TieredConfig: &workflowtypes.TieredLLMConfig{Tier1: builder, Tier2: builder, Tier3: builder},
			},
		},
	}

	svc := &SchedulerService{}
	svc.applyLLMAndSecretsToReqMap(context.Background(), reqMap, sctx)
	svc.applyGoalAdvisorLLMToReqMap(reqMap, sctx, "test-session")

	llmConfig, ok := reqMap["llm_config"].(map[string]interface{})
	if !ok {
		t.Fatalf("llm_config missing or wrong type: %#v", reqMap["llm_config"])
	}
	primary, ok := llmConfig["primary"].(map[string]interface{})
	if !ok {
		t.Fatalf("llm_config.primary missing or wrong type: %#v", llmConfig["primary"])
	}
	if got := primary["provider"]; got != "claude-code" {
		t.Fatalf("provider = %#v, want claude-code", got)
	}
	if got := primary["model_id"]; got != "claude-opus-4-8" {
		t.Fatalf("model_id = %#v, want claude-opus-4-8", got)
	}
	if got := reqMap["llm_config_source"]; got != llmConfigSourceScheduledAutoImprove {
		t.Fatalf("llm_config_source = %#v, want %q", got, llmConfigSourceScheduledAutoImprove)
	}
}

func TestBuildWorkshopRequestDisablesLiveInputDeliveryForSchedulerTurns(t *testing.T) {
	svc := &SchedulerService{}
	sctx := &ScheduleContext{
		WorkflowID:    "wf_test",
		WorkspacePath: "Workflow/test",
		Schedule:      WorkflowSchedule{ID: "daily", Name: "Daily"},
		Capabilities:  WorkflowCapabilities{},
		SourceType:    "workflow",
	}

	reqMap := svc.buildWorkshopRequest(context.Background(), sctx)
	if got := reqMap["disable_live_input_delivery"]; got != true {
		t.Fatalf("disable_live_input_delivery = %#v, want true", got)
	}
}

func TestRefreshSessionTmuxSnapshotsForIdleCheckCapturesFreshPane(t *testing.T) {
	store := terminals.NewStore()
	sessionID := "session-scheduler-refresh"
	tmuxSession := "tmux-scheduler-refresh"
	store.HandleEvent(sessionID, terminalRouteChunkEvent(sessionID, "workflow-step:review-plan", tmuxSession, "old pane", 1))

	oldRunOutput := runTerminalTmuxOutputCommand
	defer func() { runTerminalTmuxOutputCommand = oldRunOutput }()
	var calls [][]string
	runTerminalTmuxOutputCommand = func(ctx context.Context, args ...string) (string, error) {
		calls = append(calls, append([]string(nil), args...))
		return "fresh pane\n❯", nil
	}

	svc := &SchedulerService{api: &StreamingAPI{terminalStore: store}}
	if err := svc.refreshSessionTmuxSnapshotsForIdleCheck(context.Background(), sessionID); err != nil {
		t.Fatalf("refreshSessionTmuxSnapshotsForIdleCheck returned error: %v", err)
	}
	if len(calls) != 1 {
		t.Fatalf("tmux capture calls = %d, want 1", len(calls))
	}
	snapshots := store.ListMetadata(sessionID)
	if len(snapshots) != 1 {
		t.Fatalf("snapshots = %d, want 1", len(snapshots))
	}
	if got := snapshots[0].Content; !strings.Contains(got, "fresh pane") {
		t.Fatalf("snapshot content = %q, want fresh capture", got)
	}
	if got := snapshots[0].ContentSource; got != "tmux_capture" {
		t.Fatalf("content source = %q, want tmux_capture", got)
	}
}

func TestRefreshSessionTmuxSnapshotsForIdleCheckMarksMissingPaneStale(t *testing.T) {
	store := terminals.NewStore()
	sessionID := "session-scheduler-missing"
	tmuxSession := "tmux-scheduler-missing"
	store.HandleEvent(sessionID, terminalRouteChunkEvent(sessionID, "workflow-step:review-plan", tmuxSession, "old pane", 1))

	oldRunOutput := runTerminalTmuxOutputCommand
	defer func() { runTerminalTmuxOutputCommand = oldRunOutput }()
	runTerminalTmuxOutputCommand = func(ctx context.Context, args ...string) (string, error) {
		return "", errors.New("can't find session: tmux-scheduler-missing")
	}

	svc := &SchedulerService{api: &StreamingAPI{terminalStore: store}}
	if err := svc.refreshSessionTmuxSnapshotsForIdleCheck(context.Background(), sessionID); err != nil {
		t.Fatalf("refreshSessionTmuxSnapshotsForIdleCheck returned error: %v", err)
	}
	snapshots := store.ListMetadata(sessionID)
	if len(snapshots) != 1 {
		t.Fatalf("snapshots = %d, want 1", len(snapshots))
	}
	if snapshots[0].Active {
		t.Fatalf("missing tmux snapshot should be inactive")
	}
	if got := snapshots[0].State; got != "stale" {
		t.Fatalf("state = %q, want stale", got)
	}
	if got := snapshots[0].TmuxSession; got != "" {
		t.Fatalf("tmux session = %q, want cleared", got)
	}
}

func TestWaitForWorkshopIdleRequiresTwoFreshIdleTmuxChecks(t *testing.T) {
	oldInterval := schedulerWorkshopIdlePollInterval
	schedulerWorkshopIdlePollInterval = time.Millisecond
	defer func() { schedulerWorkshopIdlePollInterval = oldInterval }()

	store := terminals.NewStore()
	sessionID := "session-scheduler-idle"
	tmuxSession := "tmux-scheduler-idle"
	store.HandleEvent(sessionID, terminalRouteChunkEvent(sessionID, "workflow-step:review-plan", tmuxSession, "old pane", 1))

	oldRunOutput := runTerminalTmuxOutputCommand
	defer func() { runTerminalTmuxOutputCommand = oldRunOutput }()
	calls := 0
	runTerminalTmuxOutputCommand = func(ctx context.Context, args ...string) (string, error) {
		calls++
		return "done\n❯", nil
	}

	svc := &SchedulerService{api: &StreamingAPI{terminalStore: store}}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := svc.waitForWorkshopIdle(ctx, sessionID); err != nil {
		t.Fatalf("waitForWorkshopIdle returned error: %v", err)
	}
	if calls != schedulerWorkshopIdleConsecutiveChecks {
		t.Fatalf("tmux captures = %d, want %d", calls, schedulerWorkshopIdleConsecutiveChecks)
	}
}

func TestWaitForWorkshopIdleAbortsStoppedSequenceBeforeNextMessage(t *testing.T) {
	api := &StreamingAPI{stoppedSessions: map[string]bool{"session-stopped": true}}
	svc := &SchedulerService{api: api}

	err := svc.waitForWorkshopIdle(context.Background(), "session-stopped")
	if !errors.Is(err, errWorkshopSequenceInterrupted) {
		t.Fatalf("error = %v, want errWorkshopSequenceInterrupted", err)
	}
}

func TestWaitForWorkshopIdleAbortsCanceledTurnBeforeNextMessage(t *testing.T) {
	api := &StreamingAPI{}
	api.markSessionTurnInterrupted("session-canceled-turn")
	svc := &SchedulerService{api: api}

	err := svc.waitForWorkshopIdle(context.Background(), "session-canceled-turn")
	if !errors.Is(err, errWorkshopSequenceInterrupted) {
		t.Fatalf("error = %v, want errWorkshopSequenceInterrupted", err)
	}
	if api.consumeSessionTurnInterrupted("session-canceled-turn") {
		t.Fatalf("interruption marker was not consumed by the scheduler wait")
	}
}

func TestRunJobDoesNotJoinAnotherActiveRun(t *testing.T) {
	startedAt := time.Now().Add(-time.Minute)
	sctx := &ScheduleContext{
		WorkspacePath: "/tmp/workflow",
		Schedule:      WorkflowSchedule{ID: "schedule-1", Name: "Active schedule"},
	}
	runtimeKey := scheduleRuntimeKey(sctx)
	svc := &SchedulerService{runtimeStates: map[string]*ScheduleRuntimeState{
		runtimeKey: {
			ActiveRunID: "active-run",
			LastStatus:  "running",
			LastRunAt:   &startedAt,
		},
	}, runCancels: map[string]context.CancelFunc{
		"stale-run": func() {},
	}}

	_, err := svc.runJob(context.Background(), sctx, "stale-run")
	if !errors.Is(err, errWorkshopSequenceInterrupted) {
		t.Fatalf("runJob error = %v, want errWorkshopSequenceInterrupted", err)
	}
	if got := svc.runtimeStates[runtimeKey].ActiveRunID; got != "active-run" {
		t.Fatalf("active run ownership changed to %q", got)
	}
}

func TestWaitForWorkshopIdleTimesOutWhenSessionStaysBusy(t *testing.T) {
	oldInterval := schedulerWorkshopIdlePollInterval
	oldMaxInactivity := schedulerWorkshopMaxInactivity
	schedulerWorkshopIdlePollInterval = time.Millisecond
	schedulerWorkshopMaxInactivity = 5 * time.Millisecond
	defer func() {
		schedulerWorkshopIdlePollInterval = oldInterval
		schedulerWorkshopMaxInactivity = oldMaxInactivity
	}()

	sessionID := "session-scheduler-busy-timeout"
	api := &StreamingAPI{terminalStore: terminals.NewStore()}
	api.setSessionBusy(sessionID, true)
	svc := &SchedulerService{api: api}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err := svc.waitForWorkshopIdle(ctx, sessionID)
	if err == nil {
		t.Fatal("waitForWorkshopIdle returned nil, want timeout")
	}
	if !strings.Contains(err.Error(), "workshop idle wait timed out") {
		t.Fatalf("error = %v, want timeout", err)
	}
	if !errors.Is(err, errWorkshopIdleWaitTimeout) {
		t.Fatalf("error = %v, want errWorkshopIdleWaitTimeout", err)
	}
	if !strings.Contains(err.Error(), "no tmux, tool, execution, or session progress") {
		t.Fatalf("error = %v, want inactivity reason", err)
	}
}

func TestWaitForWorkshopIdleTreatsTmuxRefreshFailureAsInactivity(t *testing.T) {
	oldInterval := schedulerWorkshopIdlePollInterval
	schedulerWorkshopIdlePollInterval = time.Millisecond
	defer func() { schedulerWorkshopIdlePollInterval = oldInterval }()

	store := terminals.NewStore()
	sessionID := "session-scheduler-refresh-failure"
	tmuxSession := "tmux-scheduler-refresh-failure"
	store.HandleEvent(sessionID, terminalRouteChunkEvent(sessionID, "workflow-step:bug-review", tmuxSession, "starting", 1))

	api := &StreamingAPI{terminalStore: store}
	api.setSessionBusy(sessionID, true)
	svc := &SchedulerService{api: api}

	oldRunOutput := runTerminalTmuxOutputCommand
	defer func() { runTerminalTmuxOutputCommand = oldRunOutput }()
	runTerminalTmuxOutputCommand = func(ctx context.Context, args ...string) (string, error) {
		return "", errors.New("tmux capture unavailable")
	}

	maxInactivity := 20 * time.Millisecond
	startedAt := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err := svc.waitForWorkshopIdleWithInactivityTimeout(ctx, sessionID, maxInactivity)
	if !errors.Is(err, errWorkshopIdleWaitTimeout) {
		t.Fatalf("error = %v, want errWorkshopIdleWaitTimeout", err)
	}
	if elapsed := time.Since(startedAt); elapsed < maxInactivity {
		t.Fatalf("wait failed after %s, want full inactivity window %s", elapsed, maxInactivity)
	}
	if !strings.Contains(err.Error(), "last tmux refresh error:") || !strings.Contains(err.Error(), "tmux capture unavailable") {
		t.Fatalf("error = %v, want tmux refresh context", err)
	}
}

func TestWaitForWorkshopIdleReportsRuntimeFailureBeforeStopGuard(t *testing.T) {
	sessionID := "session-runtime-failed"
	api := &StreamingAPI{
		activeSessions: map[string]*ActiveSessionInfo{
			sessionID: {SessionID: sessionID, Status: "error"},
		},
		stoppedSessions: map[string]bool{sessionID: true},
	}
	svc := &SchedulerService{api: api}

	err := svc.waitForWorkshopIdleWithInactivityTimeout(context.Background(), sessionID, time.Minute)
	if !errors.Is(err, errWorkshopSessionFailed) {
		t.Fatalf("error = %v, want errWorkshopSessionFailed", err)
	}
	if errors.Is(err, errWorkshopSequenceInterrupted) {
		t.Fatalf("runtime failure was misclassified as user interruption: %v", err)
	}
}

func TestWaitForWorkshopIdleAllowsLongRunningTmuxWithProgress(t *testing.T) {
	oldInterval := schedulerWorkshopIdlePollInterval
	schedulerWorkshopIdlePollInterval = time.Millisecond
	defer func() { schedulerWorkshopIdlePollInterval = oldInterval }()

	store := terminals.NewStore()
	sessionID := "session-scheduler-progress"
	tmuxSession := "tmux-scheduler-progress"
	store.HandleEvent(sessionID, terminalRouteChunkEvent(sessionID, "workflow-step:bug-review", tmuxSession, "starting", 1))

	api := &StreamingAPI{terminalStore: store}
	api.setSessionBusy(sessionID, true)
	svc := &SchedulerService{api: api}

	oldRunOutput := runTerminalTmuxOutputCommand
	defer func() { runTerminalTmuxOutputCommand = oldRunOutput }()
	calls := 0
	runTerminalTmuxOutputCommand = func(ctx context.Context, args ...string) (string, error) {
		calls++
		if calls >= 130 {
			api.setSessionBusy(sessionID, false)
			return "done\n❯", nil
		}
		return fmt.Sprintf("bug-review progress %d", calls), nil
	}

	maxInactivity := 100 * time.Millisecond
	startedAt := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := svc.waitForWorkshopIdleWithInactivityTimeout(ctx, sessionID, maxInactivity); err != nil {
		t.Fatalf("waitForWorkshopIdleWithInactivityTimeout returned error: %v", err)
	}
	if elapsed := time.Since(startedAt); elapsed <= maxInactivity {
		t.Fatalf("wait completed in %s, want total run longer than inactivity limit %s", elapsed, maxInactivity)
	}
	if calls < 130 {
		t.Fatalf("tmux captures = %d, want at least 130", calls)
	}
}

func TestPostRunMonitorStepMaxInactivityUsesLongerGoalAdvisorCap(t *testing.T) {
	oldNormal := schedulerWorkshopMaxInactivity
	oldAdvisor := schedulerGoalAdvisorMaxInactivity
	schedulerWorkshopMaxInactivity = 10 * time.Minute
	schedulerGoalAdvisorMaxInactivity = 30 * time.Minute
	defer func() {
		schedulerWorkshopMaxInactivity = oldNormal
		schedulerGoalAdvisorMaxInactivity = oldAdvisor
	}()

	if got := (postRunMonitorStep{label: "bug-review"}).idleMaxInactivity(); got != 10*time.Minute {
		t.Fatalf("bug-review max inactivity = %s, want 10m", got)
	}
	if got := (postRunMonitorStep{label: "goal-advisor"}).idleMaxInactivity(); got != 30*time.Minute {
		t.Fatalf("goal-advisor max inactivity = %s, want 30m", got)
	}
}

func TestPostRunMonitorStepClassificationSupportsTimeoutRecovery(t *testing.T) {
	tests := []struct {
		label     string
		module    string
		finalStep bool
	}{
		{label: "bug-review", module: pulseModuleBugReview},
		{label: "eval-health", module: pulseModuleEvalHealth},
		{label: "goal-advisor", module: pulseModuleGoalAdvisor},
		{label: "finalize", finalStep: true},
	}
	for _, test := range tests {
		if got := pulseModuleForPostRunMonitorStep(test.label); got != test.module {
			t.Fatalf("module for %q = %q, want %q", test.label, got, test.module)
		}
		if got := isPostRunMonitorFinalStep(test.label); got != test.finalStep {
			t.Fatalf("final-step classification for %q = %v, want %v", test.label, got, test.finalStep)
		}
	}
}

func TestRunningWorkflowScheduleInSetLockedFindsOtherRunningSchedule(t *testing.T) {
	states := map[string]*ScheduleRuntimeState{
		"daily":     {LastStatus: "running", LastSessionID: "session-daily"},
		"optimizer": {LastStatus: "success", LastSessionID: "session-optimizer"},
	}

	id, sessionID := runningWorkflowScheduleInSetLocked(states, []string{"current", "daily", "optimizer"}, "current")
	if id != "daily" {
		t.Fatalf("running schedule id = %q, want daily", id)
	}
	if sessionID != "session-daily" {
		t.Fatalf("running schedule session = %q, want session-daily", sessionID)
	}
}

func TestRunningWorkflowScheduleInSetLockedIgnoresCurrentSchedule(t *testing.T) {
	states := map[string]*ScheduleRuntimeState{
		"current": {LastStatus: "running", LastSessionID: "session-current"},
	}

	id, sessionID := runningWorkflowScheduleInSetLocked(states, []string{"current"}, "current")
	if id != "" || sessionID != "" {
		t.Fatalf("running schedule = (%q, %q), want empty", id, sessionID)
	}
}

func TestWaitForLiveInputTurnCompleteRequiresBusyBeforeIdle(t *testing.T) {
	oldInterval := liveInputTurnPollInterval
	oldStableAfter := liveInputTurnNoBusyStableAfter
	liveInputTurnPollInterval = time.Millisecond
	liveInputTurnNoBusyStableAfter = time.Hour
	defer func() {
		liveInputTurnPollInterval = oldInterval
		liveInputTurnNoBusyStableAfter = oldStableAfter
	}()

	store := terminals.NewStore()
	sessionID := "session-live-input-wait"
	tmuxSession := "tmux-live-input-wait"
	store.HandleEvent(sessionID, terminalRouteChunkEvent(sessionID, "main:"+sessionID, tmuxSession, "old pane\n❯", 1))

	oldRunOutput := runTerminalTmuxOutputCommand
	defer func() { runTerminalTmuxOutputCommand = oldRunOutput }()
	outputs := []string{
		"prompt echoed\n❯",
		"thinking\nesc to interrupt",
		"final answer\n❯",
		"final answer\n❯",
	}
	calls := 0
	runTerminalTmuxOutputCommand = func(ctx context.Context, args ...string) (string, error) {
		if calls >= len(outputs) {
			return outputs[len(outputs)-1], nil
		}
		out := outputs[calls]
		calls++
		return out, nil
	}

	api := &StreamingAPI{terminalStore: store}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := api.waitForLiveInputTurnComplete(ctx, nil, sessionID); err != nil {
		t.Fatalf("waitForLiveInputTurnComplete returned error: %v", err)
	}
	if calls < 4 {
		t.Fatalf("tmux captures = %d, want at least 4; initial idle must not complete the live-input turn", calls)
	}
}

func TestApplyPulseLLMToReqMapUsesCodingAgentPulseDefaultWhenUnset(t *testing.T) {
	reqMap := map[string]interface{}{}
	sctx := &ScheduleContext{
		Schedule: WorkflowSchedule{WorkshopMode: "run"},
		Capabilities: WorkflowCapabilities{
			LLMConfig: &workflowtypes.PresetLLMConfig{
				SchemaVersion: workflowtypes.LLMConfigSchemaVersion,
				Mode:          workflowtypes.LLMConfigModeProviderProfile,
				Provider:      "claude-code",
			},
		},
	}

	svc := &SchedulerService{}
	svc.applyLLMAndSecretsToReqMap(context.Background(), reqMap, sctx)
	svc.applyPulseLLMToReqMap(reqMap, sctx, "test-session")

	llmConfig, ok := reqMap["llm_config"].(map[string]interface{})
	if !ok {
		t.Fatalf("llm_config missing or wrong type: %#v", reqMap["llm_config"])
	}
	primary, ok := llmConfig["primary"].(map[string]interface{})
	if !ok {
		t.Fatalf("llm_config.primary missing or wrong type: %#v", llmConfig["primary"])
	}
	if got := primary["provider"]; got != "claude-code" {
		t.Fatalf("provider = %#v, want claude-code", got)
	}
	if got := primary["model_id"]; got != "claude-sonnet-5" {
		t.Fatalf("model_id = %#v, want claude-sonnet-5", got)
	}
	options, ok := primary["options"].(map[string]interface{})
	if !ok {
		t.Fatalf("options missing or wrong type: %#v", primary["options"])
	}
	if got := options["reasoning_effort"]; got != "high" {
		t.Fatalf("reasoning_effort = %#v, want high", got)
	}
}

func TestApplyPulseLLMToReqMapKeepsWorkflowModelWhenNoProviderDefault(t *testing.T) {
	reqMap := map[string]interface{}{}
	builder := &workflowtypes.AgentLLMConfig{Provider: "openai", ModelID: "gpt-5.4"}
	sctx := &ScheduleContext{
		Schedule: WorkflowSchedule{WorkshopMode: "run"},
		Capabilities: WorkflowCapabilities{
			LLMConfig: &workflowtypes.PresetLLMConfig{
				SchemaVersion:  workflowtypes.LLMConfigSchemaVersion,
				Mode:           workflowtypes.LLMConfigModeExplicit,
				BuilderLLM:     builder,
				MaintenanceLLM: builder,
				PulseLLM:       builder,
				TieredConfig:   &workflowtypes.TieredLLMConfig{Tier1: builder, Tier2: builder, Tier3: builder},
			},
		},
	}

	svc := &SchedulerService{}
	svc.applyLLMAndSecretsToReqMap(context.Background(), reqMap, sctx)
	svc.applyPulseLLMToReqMap(reqMap, sctx, "test-session")

	llmConfig, ok := reqMap["llm_config"].(map[string]interface{})
	if !ok {
		t.Fatalf("llm_config missing or wrong type: %#v", reqMap["llm_config"])
	}
	primary, ok := llmConfig["primary"].(map[string]interface{})
	if !ok {
		t.Fatalf("llm_config.primary missing or wrong type: %#v", llmConfig["primary"])
	}
	if got := primary["provider"]; got != "openai" {
		t.Fatalf("provider = %#v, want openai", got)
	}
	if got := primary["model_id"]; got != "gpt-5.4" {
		t.Fatalf("model_id = %#v, want gpt-5.4", got)
	}
}

func TestResolveChiefOfStaffLLMForScheduleUsesExplicitOverride(t *testing.T) {
	sctx := &ScheduleContext{
		SourceType: "multi-agent",
		Capabilities: WorkflowCapabilities{
			LLMConfig: &workflowtypes.PresetLLMConfig{
				SchemaVersion: workflowtypes.LLMConfigSchemaVersion,
				Mode:          workflowtypes.LLMConfigModeProviderProfile,
				Provider:      "claude-code",
				ChiefOfStaffLLM: &workflowtypes.AgentLLMConfig{
					Provider: "codex-cli",
					ModelID:  "gpt-5.5",
					Options:  map[string]interface{}{"reasoning_effort": "xhigh"},
				},
			},
		},
	}

	got := resolveChiefOfStaffLLMForSchedule(context.Background(), sctx)
	if got == nil {
		t.Fatal("resolveChiefOfStaffLLMForSchedule() = nil")
	}
	if got.Provider != "codex-cli" || got.ModelID != "gpt-5.5" {
		t.Fatalf("resolveChiefOfStaffLLMForSchedule() = %+v, want codex-cli/gpt-5.5", got)
	}
	if got.Options["reasoning_effort"] != "xhigh" {
		t.Fatalf("reasoning_effort = %#v, want xhigh", got.Options["reasoning_effort"])
	}
}

func TestResolveChiefOfStaffLLMForScheduleUsesCodingAgentDefault(t *testing.T) {
	sctx := &ScheduleContext{
		SourceType: "multi-agent",
		Capabilities: WorkflowCapabilities{
			LLMConfig: &workflowtypes.PresetLLMConfig{
				SchemaVersion: workflowtypes.LLMConfigSchemaVersion,
				Mode:          workflowtypes.LLMConfigModeProviderProfile,
				Provider:      "claude-code",
			},
		},
	}

	got := resolveChiefOfStaffLLMForSchedule(context.Background(), sctx)
	if got == nil {
		t.Fatal("resolveChiefOfStaffLLMForSchedule() = nil")
	}
	if got.Provider != "claude-code" || got.ModelID != "claude-opus-4-8" {
		t.Fatalf("resolveChiefOfStaffLLMForSchedule() = %+v, want claude-code/claude-opus-4-8", got)
	}
	if got.Options["reasoning_effort"] != "high" {
		t.Fatalf("reasoning_effort = %#v, want high", got.Options["reasoning_effort"])
	}
}

func TestResolveChiefOfStaffLLMFromDelegationConfigUsesExplicitScheduledModel(t *testing.T) {
	got := resolveChiefOfStaffLLMFromDelegationConfig(&virtualtools.DelegationTierConfig{
		ChiefOfStaff: &virtualtools.TierModel{
			Provider: "codex-cli",
			ModelID:  "gpt-5.5",
		},
		Main: &virtualtools.TierModel{
			Provider: "claude-code",
			ModelID:  "claude-code",
		},
	})
	if got == nil {
		t.Fatal("resolveChiefOfStaffLLMFromDelegationConfig() = nil")
	}
	if got.Provider != "codex-cli" || got.ModelID != "gpt-5.5" {
		t.Fatalf("resolveChiefOfStaffLLMFromDelegationConfig() = %+v, want codex-cli/gpt-5.5", got)
	}
}

func TestResolveChiefOfStaffLLMFromDelegationConfigUsesProviderDefault(t *testing.T) {
	got := resolveChiefOfStaffLLMFromDelegationConfig(&virtualtools.DelegationTierConfig{
		Main: &virtualtools.TierModel{
			Provider: "claude-code",
			ModelID:  "claude-code",
		},
	})
	if got == nil {
		t.Fatal("resolveChiefOfStaffLLMFromDelegationConfig() = nil")
	}
	if got.Provider != "claude-code" || got.ModelID != "claude-opus-4-8" {
		t.Fatalf("resolveChiefOfStaffLLMFromDelegationConfig() = %+v, want claude-code/claude-opus-4-8", got)
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
	builder := &workflowtypes.AgentLLMConfig{Provider: "claude-code", ModelID: "claude-opus-4-6"}
	return &ScheduleContext{
		WorkspacePath: workspacePath,
		UserID:        "default",
		Schedule: WorkflowSchedule{
			ID:             scheduleID,
			ResumePrevious: &resumePrevious,
		},
		Capabilities: WorkflowCapabilities{
			LLMConfig: &workflowtypes.PresetLLMConfig{
				SchemaVersion:  workflowtypes.LLMConfigSchemaVersion,
				Mode:           workflowtypes.LLMConfigModeExplicit,
				BuilderLLM:     builder,
				MaintenanceLLM: builder,
				PulseLLM:       builder,
				TieredConfig:   &workflowtypes.TieredLLMConfig{Tier1: builder, Tier2: builder, Tier3: builder},
			},
		},
	}
}

func resumeTestMultiAgentScheduleContext(userID, scheduleID string) *ScheduleContext {
	resumePrevious := true
	builder := &workflowtypes.AgentLLMConfig{Provider: "claude-code", ModelID: "claude-opus-4-6"}
	return &ScheduleContext{
		UserID:     userID,
		SourceType: "multi-agent",
		Schedule: WorkflowSchedule{
			ID:             scheduleID,
			ResumePrevious: &resumePrevious,
		},
		Capabilities: WorkflowCapabilities{
			LLMConfig: &workflowtypes.PresetLLMConfig{
				SchemaVersion:  workflowtypes.LLMConfigSchemaVersion,
				Mode:           workflowtypes.LLMConfigModeExplicit,
				BuilderLLM:     builder,
				MaintenanceLLM: builder,
				PulseLLM:       builder,
				TieredConfig:   &workflowtypes.TieredLLMConfig{Tier1: builder, Tier2: builder, Tier3: builder},
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
