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

// TestSteerBackgroundAgentCompletionDeliversToBusySteerableSession verifies the
// fix for dropped auto-notifications: when the target session is busy but is a
// steerable CLI coding agent, a finished background agent's completion is
// injected into the running turn (live steer) instead of being queued behind an
// idle window the session may never reach.
func TestSteerBackgroundAgentCompletionDeliversToBusySteerableSession(t *testing.T) {
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

	if !api.steerBackgroundAgentCompletion(sessionID, bg.ID) {
		t.Fatal("steerBackgroundAgentCompletion = false; want true (delivered via live steer)")
	}

	// The completion must be flagged notified so the queue/drain backstop and the
	// requeue sweep do not deliver it a second time.
	bg.mu.RLock()
	notified := bg.notified
	bg.mu.RUnlock()
	if !notified {
		t.Fatal("agent.notified = false after successful steer; want true to prevent double-delivery")
	}

	queued := runningAgent.DrainSteerMessages()
	if len(queued) != 1 {
		t.Fatalf("steered messages = %d, want 1: %#v", len(queued), queued)
	}
	if !strings.Contains(queued[0], "[AUTO-NOTIFICATION]") || !strings.Contains(queued[0], "status=failed") {
		t.Fatalf("steered message missing expected content: %q", queued[0])
	}

	// Calling again must be a no-op delivery (already notified): reported handled,
	// but nothing re-injected into the running agent.
	if !api.steerBackgroundAgentCompletion(sessionID, bg.ID) {
		t.Fatal("second steer call = false; an already-notified completion should report handled")
	}
	if got := runningAgent.DrainSteerMessages(); len(got) != 0 {
		t.Fatalf("second steer call re-delivered %d messages; want 0", len(got))
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
