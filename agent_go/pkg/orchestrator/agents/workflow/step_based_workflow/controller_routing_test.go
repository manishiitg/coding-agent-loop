package step_based_workflow

import (
	"context"
	"strings"
	"testing"

	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"
	virtualtools "mcp-agent-builder-go/agent_go/cmd/server/virtual-tools"
	"mcp-agent-builder-go/agent_go/pkg/orchestrator"
)

type recordedRoutingCompletion struct {
	execID string
	name   string
	result string
	meta   map[string]string
	err    error
}

type recordingRoutingNotifier struct {
	starts    []WorkshopExecutionStart
	completes []recordedRoutingCompletion
}

func (n *recordingRoutingNotifier) OnExecutionStart(start WorkshopExecutionStart) {
	n.starts = append(n.starts, start)
}

func (n *recordingRoutingNotifier) OnExecutionComplete(execID, name, result string, meta map[string]string, err error) {
	n.completes = append(n.completes, recordedRoutingCompletion{
		execID: execID,
		name:   name,
		result: result,
		meta:   meta,
		err:    err,
	})
}

func (n *recordingRoutingNotifier) OnExecutionTerminated(execID, name string) {}

func TestEvaluateRoutingViaBuilderChatCompletesRoutingPickNotification(t *testing.T) {
	base, err := orchestrator.NewBaseOrchestrator(
		loggerv2.NewDefault(),
		nil,
		orchestrator.OrchestratorTypeWorkflow,
		"",
		0,
		"",
		nil,
		nil,
		false,
		&orchestrator.LLMConfig{},
		1,
		nil,
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("NewBaseOrchestrator returned error: %v", err)
	}

	const workflowSessionID = "workflow-session-routing-pick-notification"
	const parentSessionID = "builder-session-routing-pick-notification"
	notifier := &recordingRoutingNotifier{}
	hcpo := &StepBasedWorkflowOrchestrator{
		BaseOrchestrator: base,
		sessionID:        workflowSessionID,
	}
	hcpo.SetRoutingDecisionNotifier(notifier)

	virtualtools.RegisterParentChat(workflowSessionID, &virtualtools.ParentChatContext{
		SessionID:    parentSessionID,
		WorkflowPath: "Workflow/demo",
		GroupName:    "demo-group",
		AgentID:      "workflow-full-123",
	})
	t.Cleanup(func() {
		virtualtools.UnregisterParentChat(workflowSessionID)
		virtualtools.SetChatInjector(nil)
	})

	virtualtools.SetChatInjector(func(ctx context.Context, sessionID, userID, message string) error {
		if sessionID != parentSessionID {
			t.Fatalf("unexpected injected session: %s", sessionID)
		}
		if !strings.Contains(message, "[WORKFLOW_ROUTING]") {
			t.Fatalf("expected workflow routing marker in injected message, got %q", message)
		}
		requestID := requestIDFromRoutingMessage(message)
		if requestID == "" {
			t.Fatalf("expected request ID in injected message, got %q", message)
		}
		return virtualtools.GetHumanFeedbackStore().SubmitResponse(requestID, "route-b")
	})

	resp, ok := hcpo.evaluateRoutingViaBuilderChat(context.Background(), &RoutingPlanStep{
		CommonStepFields: CommonStepFields{
			ID:    "route-pick-topic",
			Title: "Route Pick Topic",
		},
		RoutingQuestion: "Which route should run?",
		Routes: []RoutingRoute{
			{RouteID: "route-a", RouteName: "Route A", Condition: "A", NextStepID: "step-a"},
			{RouteID: "route-b", RouteName: "Route B", Condition: "B", NextStepID: "step-b"},
		},
	}, "", "")
	if !ok {
		t.Fatal("expected builder chat routing to succeed")
	}
	if resp.SelectedRouteID != "route-b" {
		t.Fatalf("expected selected route-b, got %q", resp.SelectedRouteID)
	}

	if len(notifier.starts) != 1 {
		t.Fatalf("expected one routing notification start, got %d", len(notifier.starts))
	}
	if got := notifier.starts[0].Kind; got != "workflow_routing_pick" {
		t.Fatalf("expected workflow_routing_pick kind, got %q", got)
	}
	if got := notifier.starts[0].ParentExecutionID; got != "workflow-full-123" {
		t.Fatalf("expected parent workflow execution ID, got %q", got)
	}

	if len(notifier.completes) != 1 {
		t.Fatalf("expected one routing notification completion, got %d", len(notifier.completes))
	}
	completion := notifier.completes[0]
	if completion.err != nil {
		t.Fatalf("expected successful routing completion, got %v", completion.err)
	}
	if completion.execID != notifier.starts[0].ID {
		t.Fatalf("completion exec ID %q did not match start ID %q", completion.execID, notifier.starts[0].ID)
	}
	if got := completion.meta["selected_route_id"]; got != "route-b" {
		t.Fatalf("expected selected route metadata route-b, got %q", got)
	}
	if got := completion.meta["group_name"]; got != "demo-group" {
		t.Fatalf("expected group metadata demo-group, got %q", got)
	}
	if !strings.Contains(completion.result, `Selected route "route-b"`) {
		t.Fatalf("expected selected route in result, got %q", completion.result)
	}
}

func TestEvaluateRoutingViaBuilderChatTreatsPlainNumberAsDisplayedOneBasedIndex(t *testing.T) {
	base, err := orchestrator.NewBaseOrchestrator(
		loggerv2.NewDefault(),
		nil,
		orchestrator.OrchestratorTypeWorkflow,
		"",
		0,
		"",
		nil,
		nil,
		false,
		&orchestrator.LLMConfig{},
		1,
		nil,
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("NewBaseOrchestrator returned error: %v", err)
	}

	const workflowSessionID = "workflow-session-routing-pick-number"
	const parentSessionID = "builder-session-routing-pick-number"
	hcpo := &StepBasedWorkflowOrchestrator{
		BaseOrchestrator: base,
		sessionID:        workflowSessionID,
	}

	virtualtools.RegisterParentChat(workflowSessionID, &virtualtools.ParentChatContext{
		SessionID: parentSessionID,
	})
	t.Cleanup(func() {
		virtualtools.UnregisterParentChat(workflowSessionID)
		virtualtools.SetChatInjector(nil)
	})

	virtualtools.SetChatInjector(func(ctx context.Context, sessionID, userID, message string) error {
		if sessionID != parentSessionID {
			t.Fatalf("unexpected injected session: %s", sessionID)
		}
		requestID := requestIDFromRoutingMessage(message)
		if requestID == "" {
			t.Fatalf("expected request ID in injected message, got %q", message)
		}
		return virtualtools.GetHumanFeedbackStore().SubmitResponse(requestID, "1")
	})

	resp, ok := hcpo.evaluateRoutingViaBuilderChat(context.Background(), &RoutingPlanStep{
		CommonStepFields: CommonStepFields{
			ID:    "route-pick-number",
			Title: "Route Pick Number",
		},
		RoutingQuestion: "Which route should run?",
		Routes: []RoutingRoute{
			{RouteID: "route-a", RouteName: "Route A", Condition: "A", NextStepID: "step-a"},
			{RouteID: "route-b", RouteName: "Route B", Condition: "B", NextStepID: "step-b"},
		},
	}, "", "")
	if !ok {
		t.Fatal("expected builder chat routing to succeed")
	}
	if resp.SelectedRouteID != "route-a" {
		t.Fatalf("expected displayed option 1 to select route-a, got %q", resp.SelectedRouteID)
	}
}

func requestIDFromRoutingMessage(message string) string {
	for _, line := range strings.Split(message, "\n") {
		if strings.HasPrefix(line, "Request ID:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "Request ID:"))
		}
	}
	return ""
}
