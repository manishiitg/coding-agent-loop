package step_based_workflow

import (
	"testing"

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
