package schedulerstate

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	store, err := Open(filepath.Join(t.TempDir(), "schedule-state.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func TestRunLifecycleAndEvents(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	startedAt := time.Now().UTC()
	run := Run{
		RunID:         "run-1",
		ScopeType:     "workflow",
		ScopeID:       "Workflow/demo",
		LockKey:       "workflow:Workflow/demo",
		ScheduleID:    "schedule-1",
		TriggerSource: "cron",
		StartedAt:     startedAt,
	}
	if err := store.BeginRun(ctx, run); err != nil {
		t.Fatal(err)
	}
	transitions := []Transition{
		{RunID: run.RunID, To: StateWorkflowRunning, SessionID: "session-1", SessionKind: "workflow", Reason: "session started"},
		{RunID: run.RunID, To: StateWorkflowFinished, SessionID: "session-1", RunFolder: "iteration-0", Reason: "workflow finished"},
		{RunID: run.RunID, To: StatePulseGate, SessionID: "session-1", SessionKind: "pulse", Reason: "Pulse enabled"},
		{RunID: run.RunID, To: StatePulseModules, SessionID: "session-1", SessionKind: "pulse", Reason: "Gate recorded worklist"},
		{RunID: run.RunID, To: StatePulseFinalizing, SessionID: "recovery-session", SessionKind: "pulse_recovery", Reason: "finalizing"},
		{RunID: run.RunID, To: StateCompleted, SessionID: "recovery-session", SessionKind: "pulse_recovery", Reason: "done"},
	}
	for _, transition := range transitions {
		if err := store.Transition(ctx, transition); err != nil {
			t.Fatalf("Transition(%s): %v", transition.To, err)
		}
	}

	got, err := store.GetRun(ctx, run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != StateCompleted || got.CompletedAt == nil || got.ActiveSessionID != "recovery-session" {
		t.Fatalf("run = %+v", got)
	}
	events, err := store.ListEvents(ctx, run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != len(transitions)+1 {
		t.Fatalf("len(events) = %d, want %d", len(events), len(transitions)+1)
	}
}

func TestActiveLeaseRejectsOverlappingWorkflowSchedules(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	first := Run{RunID: "run-1", ScopeType: "workflow", ScopeID: "Workflow/demo", LockKey: "workflow:Workflow/demo", ScheduleID: "schedule-1", TriggerSource: "cron"}
	second := Run{RunID: "run-2", ScopeType: "workflow", ScopeID: "Workflow/demo", LockKey: "workflow:Workflow/demo", ScheduleID: "schedule-2", TriggerSource: "manual"}
	if err := store.BeginRun(ctx, first); err != nil {
		t.Fatal(err)
	}
	if err := store.BeginRun(ctx, second); !errors.Is(err, ErrRunAlreadyActive) {
		t.Fatalf("BeginRun(overlap) error = %v, want ErrRunAlreadyActive", err)
	}
	if err := store.Transition(ctx, Transition{RunID: first.RunID, To: StateStopped, Reason: "user stopped"}); err != nil {
		t.Fatal(err)
	}
	if err := store.BeginRun(ctx, second); err != nil {
		t.Fatalf("BeginRun(after terminal): %v", err)
	}
}

func TestRejectsInvalidAndTerminalRegression(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	run := Run{RunID: "run-1", ScopeType: "workflow", ScopeID: "Workflow/demo", LockKey: "workflow:Workflow/demo", ScheduleID: "schedule-1", TriggerSource: "cron"}
	if err := store.BeginRun(ctx, run); err != nil {
		t.Fatal(err)
	}
	if err := store.Transition(ctx, Transition{RunID: run.RunID, To: StatePulseModules}); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("invalid transition error = %v", err)
	}
	if err := store.Transition(ctx, Transition{RunID: run.RunID, To: StateFailed, Reason: "failed to start"}); err != nil {
		t.Fatal(err)
	}
	if err := store.Transition(ctx, Transition{RunID: run.RunID, To: StateWorkflowRunning}); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("terminal regression error = %v", err)
	}
}

func TestPulseGateCanCompleteWithoutSelectedModules(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	run := Run{RunID: "run-no-modules", ScopeType: "workflow", ScopeID: "Workflow/demo", LockKey: "workflow:Workflow/demo", ScheduleID: "schedule-1"}
	if err := store.BeginRun(ctx, run); err != nil {
		t.Fatal(err)
	}
	for _, transition := range []Transition{
		{RunID: run.RunID, To: StateWorkflowRunning},
		{RunID: run.RunID, To: StateWorkflowFinished},
		{RunID: run.RunID, To: StatePulseGate},
		{RunID: run.RunID, To: StateCompleted},
	} {
		if err := store.Transition(ctx, transition); err != nil {
			t.Fatalf("transition to %s: %v", transition.To, err)
		}
	}
}

func TestForceTerminalReleasesStuckLease(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	first := Run{RunID: "run-stuck", ScopeType: "workflow", ScopeID: "Workflow/demo", LockKey: "workflow:Workflow/demo", ScheduleID: "schedule-1"}
	if err := store.BeginRun(ctx, first); err != nil {
		t.Fatal(err)
	}
	if err := store.ForceTerminal(ctx, Transition{RunID: first.RunID, To: StateCompleted, Reason: "recover lease"}); err != nil {
		t.Fatalf("force terminal: %v", err)
	}
	second := Run{RunID: "run-next", ScopeType: "workflow", ScopeID: "Workflow/demo", LockKey: first.LockKey, ScheduleID: "schedule-2"}
	if err := store.BeginRun(ctx, second); err != nil {
		t.Fatalf("lease remained stuck: %v", err)
	}
}

func TestInterruptActiveRunsReleasesLeases(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	run := Run{RunID: "run-1", ScopeType: "workflow", ScopeID: "Workflow/demo", LockKey: "workflow:Workflow/demo", ScheduleID: "schedule-1", TriggerSource: "cron"}
	if err := store.BeginRun(ctx, run); err != nil {
		t.Fatal(err)
	}
	count, err := store.InterruptActiveRuns(ctx, "server restarted", time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("count = %d, want 1", count)
	}
	got, err := store.GetRun(ctx, run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != StateInterrupted {
		t.Fatalf("state = %s, want interrupted", got.State)
	}
}

func TestActiveRunByLockKey(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	run := Run{RunID: "run-active", ScopeType: "workflow", ScopeID: "demo", LockKey: "workflow/demo", ScheduleID: "daily", StartedAt: now}
	if err := store.BeginRun(ctx, run); err != nil {
		t.Fatalf("begin run: %v", err)
	}

	active, err := store.ActiveRunByLockKey(ctx, run.LockKey)
	if err != nil {
		t.Fatalf("active run: %v", err)
	}
	if active.RunID != run.RunID || active.State != StateStarting {
		t.Fatalf("unexpected active run: %+v", active)
	}

	if err := store.Transition(ctx, Transition{RunID: run.RunID, To: StateStopped, Reason: "test stop", At: now.Add(time.Second)}); err != nil {
		t.Fatalf("stop run: %v", err)
	}
	if _, err := store.ActiveRunByLockKey(ctx, run.LockKey); !errors.Is(err, ErrRunNotFound) {
		t.Fatalf("expected no active run after stop, got %v", err)
	}
}

func TestFireDecisionsAreDurableAndScoped(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	for i, decision := range []string{"skipped_busy", "started"} {
		if err := store.RecordFireDecision(ctx, FireDecision{
			DecisionID: fmt.Sprintf("decision-%d", i), ScopeType: "workflow", ScopeID: "Workflow/demo",
			ScheduleID: "daily", TriggerSource: "cron", Decision: decision, Reason: "test", FiredAt: now.Add(time.Duration(i) * time.Second),
		}); err != nil {
			t.Fatalf("record decision: %v", err)
		}
	}
	if err := store.RecordFireDecision(ctx, FireDecision{
		DecisionID: "other", ScopeType: "workflow", ScopeID: "Workflow/other", ScheduleID: "daily",
		TriggerSource: "cron", Decision: "started", Reason: "other", FiredAt: now,
	}); err != nil {
		t.Fatalf("record other decision: %v", err)
	}

	decisions, err := store.ListFireDecisions(ctx, "workflow", "Workflow/demo", "daily", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(decisions) != 2 || decisions[0].Decision != "started" || decisions[1].Decision != "skipped_busy" {
		t.Fatalf("unexpected decisions: %+v", decisions)
	}
}

func TestFireDecisionRetentionIsBoundedPerSchedule(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	for i := 0; i < fireDecisionRetentionPerSchedule+5; i++ {
		if err := store.RecordFireDecision(ctx, FireDecision{
			DecisionID: fmt.Sprintf("decision-%03d", i), ScopeType: "workflow", ScopeID: "Workflow/demo",
			ScheduleID: "frequent", TriggerSource: "cron", Decision: "skipped_paused",
			FiredAt: now.Add(time.Duration(i) * time.Second),
		}); err != nil {
			t.Fatalf("record decision %d: %v", i, err)
		}
	}
	decisions, err := store.ListFireDecisions(ctx, "workflow", "Workflow/demo", "frequent", fireDecisionRetentionPerSchedule)
	if err != nil {
		t.Fatal(err)
	}
	if len(decisions) != fireDecisionRetentionPerSchedule {
		t.Fatalf("retained decisions = %d, want %d", len(decisions), fireDecisionRetentionPerSchedule)
	}
	if got := decisions[len(decisions)-1].DecisionID; got != "decision-005" {
		t.Fatalf("oldest retained decision = %q, want decision-005", got)
	}
}
