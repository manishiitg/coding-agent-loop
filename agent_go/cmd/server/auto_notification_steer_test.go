package server

import (
	"context"
	"strings"
	"sync"
	"testing"

	mcpagent "github.com/manishiitg/mcpagent/agent"
	"github.com/manishiitg/mcpagent/llm"
	internalevents "mcp-agent-builder-go/agent_go/internal/events"
)

// TestSteerBackgroundAgentCompletionFallsBackForQueuedInjection verifies that a
// queued-for-injection steer is not treated as delivered. The queue/retry path
// must still own delivery until the provider confirms the notification reached
// the live CLI.
func TestSteerBackgroundAgentCompletionFallsBackForQueuedInjection(t *testing.T) {
	store := internalevents.NewEventStore(10)
	defer store.Stop()

	sessionID := "busy-steerable-session"
	runningAgent := &mcpagent.Agent{ModelID: "opencode"}
	runningAgent.SetProvider(llm.ProviderOpenCodeCLI)

	api := &StreamingAPI{
		eventStore:       store,
		runningAgents:    map[string]*mcpagent.Agent{sessionID: runningAgent},
		runningAgentsMux: sync.RWMutex{},
		// An active foreground turn cancel handle is the proof that makes the
		// session steerable (canSteerSession).
		agentCancelFuncs: map[string]context.CancelFunc{sessionID: func() {}},
		agentCancelMux:   sync.RWMutex{},
		bgAgentRegistry:  NewBackgroundAgentRegistry(),
	}

	bg := &BackgroundAgent{
		ID:        "workflow-full-1",
		Name:      "full-workflow [default / iteration-0]",
		SessionID: sessionID,
		Status:    BGAgentFailed,
		Error:     "step 12 execution failed: input file not found",
	}
	api.bgAgentRegistry.Register(sessionID, bg)

	if !api.canSteerSession(sessionID) {
		t.Fatal("precondition: expected session to be steerable")
	}

	if api.steerBackgroundAgentCompletion(sessionID, bg.ID) {
		t.Fatal("steerBackgroundAgentCompletion = true for queued injection; want false so caller queues")
	}

	// QueuedForInjection is not definitive delivery, so the completion must not
	// be flagged notified. The queue/drain backstop will redeliver it later.
	bg.mu.RLock()
	notified := bg.notified
	bg.mu.RUnlock()
	if notified {
		t.Fatal("agent.notified = true after queued injection; want false so the queue path can retry")
	}

	queued := runningAgent.DrainSteerMessages()
	if len(queued) != 1 {
		t.Fatalf("steered messages = %d, want 1: %#v", len(queued), queued)
	}
	if !strings.Contains(queued[0], "[AUTO-NOTIFICATION]") || !strings.Contains(queued[0], "status=failed") {
		t.Fatalf("steered message missing expected content: %q", queued[0])
	}

	if api.steerBackgroundAgentCompletion(sessionID, bg.ID) {
		t.Fatal("second queued-injection steer call = true; want false until delivery is confirmed")
	}
}

// TestSteerBackgroundAgentCompletionFallsBackWhenNoRunningAgent verifies that the
// steer path declines (returns false) when there is no running agent to receive
// the message, so the caller falls back to the queue + drain-on-idle backstop.
func TestSteerBackgroundAgentCompletionFallsBackWhenNoRunningAgent(t *testing.T) {
	api := &StreamingAPI{
		runningAgents:    map[string]*mcpagent.Agent{},
		runningAgentsMux: sync.RWMutex{},
		bgAgentRegistry:  NewBackgroundAgentRegistry(),
	}
	const sessionID = "no-running-agent"
	bg := &BackgroundAgent{ID: "bg-1", SessionID: sessionID, Status: BGAgentCompleted, Result: "done"}
	api.bgAgentRegistry.Register(sessionID, bg)

	if api.steerBackgroundAgentCompletion(sessionID, bg.ID) {
		t.Fatal("steerBackgroundAgentCompletion = true with no running agent; want false so caller queues")
	}
	bg.mu.RLock()
	notified := bg.notified
	bg.mu.RUnlock()
	if notified {
		t.Fatal("agent.notified = true after a declined steer; the queue path must still own delivery")
	}
}
