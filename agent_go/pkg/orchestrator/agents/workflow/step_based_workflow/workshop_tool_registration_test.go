package step_based_workflow

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/orchestrator"
	mcpagent "github.com/manishiitg/mcpagent/agent"
	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"
)

type workshopToolTestLogger struct{}

func (workshopToolTestLogger) Debug(string, ...loggerv2.Field)          {}
func (workshopToolTestLogger) Info(string, ...loggerv2.Field)           {}
func (workshopToolTestLogger) Warn(string, ...loggerv2.Field)           {}
func (workshopToolTestLogger) Error(string, error, ...loggerv2.Field)   {}
func (workshopToolTestLogger) Fatal(string, error, ...loggerv2.Field)   {}
func (l workshopToolTestLogger) With(...loggerv2.Field) loggerv2.Logger { return l }
func (workshopToolTestLogger) Close() error                             { return nil }

func TestRegisterWorkshopChatToolsIncludesArtifactReviewMarker(t *testing.T) {
	agent := &mcpagent.Agent{}
	workspacePath := t.TempDir()
	base := &orchestrator.BaseOrchestrator{}
	base.SetWorkspacePath(workspacePath)
	session := &WorkshopChatSession{
		controller:   &StepBasedWorkflowOrchestrator{BaseOrchestrator: base},
		StepRegistry: NewWorkshopStepRegistry(),
		config:       &WorkshopConfig{WorkspacePath: workspacePath},
	}

	RegisterWorkshopChatTools(agent, session, workshopToolTestLogger{})

	tool, ok := agent.GetCustomTools()["mark_changelog_artifact_reviewed"]
	if !ok {
		t.Fatal("actual workshop agent registry is missing mark_changelog_artifact_reviewed")
	}
	if tool.Category != "workflow" {
		t.Fatalf("mark_changelog_artifact_reviewed category = %q, want workflow", tool.Category)
	}
	if agent.GetCustomToolExecutor("mark_changelog_artifact_reviewed") == nil {
		t.Fatal("mark_changelog_artifact_reviewed has no registered executor")
	}
}

func TestRunningStatusToolsShareRapidPollGuard(t *testing.T) {
	agent := &mcpagent.Agent{}
	workspacePath := t.TempDir()
	base, err := orchestrator.NewBaseOrchestrator(
		loggerv2.NewNoop(), nil, orchestrator.OrchestratorTypeWorkflow, "", 0, "",
		nil, nil, false, &orchestrator.LLMConfig{}, 1, nil, nil, nil,
	)
	if err != nil {
		t.Fatalf("NewBaseOrchestrator: %v", err)
	}
	base.SetWorkspacePath(workspacePath)
	registry := NewWorkshopStepRegistry()
	registry.Register(&WorkshopStepExecution{
		ID:     "workflow-1",
		StepID: "step-1",
		Status: WorkshopStepRunning,
	})
	session := &WorkshopChatSession{
		controller:   &StepBasedWorkflowOrchestrator{BaseOrchestrator: base},
		StepRegistry: registry,
		config:       &WorkshopConfig{WorkspacePath: workspacePath},
	}

	RegisterWorkshopChatTools(agent, session, workshopToolTestLogger{})
	queryStep := agent.GetCustomToolExecutor("query_step")
	listExecutions := agent.GetCustomToolExecutor("list_executions")
	if queryStep == nil || listExecutions == nil {
		t.Fatal("running-status tools were not registered")
	}

	first, err := queryStep(context.Background(), map[string]interface{}{"step_id": "step-1"})
	if err != nil {
		t.Fatalf("first query_step: %v", err)
	}
	if !strings.Contains(first, statusPollNextAction) || strings.Contains(first, statusPollSuppressed) {
		t.Fatalf("first status response should direct the agent to end its turn without suppression:\n%s", first)
	}

	second, err := listExecutions(context.Background(), map[string]interface{}{"status_filter": "running"})
	if err != nil {
		t.Fatalf("second list_executions: %v", err)
	}
	if !strings.Contains(second, statusPollWarning) || strings.Contains(second, statusPollSuppressed) {
		t.Fatalf("second cross-tool status response should warn without suppression:\n%s", second)
	}

	third, err := queryStep(context.Background(), map[string]interface{}{"step_id": "step-1"})
	if err != nil {
		t.Fatalf("third query_step: %v", err)
	}
	if third != statusPollSuppressed {
		t.Fatalf("third unchanged cross-tool status response = %q, want compact suppression", third)
	}
}

func TestRapidPollGuardAllowsChangedStateAndResetsAfterIdleWindow(t *testing.T) {
	registry := NewWorkshopStepRegistry()
	now := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)

	if got := registry.observeStatusPoll(now, "workflow-1:running"); got.Warn || got.Suppress {
		t.Fatalf("first decision = %+v, want allowed without warning", got)
	}
	if got := registry.observeStatusPoll(now.Add(time.Second), "workflow-1:running"); !got.Warn || got.Suppress {
		t.Fatalf("second decision = %+v, want warning only", got)
	}
	if got := registry.observeStatusPoll(now.Add(2*time.Second), "workflow-1:running"); !got.Suppress {
		t.Fatalf("third unchanged decision = %+v, want suppression", got)
	}
	if got := registry.observeStatusPoll(now.Add(3*time.Second), "workflow-1:done"); !got.Changed || got.Suppress {
		t.Fatalf("changed decision = %+v, want changed state returned", got)
	}
	if got := registry.observeStatusPoll(now.Add(statusPollWindow+4*time.Second), "workflow-2:running"); got.Count != 1 || got.Warn || got.Suppress {
		t.Fatalf("post-idle decision = %+v, want fresh budget", got)
	}
}
