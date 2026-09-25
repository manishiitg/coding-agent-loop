package server

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

// startLoginFlowCall starts Alpha's call of Beta.run_login_flow with a short
// timeout, bypassing call_function's one-minute minimum.
func startLoginFlowCall(t *testing.T, env crewFunctionEnv, timeout time.Duration) *crewFunctionCall {
	t.Helper()
	ctx := context.WithValue(context.Background(), UserContextKey, &UserClaims{UserID: "owner"})
	if _, err := env.alpha["define_function"].exec(ctx, loginFlowArgs); err != nil {
		t.Fatal(err)
	}
	target, err := resolveTriggerTarget(ctx, &UserClaims{UserID: "owner"}, "Beta")
	if err != nil {
		t.Fatal(err)
	}
	caller, err := crewTriggerLinkCaller(linkAlphaPath)(ctx)
	if err != nil {
		t.Fatal(err)
	}
	functions, err := callableFunctions(ctx, target)
	if err != nil {
		t.Fatal(err)
	}
	fn, ok := findCrewFunction(functions, "run_login_flow")
	if !ok {
		t.Fatal("run_login_flow not declared")
	}
	call, err := env.api.startCrewFunctionCall(ctx, "owner", caller, target, fn, map[string]interface{}{"build": "7"}, timeout)
	if err != nil {
		t.Fatal(err)
	}
	return call
}

func callClosed(call *crewFunctionCall) bool {
	select {
	case <-call.done:
		return true
	default:
		return false
	}
}

// Progress reports keep a call alive past its timeout; once the target goes
// quiet the caller stops waiting, and the target's later answer is still
// accepted and delivered to the caller's chat.
func TestCrewFunctionTimeoutFollowsActivityAndAcceptsLateAnswer(t *testing.T) {
	env := newCrewFunctionEnv(t)
	ctx := context.Background()
	timeout := 300 * time.Millisecond
	call := startLoginFlowCall(t, env, timeout)
	executionID, err := env.api.startCrewFunctionWatch(QueryRequest{SelectedFolder: linkAlphaPath}, "sess-caller", "owner", call, timeout)
	if err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 8; i++ {
		if _, err := env.beta["report_function_progress"].exec(ctx, map[string]interface{}{"call_id": call.ID, "message": "still testing"}); err != nil {
			t.Fatalf("progress %d: %v", i, err)
		}
		time.Sleep(100 * time.Millisecond)
	}
	if callClosed(call) {
		t.Fatalf("a call reporting progress timed out after %s: %v", timeout, call.snapshot())
	}

	select {
	case <-call.done:
	case <-time.After(3 * time.Second):
		t.Fatal("a quiet call never timed out")
	}
	timedOut := call.snapshot()
	if timedOut["status"] != "failed" || timedOut["timed_out"] != true || !strings.Contains(timedOut["error"].(string), "late answer") {
		t.Fatalf("timed-out call = %v", timedOut)
	}
	waitForNotification(t, env, executionID, "late answer is sent to you automatically")

	if _, err := env.beta["report_function_progress"].exec(ctx, map[string]interface{}{"call_id": call.ID, "message": "almost done"}); err != nil {
		t.Fatalf("progress after the timeout was refused: %v", err)
	}
	if _, err := env.beta["return_function_result"].exec(ctx, map[string]interface{}{"call_id": call.ID, "result": map[string]interface{}{"passed": true}}); err != nil {
		t.Fatalf("late result was refused: %v", err)
	}
	late := call.snapshot()
	if late["status"] != "completed" || late["late"] != true {
		t.Fatalf("late call = %v", late)
	}
	if _, err := env.beta["return_function_result"].exec(ctx, map[string]interface{}{"call_id": call.ID, "result": map[string]interface{}{"passed": false}}); err == nil {
		t.Fatal("a second late result was accepted")
	}

	deadline := time.Now().Add(3 * time.Second)
	for {
		var delivered string
		for _, agent := range env.api.bgAgentRegistry.GetAll("sess-caller") {
			snapshot := agent.GetSnapshot()
			if agent.ID != executionID && snapshot.Status == BGAgentCompleted {
				delivered = snapshot.Result
			}
		}
		if delivered != "" {
			if !strings.Contains(delivered, "late answer") || !strings.Contains(delivered, `"passed": true`) {
				t.Fatalf("late notification = %q", delivered)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the late answer never reached the caller's chat")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func waitForNotification(t *testing.T, env crewFunctionEnv, executionID, want string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		snapshot := env.api.bgAgentRegistry.Get("sess-caller", executionID).GetSnapshot()
		if snapshot.Status != BGAgentRunning {
			if text := snapshot.Result + snapshot.Error; !strings.Contains(text, want) {
				t.Fatalf("notification = %+v, want %q", snapshot, want)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("notification not delivered: %+v", snapshot)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// After a restart a call is found from its saved record; one that was still
// open is settled as interrupted with its progress kept.
func TestCrewFunctionCallSurvivesRestartAsInterrupted(t *testing.T) {
	env := newCrewFunctionEnv(t)
	call := startLoginFlowCall(t, env, time.Minute)
	if _, err := env.beta["report_function_progress"].exec(context.Background(), map[string]interface{}{"call_id": call.ID, "message": "halfway"}); err != nil {
		t.Fatal(err)
	}
	// Simulate the restart: the process forgets the call and its supervisor.
	call.finish("failed", nil, "test: stop supervisor")
	record := strings.TrimSuffix(call.TargetPath, "/") + "/functions/calls/" + call.ID + ".json"
	env.mock.mu.Lock()
	var saved map[string]interface{}
	_ = json.Unmarshal([]byte(env.mock.files[record]), &saved)
	saved["status"], saved["error"] = "running", ""
	encoded, _ := json.Marshal(saved)
	env.mock.files[record] = string(encoded)
	env.mock.mu.Unlock()
	crewFunctionCalls.Lock()
	delete(crewFunctionCalls.m, call.ID)
	crewFunctionCalls.Unlock()

	polled, err := env.alpha["get_function_call"].exec(context.Background(), map[string]interface{}{"call_id": call.ID})
	if err != nil {
		t.Fatalf("lookup after restart: %v", err)
	}
	snapshot := decodeToolJSON(t, polled)
	if snapshot["status"] != "failed" || !strings.Contains(snapshot["error"].(string), "server restarted") || !strings.Contains(polled, "halfway") {
		t.Fatalf("after restart = %s", polled)
	}
	env.mock.mu.Lock()
	persisted := env.mock.files[record]
	env.mock.mu.Unlock()
	if !strings.Contains(persisted, "server restarted") {
		t.Fatalf("interrupted status not saved: %s", persisted)
	}
	if lookupCrewFunctionCall("fn-does-not-exist") != nil || lookupCrewFunctionCall("fn-../../etc") != nil {
		t.Fatal("unknown or unsafe IDs must not resolve")
	}
}

// A workflow ask that outlives its timeout releases the caller but keeps the
// assistant's turn running; its reply arrives as a late answer.
func TestWorkflowAskTimeoutKeepsTurnAndDeliversLateAnswer(t *testing.T) {
	env := newCrewFunctionEnv(t)
	release := make(chan struct{})
	turnCtxErr := make(chan error, 1)
	previous := workflowAskTurn
	workflowAskTurn = func(_ *StreamingAPI, ctx context.Context, _ map[string]interface{}, _, _ string) (internalSessionTurnResult, error) {
		<-release
		turnCtxErr <- ctx.Err()
		return internalSessionTurnResult{FinalResponse: "Ran review_pr on #149: passed."}, nil
	}
	t.Cleanup(func() { workflowAskTurn = previous })

	ctx := context.WithValue(context.Background(), UserContextKey, &UserClaims{UserID: "owner"})
	target, err := resolveTriggerTarget(ctx, &UserClaims{UserID: "owner"}, "#workflow:Reports")
	if err != nil {
		t.Fatal(err)
	}
	caller, err := crewTriggerLinkCaller(linkAlphaPath)(ctx)
	if err != nil {
		t.Fatal(err)
	}
	call, err := env.api.startCrewFunctionCall(ctx, "owner", caller, target, workflowAskFunction(), map[string]interface{}{"message": "review PR 149"}, 200*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-call.done:
	case <-time.After(3 * time.Second):
		t.Fatal("a quiet workflow ask never timed out")
	}
	if snapshot := call.snapshot(); snapshot["timed_out"] != true {
		t.Fatalf("timed-out ask = %v", snapshot)
	}
	close(release)
	if err := <-turnCtxErr; err != nil {
		t.Fatalf("the assistant's turn was cancelled by the caller's timeout: %v", err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		snapshot := call.snapshot()
		if snapshot["late"] == true {
			if snapshot["status"] != "completed" || !strings.Contains(fmt.Sprint(snapshot["result"]), "passed") {
				t.Fatalf("late ask = %v", snapshot)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("late answer not recorded: %v", snapshot)
		}
		time.Sleep(5 * time.Millisecond)
	}
}
