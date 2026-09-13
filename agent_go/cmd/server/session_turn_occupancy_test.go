package server

import (
	"os"
	"strings"
	"testing"
)

// PLAT-113 step 2. executeSyntheticTurn must acquire the input lane BEFORE it
// registers the execution. Registering first meant a synthetic turn parked on
// the lane counted in the parent's running_children — so the parent judged its
// own liveness by counting children that were blocked on the parent, and the
// idle-wait watchdog killed runs that were working.
//
// This is a source-order assertion rather than a runtime one: reaching that code
// requires a fully constructed LLMAgentWrapper for the session, so a unit test
// cannot drive the two calls without reimplementing the turn. The ordering is
// the invariant, so the ordering is what is pinned.
func TestSyntheticTurnRegistersOnlyAfterAcquiringTheLane(t *testing.T) {
	source, err := os.ReadFile("background_agents.go")
	if err != nil {
		t.Fatal(err)
	}
	body := string(source)

	start := strings.Index(body, "func (api *StreamingAPI) executeSyntheticTurnWithOutcome(")
	if start < 0 {
		t.Fatal("executeSyntheticTurnWithOutcome not found; update this test with the new name")
	}
	fn := body[start:]
	if end := strings.Index(fn, "\nfunc "); end > 0 {
		fn = fn[:end]
	}

	lockAt := strings.Index(fn, "lockSessionInputLane(")
	trackAt := strings.Index(fn, "trackSyntheticConversationTurnStart(")
	if lockAt < 0 || trackAt < 0 {
		t.Fatalf("expected both calls in executeSyntheticTurn: lock=%d track=%d", lockAt, trackAt)
	}
	if trackAt < lockAt {
		t.Fatal("executeSyntheticTurn registers the execution before acquiring the input lane; " +
			"a turn blocked on the lane would again be counted as a running child of the turn blocking it")
	}
}

// P0 regression: a retained coding-CLI turn is not guaranteed to own the HTTP
// cancel handle, input lane, or terminal-store busy signal. The durable agent
// session is therefore an authoritative occupancy source and must be checked
// before the 15-second display-busy recovery is allowed to clear anything.
func TestP0AutoNotificationChecksDurableAgentTurnBeforeClearingStaleBusy(t *testing.T) {
	source, err := os.ReadFile("background_agents.go")
	if err != nil {
		t.Fatal(err)
	}
	body := string(source)
	start := strings.Index(body, "func (api *StreamingAPI) isSessionBusyForAutoNotification(")
	if start < 0 {
		t.Fatal("isSessionBusyForAutoNotification not found")
	}
	fn := body[start:]
	if end := strings.Index(fn, "\nfunc "); end > 0 {
		fn = fn[:end]
	}
	activeAt := strings.Index(fn, "storedAgentTurnInProgress(sessionID)")
	clearAt := strings.Index(fn, "clearStaleBusyIfNeeded(sessionID)")
	if activeAt < 0 || clearAt < 0 || activeAt > clearAt {
		t.Fatalf("durable agent occupancy must be checked before stale clear: active=%d clear=%d", activeAt, clearAt)
	}
}

func TestP0CompletionDeliveryCommitsOnlyAfterAsynchronousOutcome(t *testing.T) {
	source, err := os.ReadFile("background_agents.go")
	if err != nil {
		t.Fatal(err)
	}
	body := string(source)
	start := strings.Index(body, "func (api *StreamingAPI) processBackgroundAgentCompletion(")
	if start < 0 {
		t.Fatal("processBackgroundAgentCompletion not found")
	}
	fn := body[start:]
	if end := strings.Index(fn, "\nfunc "); end > 0 {
		fn = fn[:end]
	}
	for _, required := range []string{
		"executeSyntheticTurnWithOutcome(",
		"delivered := turnErr == nil",
		"agent.finishCompletionNotification(delivered)",
		"api.queuePendingCompletion(sessionID, agentID)",
	} {
		if !strings.Contains(fn, required) {
			t.Fatalf("completion path no longer waits/retries on terminal outcome; missing %q", required)
		}
	}
	if strings.Contains(fn, "finishCompletionNotification(dispatched)") {
		t.Fatal("completion is again being marked delivered when the stream merely opens")
	}
}

// PLAT-113. Auto-notifications are supposed to queue while a turn is running and
// be delivered afterwards in one batch. The gate that decides this read
// sessionBusy — a user-facing display flag that is deliberately never set for
// workflow turns — so during a scheduled workflow run every background-agent
// completion skipped the queue, called executeSyntheticTurn, registered itself
// as a running execution, and then blocked on the input lane the workflow turn
// was holding. 25 of them piled up behind one 5-hour turn until the idle-wait
// watchdog killed a run that was working correctly.
//
// The lane is the authority: a turn occupies the session exactly when it holds
// that mutex.

func TestWorkflowTurnQueuesAutoNotificationsEvenWithoutTheBusyFlag(t *testing.T) {
	api := &StreamingAPI{}
	const sessionID = "schedule-cron--5227790a_workflow"

	// A workflow turn never sets sessionBusy — this is the exact state the old
	// gate misread as "idle".
	if api.isSessionBusy(sessionID) {
		t.Fatal("precondition: a workflow turn must not set the display busy flag")
	}

	release := api.lockSessionInputLane(sessionID)
	defer release()

	if !api.sessionTurnInProgress(sessionID) {
		t.Fatal("a turn holding the lane must report the session as occupied")
	}
	if !api.isSessionBusyForAutoNotification(sessionID) {
		t.Fatal("auto-notification must queue while a workflow turn holds the lane; " +
			"executing instead is what blocked 25 synthetic turns behind one turn")
	}
}

// The old gate is still the reason this used to fail: sessionBusy alone reports
// idle for the same session and state.
func TestDisplayBusyFlagAloneMisreadsAWorkflowTurn(t *testing.T) {
	api := &StreamingAPI{}
	const sessionID = "schedule-cron--5227790a_workflow"

	release := api.lockSessionInputLane(sessionID)
	defer release()

	if api.isSessionBusy(sessionID) {
		t.Fatal("sessionBusy is expected to be false here; if this changes, the " +
			"lane check is no longer the thing carrying this case")
	}
	if !api.sessionTurnInProgress(sessionID) {
		t.Fatal("the lane must still report occupancy when the display flag does not")
	}
}

func TestSessionIsFreeOnceTheTurnReleasesTheLane(t *testing.T) {
	api := &StreamingAPI{}
	const sessionID = "session-released"

	release := api.lockSessionInputLane(sessionID)
	if !api.sessionTurnInProgress(sessionID) {
		t.Fatal("occupied while held")
	}
	release()

	if api.sessionTurnInProgress(sessionID) {
		t.Fatal("still occupied after release; queued notifications would never drain")
	}
	if api.isSessionBusyForAutoNotification(sessionID) {
		t.Fatal("an idle session must let an auto-notification execute rather than queue forever")
	}

	// Releasing twice must stay safe — the release closure is once-guarded.
	release()
	if api.sessionTurnInProgress(sessionID) {
		t.Fatal("double release corrupted the lane refcount")
	}
}

func TestUnknownSessionIsNotOccupied(t *testing.T) {
	api := &StreamingAPI{}
	if api.sessionTurnInProgress("never-seen") {
		t.Fatal("a session with no lane must not report occupancy")
	}
	if api.sessionTurnInProgress("") {
		t.Fatal("an empty session id must not report occupancy")
	}
	var nilAPI *StreamingAPI
	if nilAPI.sessionTurnInProgress("x") {
		t.Fatal("nil receiver must be safe")
	}
}
