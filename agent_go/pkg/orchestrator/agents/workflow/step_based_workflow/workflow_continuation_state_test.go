package step_based_workflow

import (
	"testing"
	"time"

	mcpagent "github.com/manishiitg/mcpagent/agent"
	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

func TestWorkflowContinuationStatePathUsesStepLogFolder(t *testing.T) {
	got := workflowContinuationStatePath("Workflow/demo", "iteration-0/group-a", "step-output", "step-2")
	want := "Workflow/demo/runs/iteration-0/group-a/logs/step-output/continuation_state.json"
	if got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
}

func TestUpsertWorkflowContinuationStatePreservesHandleAcrossPhaseUpdates(t *testing.T) {
	firstTime := time.Date(2026, 5, 22, 10, 0, 0, 0, time.UTC)
	handle := &mcpagent.AgentSessionHandle{
		SessionID: "session-1",
		Provider: llmtypes.CodingProviderSessionHandle{
			Provider:        "codex-cli",
			Transport:       "tmux",
			NativeSessionID: "codex-thread-1",
			TmuxSession:     "tmux-1",
			WorkingDir:      "/tmp/work",
		},
	}
	state := upsertWorkflowContinuationState(nil, workflowContinuationStateUpdate{
		workspacePath: "Workflow/demo",
		runFolder:     "iteration-0/group-a",
		stepID:        "step-output",
		stepPath:      "step-2",
		ownerKind:     workflowContinuationOwnerStepExecution,
		phase:         workflowContinuationPhaseMainExecution,
		status:        workflowContinuationStatusCompleted,
		handle:        handle,
		now:           firstTime,
	})

	if state.AgentSessionHandle == nil || state.AgentSessionHandle.Provider.NativeSessionID != "codex-thread-1" {
		t.Fatalf("handle not stored: %#v", state.AgentSessionHandle)
	}
	if got := state.Phases[workflowContinuationPhaseMainExecution].Status; got != workflowContinuationStatusCompleted {
		t.Fatalf("main phase status = %q", got)
	}

	secondTime := firstTime.Add(time.Minute)
	state = upsertWorkflowContinuationState(state, workflowContinuationStateUpdate{
		workspacePath: "Workflow/demo",
		runFolder:     "iteration-0/group-a",
		stepID:        "step-output",
		stepPath:      "step-2",
		ownerKind:     workflowContinuationOwnerStepExecution,
		phase:         workflowContinuationPhaseDirectLearning,
		status:        workflowContinuationStatusWaitingForLock,
		now:           secondTime,
	})

	if state.AgentSessionHandle == nil || state.AgentSessionHandle.Provider.NativeSessionID != "codex-thread-1" {
		t.Fatalf("handle should survive nil-handle phase update: %#v", state.AgentSessionHandle)
	}
	if got := state.Phases[workflowContinuationPhaseDirectLearning].Status; got != workflowContinuationStatusWaitingForLock {
		t.Fatalf("direct learning phase status = %q", got)
	}
	if phaseHandle := state.Phases[workflowContinuationPhaseMainExecution].AgentSessionHandle; phaseHandle == nil || phaseHandle.Provider.NativeSessionID != "codex-thread-1" {
		t.Fatalf("main phase handle not stored: %#v", phaseHandle)
	}
	if state.UpdatedAt != secondTime.Format(time.RFC3339Nano) {
		t.Fatalf("updated_at = %q", state.UpdatedAt)
	}
}

func TestUpsertWorkflowContinuationStatePreservesPerPhaseHandle(t *testing.T) {
	firstHandle := &mcpagent.AgentSessionHandle{
		SessionID: "session-main",
		Provider: llmtypes.CodingProviderSessionHandle{
			Provider:        "claude-code",
			Transport:       "tmux",
			NativeSessionID: "claude-main",
		},
	}
	learningHandle := &mcpagent.AgentSessionHandle{
		SessionID: "session-learning",
		Provider: llmtypes.CodingProviderSessionHandle{
			Provider:        "claude-code",
			Transport:       "tmux",
			NativeSessionID: "claude-learning",
		},
	}
	state := upsertWorkflowContinuationState(nil, workflowContinuationStateUpdate{
		workspacePath: "Workflow/demo",
		runFolder:     "iteration-0/group-a",
		stepID:        "step-output",
		stepPath:      "step-2",
		phase:         workflowContinuationPhaseMainExecution,
		status:        workflowContinuationStatusCompleted,
		handle:        firstHandle,
		now:           time.Date(2026, 5, 22, 10, 0, 0, 0, time.UTC),
	})
	state = upsertWorkflowContinuationState(state, workflowContinuationStateUpdate{
		workspacePath: "Workflow/demo",
		runFolder:     "iteration-0/group-a",
		stepID:        "step-output",
		stepPath:      "step-2",
		phase:         workflowContinuationPhaseLearningAgent,
		status:        workflowContinuationStatusRunning,
		handle:        learningHandle,
		now:           time.Date(2026, 5, 22, 10, 1, 0, 0, time.UTC),
	})
	state = upsertWorkflowContinuationState(state, workflowContinuationStateUpdate{
		workspacePath: "Workflow/demo",
		runFolder:     "iteration-0/group-a",
		stepID:        "step-output",
		stepPath:      "step-2",
		phase:         workflowContinuationPhaseLearningAgent,
		status:        workflowContinuationStatusWaitingForLock,
		now:           time.Date(2026, 5, 22, 10, 2, 0, 0, time.UTC),
	})

	if got := workflowContinuationPhaseHandle(state, workflowContinuationPhaseMainExecution).Provider.NativeSessionID; got != "claude-main" {
		t.Fatalf("main handle = %q", got)
	}
	if got := workflowContinuationPhaseHandle(state, workflowContinuationPhaseLearningAgent).Provider.NativeSessionID; got != "claude-learning" {
		t.Fatalf("learning handle = %q", got)
	}
}

func TestWorkflowContinuationPendingRecoveryPhasesRequireCompletedGate(t *testing.T) {
	state := &WorkflowContinuationState{
		Phases: map[string]WorkflowContinuationPhaseRecord{
			workflowContinuationPhaseMainExecution: {
				Status: workflowContinuationStatusRunning,
			},
			workflowContinuationPhaseLearningAgent: {
				Status: workflowContinuationStatusWaitingForLock,
			},
		},
	}
	if phases := state.PendingRecoveryPhases(); len(phases) != 0 {
		t.Fatalf("pending phases before main completion = %v, want none", phases)
	}

	state.Phases[workflowContinuationPhaseMainExecution] = WorkflowContinuationPhaseRecord{Status: workflowContinuationStatusCompleted}
	state.Phases[workflowContinuationPhasePreValidation] = WorkflowContinuationPhaseRecord{Status: workflowContinuationStatusCompleted}
	state.Phases[workflowContinuationPhaseKBUpdateAgent] = WorkflowContinuationPhaseRecord{Status: workflowContinuationStatusPending}

	phases := state.PendingRecoveryPhases()
	if len(phases) != 2 {
		t.Fatalf("pending phases len = %d, want 2 (%v)", len(phases), phases)
	}
	if phases[0] != workflowContinuationPhaseLearningAgent || phases[1] != workflowContinuationPhaseKBUpdateAgent {
		t.Fatalf("pending phases = %v", phases)
	}
}
