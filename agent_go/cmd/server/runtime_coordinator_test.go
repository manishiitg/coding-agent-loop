package server

import (
	"testing"
	"time"
)

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

	snapshot, changed := api.observeRuntimeSnapshot("session-1", nil)
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

	completed, changed := api.observeRuntimeSnapshot("session-1", nil)
	if !changed || completed.Phase != runtimePhaseCompleted || completed.CompletedAt == nil {
		t.Fatalf("completed aggregate = %#v changed=%t", completed, changed)
	}
}

func TestRuntimeCoordinatorLegacyMismatchIsDeduplicatedAndCleared(t *testing.T) {
	coordinator := NewRuntimeCoordinator()
	snapshot, _ := coordinator.Observe(RuntimeSnapshot{SessionID: "session-1", Phase: runtimePhaseRunning})
	mismatch := SessionDisplayStatus{Status: sessionExecutionDisplayIdle}
	coordinator.CompareLegacy(snapshot, mismatch)

	record := coordinator.records["session-1"]
	if record.lastMismatchSignature == "" {
		t.Fatal("expected mismatch signature")
	}
	signature := record.lastMismatchSignature
	coordinator.CompareLegacy(snapshot, mismatch)
	if coordinator.records["session-1"].lastMismatchSignature != signature {
		t.Fatal("identical mismatch was not deduplicated")
	}

	coordinator.CompareLegacy(snapshot, SessionDisplayStatus{Status: sessionExecutionDisplayBusy})
	if coordinator.records["session-1"].lastMismatchSignature != "" {
		t.Fatal("matching legacy state did not clear mismatch signature")
	}
}
