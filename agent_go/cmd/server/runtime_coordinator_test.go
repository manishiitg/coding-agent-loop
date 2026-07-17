package server

import (
	"context"
	"testing"
	"time"

	"github.com/manishiitg/coding-agent-loop/agent_go/internal/events"
)

func TestRuntimeCoordinatorDerivesWaitingForUserFromEventStore(t *testing.T) {
	store := events.NewEventStore(10)
	defer store.Stop()
	const sessionID = "waiting-session"
	now := time.Now().UTC()
	store.AddEvent(sessionID, events.Event{ID: "question", Type: "request_human_feedback", SessionID: sessionID, Timestamp: now})
	api := &StreamingAPI{
		runtimeCoordinator: NewRuntimeCoordinator(), eventStore: store,
		activeSessions: map[string]*ActiveSessionInfo{
			sessionID: {SessionID: sessionID, Status: "running", CreatedAt: now.Add(-time.Minute), LastActivity: now},
		},
	}

	snapshot, _ := api.authoritativeRuntimeSnapshot(sessionID)
	if snapshot.Phase != runtimePhaseWaiting || !snapshot.WaitingForUser {
		t.Fatalf("waiting event produced runtime state %#v", snapshot)
	}
}

func TestRuntimeCoordinatorSnapshotsAreImmutableAndDeduplicated(t *testing.T) {
	coordinator := NewRuntimeCoordinator()
	startedAt := time.Now().Add(-time.Minute).UTC()
	completedAt := startedAt.Add(10 * time.Second)
	candidate := RuntimeSnapshot{
		SessionID:  "session-1",
		Phase:      runtimePhaseRunning,
		StartedAt:  startedAt,
		ObservedAt: time.Now(),
		ChildExecutions: []RuntimeChildSnapshot{{
			ExecutionID: "child-1",
			Status:      trackedExecutionStatusCompleted,
			StartedAt:   startedAt,
			CompletedAt: &completedAt,
		}},
	}

	first, changed := coordinator.Observe(candidate)
	if !changed || first.Revision == 0 || first.Generation != 1 {
		t.Fatalf("first observation = %#v changed=%t", first, changed)
	}
	second, changed := coordinator.Observe(candidate)
	if changed || second.Revision != first.Revision {
		t.Fatalf("identical observation created revision: first=%d second=%d changed=%t", first.Revision, second.Revision, changed)
	}

	first.ChildExecutions[0].Status = trackedExecutionStatusFailed
	*first.ChildExecutions[0].CompletedAt = time.Time{}
	stored, ok := coordinator.Snapshot(candidate.SessionID)
	if !ok {
		t.Fatal("expected stored snapshot")
	}
	if stored.ChildExecutions[0].Status != trackedExecutionStatusCompleted || stored.ChildExecutions[0].CompletedAt.IsZero() {
		t.Fatalf("returned snapshot mutated coordinator state: %#v", stored.ChildExecutions[0])
	}
}

func TestRuntimeCoordinatorEvictsRetainedSnapshot(t *testing.T) {
	coordinator := NewRuntimeCoordinator()
	coordinator.Observe(RuntimeSnapshot{SessionID: "session-1", Phase: runtimePhaseIdle})

	coordinator.Evict("session-1")

	if _, ok := coordinator.Snapshot("session-1"); ok {
		t.Fatal("evicted runtime snapshot is still retained")
	}
}

func TestSessionDisplayStatusReadsAuthoritativeCoordinator(t *testing.T) {
	coordinator := NewRuntimeCoordinator()
	api := &StreamingAPI{
		runtimeCoordinator: coordinator,
		activeSessions: map[string]*ActiveSessionInfo{
			"session-1": {SessionID: "session-1", Status: "running"},
		},
		sessionBusy:      map[string]bool{"session-1": true},
		agentCancelFuncs: map[string]context.CancelFunc{},
	}

	status := api.sessionDisplayStatus("session-1")
	if status.Status != sessionExecutionDisplayBusy {
		t.Fatalf("display status = %q, want busy", status.Status)
	}
	if _, ok := coordinator.Snapshot("session-1"); !ok {
		t.Fatal("display-status read did not update the authoritative runtime snapshot")
	}
}

func TestRuntimeCoordinatorStartsNewGenerationAfterIdle(t *testing.T) {
	coordinator := NewRuntimeCoordinator()
	startedAt := time.Now().Add(-time.Hour).UTC()
	idle, _ := coordinator.Observe(RuntimeSnapshot{
		SessionID: "session-1",
		Phase:     runtimePhaseIdle,
		StartedAt: startedAt,
	})
	running, _ := coordinator.Observe(RuntimeSnapshot{
		SessionID: "session-1",
		Phase:     runtimePhaseRunning,
		StartedAt: startedAt,
	})
	stillRunning, _ := coordinator.Observe(RuntimeSnapshot{
		SessionID: "session-1",
		Phase:     runtimePhaseRunning,
		StartedAt: startedAt,
		Reason:    "progressed",
	})

	if idle.Generation != 1 || running.Generation != 2 || stillRunning.Generation != 2 {
		t.Fatalf("unexpected generations: idle=%d running=%d progressed=%d", idle.Generation, running.Generation, stillRunning.Generation)
	}
}

func TestRuntimeCoordinatorTerminalBoundaryRejectsStaleStateUntilExplicitRestart(t *testing.T) {
	coordinator := NewRuntimeCoordinator()
	startedAt := time.Now().Add(-time.Minute).UTC()
	running, _ := coordinator.Observe(RuntimeSnapshot{
		SessionID: "session-1", Phase: runtimePhaseRunning, StartedAt: startedAt,
		ForegroundTurn: RuntimeForegroundSnapshot{Busy: true, HasCancel: true, CanSteer: true},
		BackgroundLive: true, TerminalBusy: true, WaitingForUser: true,
	})
	stopped, changed := coordinator.MarkTerminalBoundary("session-1", runtimePhaseCanceled, "stopped by user")
	if !changed || stopped.Phase != runtimePhaseCanceled || stopped.Generation != running.Generation {
		t.Fatalf("terminal boundary = %#v changed=%t; running generation=%d", stopped, changed, running.Generation)
	}
	if stopped.ForegroundTurn.Busy || stopped.ForegroundTurn.HasCancel || stopped.ForegroundTurn.CanSteer ||
		stopped.BackgroundLive || stopped.TerminalBusy || stopped.WaitingForUser {
		t.Fatalf("terminal boundary retained live signals: %#v", stopped)
	}

	stale, changed := coordinator.Observe(RuntimeSnapshot{
		SessionID: "session-1", Phase: runtimePhaseRunning, StartedAt: startedAt,
		Reason: "late completion from old turn",
	})
	if changed || stale.Phase != runtimePhaseCanceled || stale.Revision != stopped.Revision {
		t.Fatalf("stale observation reopened terminal generation: %#v changed=%t", stale, changed)
	}

	restarted, changed := coordinator.StartGeneration("session-1", "new user turn")
	if !changed || restarted.Phase != runtimePhaseStarting || restarted.Generation != stopped.Generation+1 {
		t.Fatalf("explicit restart = %#v changed=%t", restarted, changed)
	}
	staleAfterRestart, changed := coordinator.Observe(RuntimeSnapshot{
		SessionID: "session-1", Phase: runtimePhaseRunning, StartedAt: startedAt,
		LastProgressAt: startedAt.Add(30 * time.Second),
	})
	if changed || staleAfterRestart.Phase != runtimePhaseStarting || staleAfterRestart.Revision != restarted.Revision {
		t.Fatalf("old generation overwrote restart boundary: %#v changed=%t", staleAfterRestart, changed)
	}
	staleCompletion, changed := coordinator.Observe(RuntimeSnapshot{
		SessionID: "session-1", Phase: runtimePhaseCompleted, StartedAt: startedAt,
		LastProgressAt: startedAt.Add(45 * time.Second),
	})
	if changed || staleCompletion.Phase != runtimePhaseStarting || staleCompletion.Revision != restarted.Revision {
		t.Fatalf("old completion overwrote restart boundary: %#v changed=%t", staleCompletion, changed)
	}
	newProgressAt := time.Now().UTC().Add(time.Second)
	resumed, changed := coordinator.Observe(RuntimeSnapshot{
		SessionID: "session-1", Phase: runtimePhaseRunning, StartedAt: newProgressAt,
		LastProgressAt: newProgressAt,
	})
	if !changed || resumed.Phase != runtimePhaseRunning || resumed.Generation != restarted.Generation {
		t.Fatalf("new generation did not accept running observation: %#v changed=%t", resumed, changed)
	}
}

func TestMarkSessionStoppedAsPreservesExplicitFailureOutcome(t *testing.T) {
	coordinator := NewRuntimeCoordinator()
	api := &StreamingAPI{
		runtimeCoordinator: coordinator,
		stoppedSessions:    map[string]bool{},
	}
	coordinator.Observe(RuntimeSnapshot{
		SessionID: "failed-session",
		Phase:     runtimePhaseRunning,
		StartedAt: time.Now().Add(-time.Minute).UTC(),
	})

	api.markSessionStoppedAs("failed-session", runtimePhaseFailed, "terminal exited unexpectedly")

	snapshot, ok := coordinator.Snapshot("failed-session")
	if !ok || snapshot.Phase != runtimePhaseFailed || snapshot.Reason != "terminal exited unexpectedly" {
		t.Fatalf("explicit failure cancellation = %#v, present=%t", snapshot, ok)
	}
	if !api.isSessionMarkedStopped("failed-session") {
		t.Fatal("explicit failure did not set the hard stopped guard")
	}
}

func TestRuntimeCoordinatorDoesNotCreateGenerationWhenStartTimestampArrivesLate(t *testing.T) {
	coordinator := NewRuntimeCoordinator()
	first, _ := coordinator.Observe(RuntimeSnapshot{
		SessionID: "session-1",
		Phase:     runtimePhaseRunning,
	})
	withTimestamp, _ := coordinator.Observe(RuntimeSnapshot{
		SessionID: "session-1",
		Phase:     runtimePhaseRunning,
		StartedAt: time.Now().UTC(),
	})
	if first.Generation != 1 || withTimestamp.Generation != 1 {
		t.Fatalf("late start timestamp changed generation: first=%d later=%d", first.Generation, withTimestamp.Generation)
	}
}

func TestRuntimeCoordinatorCollectsExistingRuntimeStoresWithoutMutatingThem(t *testing.T) {
	now := time.Now().UTC()
	api := &StreamingAPI{
		runtimeCoordinator: NewRuntimeCoordinator(),
		activeSessions: map[string]*ActiveSessionInfo{
			"session-1": {
				SessionID:    "session-1",
				Status:       "running",
				CreatedAt:    now.Add(-time.Minute),
				LastActivity: now,
			},
		},
		sessionBusy: map[string]bool{"session-1": true},
		trackedWorkflowExecutions: map[string]*TrackedWorkflowExecution{
			"child-1": {
				ExecutionID: "child-1",
				SessionID:   "session-1",
				Kind:        "workflow_step",
				Status:      trackedExecutionStatusRunning,
				StartedAt:   now.Add(-30 * time.Second),
			},
		},
	}

	snapshot, changed := api.observeRuntimeSnapshot("session-1")
	if !changed || snapshot.Phase != runtimePhaseRunning || len(snapshot.ChildExecutions) != 1 {
		t.Fatalf("running aggregate = %#v changed=%t", snapshot, changed)
	}
	if api.activeSessions["session-1"].Status != "running" || api.trackedWorkflowExecutions["child-1"].Status != trackedExecutionStatusRunning {
		t.Fatal("observer changed an authoritative runtime store")
	}

	completedAt := now.Add(time.Second)
	api.sessionBusy["session-1"] = false
	api.activeSessions["session-1"].Status = "completed"
	api.activeSessions["session-1"].LastActivity = completedAt
	api.trackedWorkflowExecutions["child-1"].Status = trackedExecutionStatusCompleted
	api.trackedWorkflowExecutions["child-1"].CompletedAt = &completedAt

	completed, changed := api.observeRuntimeSnapshot("session-1")
	if !changed || completed.Phase != runtimePhaseCompleted || completed.CompletedAt == nil {
		t.Fatalf("completed aggregate = %#v changed=%t", completed, changed)
	}
}
