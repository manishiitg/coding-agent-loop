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
	if state.UpdatedAt != secondTime.Format(time.RFC3339Nano) {
		t.Fatalf("updated_at = %q", state.UpdatedAt)
	}
}
