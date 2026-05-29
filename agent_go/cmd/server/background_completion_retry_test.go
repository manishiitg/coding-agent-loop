package server

import (
	"sort"
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
	mk("done-unnotified", BGAgentCompleted, false)   // → requeue
	mk("failed-unnotified", BGAgentFailed, false)     // → requeue
	mk("running", BGAgentRunning, false)              // → skip (not terminal)
	mk("done-notified", BGAgentCompleted, true)       // → skip (already notified)

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
