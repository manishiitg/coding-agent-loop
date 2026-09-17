package server

import (
	"context"
	"testing"
	"time"

	mcpagent "github.com/manishiitg/mcpagent/agent"
	"github.com/manishiitg/mcpagent/llm"
)

func TestBackgroundRegistryCannotRehomeScheduledExecution(t *testing.T) {
	registry := NewBackgroundAgentRegistry()
	scheduled := &BackgroundAgent{ID: "step", SessionID: "schedule-cron--job_1", Status: BGAgentCompleted}
	registry.Register(scheduled.SessionID, scheduled)
	registry.Register("unrelated-chat", scheduled)
	if registry.Get("unrelated-chat", scheduled.ID) != nil {
		t.Fatal("scheduled execution was registered in an unrelated chat")
	}
	if registry.Get(scheduled.SessionID, scheduled.ID) != scheduled {
		t.Fatal("original owner lost its execution")
	}
	child := &BackgroundAgent{ID: "chat-step"}
	registry.Register("chat", child)
	if child.GetSnapshot().SessionID != "chat" {
		t.Fatal("launch session was not bound at registration")
	}
	registry.Register("another-chat", child)
	if registry.Get("another-chat", child.ID) != nil {
		t.Fatal("bound child was re-homed")
	}
}

func TestBackgroundNotificationOwnershipPreservesInitiatingSession(t *testing.T) {
	api := lifecycleTestAPI()
	api.trackedWorkflowExecutions["schedule-root"] = &TrackedWorkflowExecution{SessionID: "schedule-cron--job_1", Status: trackedExecutionStatusCompleted}
	api.trackedWorkflowExecutions["chat-root"] = &TrackedWorkflowExecution{SessionID: "chat", Status: trackedExecutionStatusCompleted}
	api.trackedWorkflowExecutions["new-chat-turn"] = &TrackedWorkflowExecution{SessionID: "chat", Source: trackedExecutionSourceConversationTurn, Status: trackedExecutionStatusRunning, StartedAt: time.Now()}
	for _, tc := range []struct {
		name, session, owner, parent string
		want                         bool
	}{
		{"scheduled owner", "schedule-cron--job_1", "schedule-cron--job_1", "schedule-root", true},
		{"foreign scheduled owner", "chat", "schedule-cron--job_1", "schedule-root", false},
		{"foreign parent despite wrong registry bucket", "chat", "chat", "schedule-root", false},
		{"chat work after another human turn", "chat", "chat", "chat-root", true},
		{"foreign session root", "chat", "chat", "session:other-chat", false},
		{"legacy owner without tracked parent", "chat", "chat", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := api.backgroundNotificationOwnedBySession(tc.session, BackgroundAgentSnapshot{SessionID: tc.owner, ParentExecutionID: tc.parent})
			if got != tc.want {
				t.Fatalf("owned = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestForeignCompletionCannotSteerOrEnterNotificationBatch(t *testing.T) {
	api := lifecycleTestAPI()
	agent := testCodingAgent(llm.ProviderCodexCLI, "codex-cli")
	api.runningAgents = map[string]*mcpagent.Agent{"chat": agent}
	api.agentCancelFuncs = map[string]context.CancelFunc{"chat": func() {}}
	api.trackedWorkflowExecutions["schedule-root"] = &TrackedWorkflowExecution{SessionID: "schedule-cron--job_1"}
	called := false
	api.internalUserMessageDeliveryHandler = func(context.Context, *mcpagent.Agent, mcpagent.UserMessageDeliveryRequest) (mcpagent.UserMessageDeliveryResult, error) {
		called = true
		return mcpagent.UserMessageDeliveryResult{}, nil
	}
	// Simulate a legacy/misrouted registration with a known foreign launch parent.
	foreign := &BackgroundAgent{ID: "foreign", SessionID: "chat", ParentExecutionID: "schedule-root", Status: BGAgentCompleted}
	own := &BackgroundAgent{ID: "own", SessionID: "chat", Status: BGAgentCompleted}
	api.bgAgentRegistry.Register("chat", foreign)
	api.bgAgentRegistry.Register("chat", own)
	if !api.steerBackgroundAgentCompletion("chat", foreign.ID) {
		t.Fatal("foreign completion must be consumed without a retry into this chat")
	}
	if called {
		t.Fatal("foreign completion was delivered into the running chat")
	}
	ids := api.filterSupersededCompletions("chat", []string{foreign.ID, own.ID})
	if len(ids) != 1 || ids[0] != own.ID {
		t.Fatalf("batch = %v, want only chat-owned work", ids)
	}
	api.processBackgroundAgentCompletion("chat", foreign.ID)
	api.processBatchedBackgroundAgentCompletions("chat", []string{foreign.ID})
	if called {
		t.Fatal("queued completion delivered into unrelated chat")
	}
}

func TestAutoNotificationSweepDoesNotResurrectSuppressedCompletedChild(t *testing.T) {
	api := lifecycleTestAPI()
	old := time.Now().Add(-time.Minute)
	suppressed := &BackgroundAgent{ID: "msgseq-verify-and-repair", SessionID: "chat", Status: BGAgentCompleted, CreatedAt: old, Metadata: map[string]string{"suppress_auto_notification": "true", "execution_type": "workflow-step"}}
	own := &BackgroundAgent{ID: "legitimate-step", SessionID: "chat", Status: BGAgentCompleted, CreatedAt: old}
	api.bgAgentRegistry.Register("chat", suppressed)
	api.bgAgentRegistry.Register("chat", own)
	// This is the production failure: the notifier omitted its channel send, but
	// a later retry sweep runs after the minimum completion delay has expired.
	api.requeueUnnotifiedCompletions("chat")
	pending := api.drainPendingCompletions("chat")
	if len(pending) != 1 || pending[0] != own.ID {
		t.Fatalf("sweep resurrected a parent-reconciled child: %v", pending)
	}
	if suppressed.beginCompletionNotification() {
		t.Fatal("suppressed completion acquired delivery claim")
	}
	if !own.beginCompletionNotification() {
		t.Fatal("legitimate same-session completion lost its claim")
	}
}

func TestAutoNotificationSuppressionSurvivesAlreadyQueuedDispatch(t *testing.T) {
	for _, lane := range []string{"single", "batch", "steer-without-agent", "steer-with-agent"} {
		t.Run(lane, func(t *testing.T) {
			api := lifecycleTestAPI()
			suppressed := &BackgroundAgent{ID: "suppressed", SessionID: "chat", Status: BGAgentCompleted, CreatedAt: time.Now().Add(-time.Minute), Metadata: map[string]string{"suppress_auto_notification": "true", "execution_type": "workflow-step"}}
			own := &BackgroundAgent{ID: "own", SessionID: "chat", Status: BGAgentCompleted}
			api.bgAgentRegistry.Register("chat", suppressed)
			api.bgAgentRegistry.Register("chat", own)
			called := false
			api.internalUserMessageDeliveryHandler = func(context.Context, *mcpagent.Agent, mcpagent.UserMessageDeliveryRequest) (mcpagent.UserMessageDeliveryResult, error) {
				called = true
				return mcpagent.UserMessageDeliveryResult{}, nil
			}
			switch lane {
			case "single":
				api.processBackgroundAgentCompletion("chat", suppressed.ID)
			case "batch":
				api.processBatchedBackgroundAgentCompletions("chat", []string{suppressed.ID})
			case "steer-with-agent":
				api.runningAgents = map[string]*mcpagent.Agent{"chat": testCodingAgent(llm.ProviderCodexCLI, "codex-cli")}
				if !api.steerBackgroundAgentCompletion("chat", suppressed.ID) {
					t.Fatal("suppressed completion was returned for retry")
				}
			case "steer-without-agent":
				if !api.steerBackgroundAgentCompletion("chat", suppressed.ID) {
					t.Fatal("suppressed completion was returned for retry")
				}
			}
			if called || len(api.drainPendingCompletions("chat")) != 0 {
				t.Fatal("suppressed completion dispatched or requeued")
			}
			suppressed.mu.RLock()
			state, delayed := suppressed.completionNotification, suppressed.completionDelayScheduled
			suppressed.mu.RUnlock()
			if state != completionNotificationNone || delayed {
				t.Fatal("suppressed work was claimed, marked delivered, or given a timer")
			}
			eligible := api.filterSupersededCompletions("chat", []string{suppressed.ID, own.ID})
			if len(eligible) != 1 || eligible[0] != own.ID {
				t.Fatalf("legitimate completion eligibility changed: %v", eligible)
			}
		})
	}
}
