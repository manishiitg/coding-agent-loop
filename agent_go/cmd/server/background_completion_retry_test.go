package server

import (
	"sort"
	"sync"
	"sync/atomic"
	"testing"
)

// TestRequeueUnnotifiedCompletions verifies the backstop sweep: it must re-queue
// exactly the agents whose execution finished (completed/failed) but were never
// notified, and skip still-running agents and already-notified ones. This is the
// safety net that recovers completions a full notification channel dropped.
func TestRequeueUnnotifiedCompletions(t *testing.T) {
	api := &StreamingAPI{bgAgentRegistry: NewBackgroundAgentRegistry()}
	const sid = "sess-1"

	mk := func(id string, status BackgroundAgentStatus, notified bool) {
		a := &BackgroundAgent{ID: id, SessionID: sid, Status: status}
		a.notified = notified
		api.bgAgentRegistry.Register(sid, a)
	}
	mk("done-unnotified", BGAgentCompleted, false) // → requeue
	mk("failed-unnotified", BGAgentFailed, false)  // → requeue
	mk("running", BGAgentRunning, false)           // → skip (not terminal)
	mk("done-notified", BGAgentCompleted, true)    // → skip (already notified)

	api.requeueUnnotifiedCompletions(sid)
	got := api.drainPendingCompletions(sid)
	sort.Strings(got)

	want := []string{"done-unnotified", "failed-unnotified"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("requeued %v, want exactly %v (running + already-notified must be skipped)", got, want)
	}

	// A second sweep with the same registry state still finds the same terminal
	// agents (they're still unnotified); dedup happens at the queue/notified layer,
	// so this must not panic or double-count within a single drain.
	api.requeueUnnotifiedCompletions(sid)
	again := api.drainPendingCompletions(sid)
	if len(again) != 2 {
		t.Fatalf("second sweep requeued %v, want 2 (idempotent per-drain)", again)
	}
}

func TestBackgroundAgentCompletionNotificationClaimIsAtomic(t *testing.T) {
	agent := &BackgroundAgent{}
	const contenders = 32
	var winners int32
	var wg sync.WaitGroup
	for i := 0; i < contenders; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if agent.beginCompletionNotification() {
				atomic.AddInt32(&winners, 1)
			}
		}()
	}
	wg.Wait()
	if got := atomic.LoadInt32(&winners); got != 1 {
		t.Fatalf("completion delivery claims = %d, want 1", got)
	}

	// A failed owner releases the claim for retry; a successful retry commits it.
	agent.finishCompletionNotification(false)
	if !agent.beginCompletionNotification() {
		t.Fatal("failed delivery did not release completion claim")
	}
	agent.finishCompletionNotification(true)
	if agent.beginCompletionNotification() {
		t.Fatal("delivered completion was claimable again")
	}
}

func TestRequeueSkipsCompletionCurrentlyBeingDelivered(t *testing.T) {
	api := &StreamingAPI{bgAgentRegistry: NewBackgroundAgentRegistry()}
	const sid = "sess-in-flight"
	agent := &BackgroundAgent{ID: "done", SessionID: sid, Status: BGAgentCompleted}
	if !agent.beginCompletionNotification() {
		t.Fatal("could not claim completion")
	}
	api.bgAgentRegistry.Register(sid, agent)

	api.requeueUnnotifiedCompletions(sid)
	if got := api.drainPendingCompletions(sid); len(got) != 0 {
		t.Fatalf("in-flight completion requeued: %v", got)
	}
	agent.finishCompletionNotification(false)
	api.requeueUnnotifiedCompletions(sid)
	if got := api.drainPendingCompletions(sid); len(got) != 1 || got[0] != "done" {
		t.Fatalf("released completion was not requeued: %v", got)
	}
}
