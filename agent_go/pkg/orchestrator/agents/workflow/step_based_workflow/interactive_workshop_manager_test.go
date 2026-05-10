package step_based_workflow

import (
	"context"
	"testing"
	"time"
)

func TestWorkshopModeAllowsOrphanStepConfigCleanup(t *testing.T) {
	for _, mode := range []string{"builder", "optimizer"} {
		tools := GetToolsForWorkshopMode(mode)
		if !containsToolName(tools, "cleanup_orphan_step_configs") {
			t.Fatalf("expected %s mode to allow cleanup_orphan_step_configs", mode)
		}
	}
}

func TestWorkshopModesAllowCalendarScheduleToolWhereSchedulesAreEditable(t *testing.T) {
	for _, mode := range []string{"builder", "optimizer"} {
		tools := GetToolsForWorkshopMode(mode)
		if !containsToolName(tools, "create_calendar_schedule") {
			t.Fatalf("expected %s mode to allow create_calendar_schedule", mode)
		}
	}
}

func TestOptimizerToolAgentAllowsArtifactMaintenanceTools(t *testing.T) {
	tools := optimizerToolAgentAllowedToolNames()
	for _, name := range []string{
		"cleanup_orphan_step_configs",
		"validate_evaluation_plan",
		"get_report_plan",
		"upsert_report_widget",
		"validate_report_plan",
		"preview_report_render",
		"improve_kb",
		"improve_db",
		"update_workflow_config",
		"set_workflow_llm_config",
	} {
		if !containsToolName(tools, name) {
			t.Fatalf("expected optimizer tool agent to allow %s", name)
		}
	}
}

func TestOptimizerToolAgentBlocksRecursiveAndScheduleTools(t *testing.T) {
	tools := optimizerToolAgentAllowedToolNames()
	for _, name := range []string{
		"execute_step",
		"run_full_workflow",
		"run_full_evaluation",
		"harden_workflow",
		"replan_workflow_from_results",
		"create_schedule",
		"delete_schedule",
		"trigger_schedule",
		"set_user_secret",
		"delete_user_secret",
		"publish_workflow_version",
		"restore_workflow_version",
	} {
		if containsToolName(tools, name) {
			t.Fatalf("expected optimizer tool agent to block %s", name)
		}
	}
}

func containsToolName(tools []string, name string) bool {
	for _, tool := range tools {
		if tool == name {
			return true
		}
	}
	return false
}

func TestWorkshopSubAgentNotifierRegistersCancelableExecution(t *testing.T) {
	registry := NewWorkshopStepRegistry()
	notifier := &workshopSubAgentNotifier{registry: registry}

	cancelCalled := make(chan struct{}, 1)
	cancel := func() {
		select {
		case cancelCalled <- struct{}{}:
		default:
		}
	}

	notifier.OnSubAgentStart(WorkshopExecutionStart{
		ID:     "todo-sub-step-9-functional-test-agent",
		Name:   "Functional Test Agent",
		Cancel: cancel,
	})

	exec := registry.Get("todo-sub-step-9-functional-test-agent")
	if exec == nil {
		t.Fatal("expected execution to be registered")
	}
	if exec.cancel == nil {
		t.Fatal("expected registered execution to keep its cancel function")
	}

	canceled := registry.CancelAll()
	if len(canceled) != 1 {
		t.Fatalf("expected 1 canceled execution, got %d", len(canceled))
	}

	select {
	case <-cancelCalled:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected cancel function to be invoked")
	}
}

func TestWorkshopStepRegistryCancelAllMarksLegacyExecutionsCancelled(t *testing.T) {
	registry := NewWorkshopStepRegistry()
	registry.Register(&WorkshopStepExecution{
		ID:     "legacy-sub-agent",
		StepID: "Legacy Sub-Agent",
		Status: WorkshopStepRunning,
		cancel: nil,
	})

	canceled := registry.CancelAll()
	if len(canceled) != 1 {
		t.Fatalf("expected 1 canceled execution, got %d", len(canceled))
	}

	exec := registry.Get("legacy-sub-agent")
	if exec == nil {
		t.Fatal("expected legacy execution to remain registered")
	}
	if exec.Status != WorkshopStepCancelled {
		t.Fatalf("expected legacy execution status %q, got %q", WorkshopStepCancelled, exec.Status)
	}
}

func TestWorkshopStepRegistryCancelReturnsRawExecutionSnapshot(t *testing.T) {
	registry := NewWorkshopStepRegistry()
	cancelCalled := make(chan struct{}, 1)
	registry.Register(&WorkshopStepExecution{
		ID:     "exec-step-123",
		StepID: "step-123",
		Status: WorkshopStepRunning,
		cancel: func() {
			select {
			case cancelCalled <- struct{}{}:
			default:
			}
		},
	})

	snap, err := registry.Cancel("exec-step-123")
	if err != nil {
		t.Fatalf("expected cancel to succeed, got %v", err)
	}
	if snap.ID != "exec-step-123" {
		t.Fatalf("expected raw execution ID, got %q", snap.ID)
	}
	if snap.StepID != "step-123" {
		t.Fatalf("expected step ID to be preserved, got %q", snap.StepID)
	}
	if snap.Status != WorkshopStepCancelled {
		t.Fatalf("expected canceled status, got %q", snap.Status)
	}

	select {
	case <-cancelCalled:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected cancel function to be invoked")
	}
}

func TestCompositeSubAgentNotifierForwardsCancelFunc(t *testing.T) {
	var gotCancel context.CancelFunc
	notifier := &recordingSubAgentNotifier{
		onStart: func(_ string, _ string, cancel context.CancelFunc) {
			gotCancel = cancel
		},
	}
	cancel := func() {}

	composite := ChainSubAgentNotifiers(notifier)
	composite.OnSubAgentStart(WorkshopExecutionStart{
		ID:     "agent-1",
		Name:   "Agent 1",
		Cancel: cancel,
	})

	if gotCancel == nil {
		t.Fatal("expected composite notifier to forward cancel func")
	}
}

type recordingSubAgentNotifier struct {
	onStart func(agentID, name string, cancel context.CancelFunc)
}

func (r *recordingSubAgentNotifier) OnSubAgentStart(start WorkshopExecutionStart) {
	if r.onStart != nil {
		r.onStart(start.ID, start.Name, start.Cancel)
	}
}

func (r *recordingSubAgentNotifier) OnSubAgentComplete(agentID, name, result string, err error) {}
