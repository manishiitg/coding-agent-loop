package server

import (
	"context"
	"testing"
	"time"

	"github.com/manishiitg/coding-agent-loop/agent_go/cmd/server/services"
)

func TestResolveBotResumeTargetLatestDashboardSession(t *testing.T) {
	now := time.Now()
	api := &StreamingAPI{
		activeSessions: map[string]*ActiveSessionInfo{
			"old": {
				SessionID:    "old",
				AgentMode:    "multi-agent",
				Status:       "running",
				UserID:       "user-1",
				LastActivity: now.Add(-time.Minute),
				Query:        "older chat",
			},
			"bot-newer": {
				SessionID:    "bot-newer",
				AgentMode:    "multi-agent",
				Status:       "running",
				UserID:       "user-1",
				LastActivity: now,
				BotPlatform:  "whatsapp",
			},
			"workflow": {
				SessionID:     "workflow",
				AgentMode:     "workflow_phase",
				Status:        "running",
				UserID:        "user-1",
				LastActivity:  now.Add(-time.Second),
				Query:         "workflow chat",
				WorkspacePath: "Workflow/report",
				PresetQueryID: "preset-report",
				PhaseID:       "workflow-builder",
				WorkshopMode:  "run",
			},
		},
	}

	target, err := api.resolveBotResumeTarget(context.Background(), "user-1", "latest", services.BotResumeFilter{})
	if err != nil {
		t.Fatalf("resolveBotResumeTarget returned error: %v", err)
	}
	if target == nil {
		t.Fatal("expected target")
	}
	if target.SessionID != "workflow" {
		t.Fatalf("SessionID = %q, want workflow", target.SessionID)
	}
	if target.AgentMode != "workflow_phase" || target.WorkspacePath != "Workflow/report" || target.PresetQueryID != "preset-report" || target.WorkshopMode != "run" {
		t.Fatalf("target metadata = %+v", target)
	}
}

func TestResolveBotResumeTargetByPrefix(t *testing.T) {
	api := &StreamingAPI{
		activeSessions: map[string]*ActiveSessionInfo{
			"abc-123": {
				SessionID:    "abc-123",
				AgentMode:    "multi-agent",
				Status:       "completed",
				UserID:       "user-1",
				LastActivity: time.Now(),
			},
		},
	}

	target, err := api.resolveBotResumeTarget(context.Background(), "user-1", "abc", services.BotResumeFilter{})
	if err != nil {
		t.Fatalf("resolveBotResumeTarget returned error: %v", err)
	}
	if target == nil || target.SessionID != "abc-123" {
		t.Fatalf("target = %+v, want abc-123", target)
	}
}

func TestResolveBotResumeTargetByOrdinalWithWorkflowFilter(t *testing.T) {
	now := time.Now()
	api := &StreamingAPI{
		activeSessions: map[string]*ActiveSessionInfo{
			"new-report": {
				SessionID:     "new-report",
				AgentMode:     "workflow_phase",
				Status:        "running",
				UserID:        "user-1",
				LastActivity:  now,
				WorkspacePath: "Workflow/report",
				PresetQueryID: "preset-report",
			},
			"old-report": {
				SessionID:     "old-report",
				AgentMode:     "workflow_phase",
				Status:        "running",
				UserID:        "user-1",
				LastActivity:  now.Add(-time.Minute),
				WorkspacePath: "Workflow/report",
				PresetQueryID: "preset-report",
			},
			"other": {
				SessionID:     "other",
				AgentMode:     "workflow_phase",
				Status:        "running",
				UserID:        "user-1",
				LastActivity:  now.Add(time.Minute),
				WorkspacePath: "Workflow/other",
				PresetQueryID: "preset-other",
			},
		},
	}

	filter := services.BotResumeFilter{WorkspacePath: "Workflow/report", PresetQueryID: "preset-report"}
	target, err := api.resolveBotResumeTarget(context.Background(), "user-1", "2", filter)
	if err != nil {
		t.Fatalf("resolveBotResumeTarget returned error: %v", err)
	}
	if target == nil || target.SessionID != "old-report" {
		t.Fatalf("target = %+v, want old-report", target)
	}
}
