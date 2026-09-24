package server

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestCrewFunctionSchemaValidation(t *testing.T) {
	schema := map[string]interface{}{
		"type": "object", "required": []interface{}{"build", "env"},
		"properties": map[string]interface{}{
			"build":   map[string]interface{}{"type": "string"},
			"env":     map[string]interface{}{"type": "string", "enum": []interface{}{"staging", "prod"}},
			"retries": map[string]interface{}{"type": "integer"},
			"tags":    map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
		},
	}
	if err := checkCrewFunctionSchema(schema, "input_schema"); err != nil {
		t.Fatalf("valid schema rejected: %v", err)
	}
	for _, bad := range []map[string]interface{}{
		{"type": "date"},
		{"type": "object", "required": []interface{}{"missing"}, "properties": map[string]interface{}{}},
		{"type": "string", "enum": []interface{}{}},
		{"type": "array", "items": "string"},
	} {
		if err := checkCrewFunctionSchema(bad, "s"); err == nil {
			t.Fatalf("invalid schema accepted: %v", bad)
		}
	}
	if problems := validateCrewFunctionValue(schema, map[string]interface{}{"build": "812", "env": "staging", "retries": float64(2), "tags": []interface{}{"a"}}); len(problems) != 0 {
		t.Fatalf("valid value rejected: %v", problems)
	}
	problems := validateCrewFunctionValue(schema, map[string]interface{}{"build": float64(812), "env": "qa", "retries": 1.5, "tags": []interface{}{float64(1)}})
	joined := strings.Join(problems, " | ")
	for _, want := range []string{"$.build: expected string", "$.env: qa is not one of", "$.retries: expected integer", "$.tags[0]: expected string"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("problems %q missing %q", joined, want)
		}
	}
	if problems := validateCrewFunctionValue(schema, map[string]interface{}{"build": "812"}); len(problems) != 1 || !strings.Contains(problems[0], "$.env: required") {
		t.Fatalf("missing required = %v", problems)
	}
}

func TestCrewFunctionToolName(t *testing.T) {
	if got := crewFunctionToolName("RTS Flow Tester", "run_login_flow"); got != "rts_flow_tester__run_login_flow" {
		t.Fatalf("tool name = %q", got)
	}
	long := crewFunctionToolName(strings.Repeat("very long crew name ", 6), "run_login_flow")
	if len(long) > 64 || !strings.HasSuffix(long, "__run_login_flow") {
		t.Fatalf("long tool name = %q", long)
	}
}

type crewFunctionEnv struct {
	triggerLinkEnv
	alpha map[string]recordedTool
	beta  map[string]recordedTool
}

func newCrewFunctionEnv(t *testing.T) crewFunctionEnv {
	t.Helper()
	previousPoll, previousWait := triggerTargetPollInterval, crewFunctionFastWait
	triggerTargetPollInterval = 10 * time.Millisecond
	crewFunctionFastWait = 3 * time.Second
	t.Cleanup(func() { triggerTargetPollInterval, crewFunctionFastWait = previousPoll, previousWait })
	env := crewFunctionEnv{triggerLinkEnv: newTriggerLinkEnv(t)}
	env.alpha = env.functionTools(t, linkAlphaPath, "sess-caller", nil)
	env.beta = env.functionTools(t, linkBetaPath, "sess-beta-chat", nil)
	return env
}

func (env crewFunctionEnv) functionTools(t *testing.T, crewPath, sessionID string, contextPaths []string) map[string]recordedTool {
	t.Helper()
	reg := &recordingRegistrar{}
	if err := env.api.registerCrewFunctionTools(reg, "owner", sessionID, QueryRequest{SelectedFolder: crewPath, WorkflowContextPaths: contextPaths}, crewTriggerLinkCaller(crewPath), nil); err != nil {
		t.Fatal(err)
	}
	return reg.tools
}

var loginFlowArgs = map[string]interface{}{
	"target": "Beta", "name": "run_login_flow", "description": "Run the login flow against a build.",
	"instructions": "Run tests/login.py with the build and report the failing step.",
	"input_schema": map[string]interface{}{"type": "object", "required": []interface{}{"build"}, "properties": map[string]interface{}{
		"build": map[string]interface{}{"type": "string"}, "env": map[string]interface{}{"type": "string", "enum": []interface{}{"staging", "prod"}},
	}},
	"result_schema": map[string]interface{}{"type": "object", "required": []interface{}{"passed"}, "properties": map[string]interface{}{
		"passed": map[string]interface{}{"type": "boolean"}, "failed_step": map[string]interface{}{"type": "integer"},
	}},
}

// waitForCall returns the in-flight call of function from the registry.
func waitForCall(t *testing.T, function string) *crewFunctionCall {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		crewFunctionCalls.Lock()
		for _, call := range crewFunctionCalls.m {
			call.mu.Lock()
			match := call.Function == function && !call.terminalLocked()
			call.mu.Unlock()
			if match {
				crewFunctionCalls.Unlock()
				return call
			}
		}
		crewFunctionCalls.Unlock()
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("no in-flight call of %s", function)
	return nil
}

// Alpha declares a function on Beta, calls it, Beta reports progress and
// returns a valid result within the fast window: the caller's tool call
// returns the validated result directly.
func TestCrewFunctionFastPathReturnsValidatedResult(t *testing.T) {
	env := newCrewFunctionEnv(t)
	ctx := context.Background()
	defined, err := env.alpha["define_function"].exec(ctx, loginFlowArgs)
	if err != nil {
		t.Fatalf("define: %v", err)
	}
	if got := decodeToolJSON(t, defined); got["tool_name_for_callers"] != "beta__run_login_flow" {
		t.Fatalf("define = %v", got)
	}
	env.mock.mu.Lock()
	stored := env.mock.files[agentProfileRuntimeWorkspace("owner", linkBetaPath)+"/functions.json"]
	env.mock.mu.Unlock()
	if !strings.Contains(stored, `"run_login_flow"`) || !strings.Contains(stored, `"created_by": "crew:alpha (Alpha Bot)"`) {
		t.Fatalf("functions.json = %s", stored)
	}
	listed, err := env.alpha["list_functions"].exec(ctx, map[string]interface{}{"target": "#crew:Beta"})
	if err != nil || !strings.Contains(listed, "run_login_flow") {
		t.Fatalf("list = %s, %v", listed, err)
	}

	if _, err := env.alpha["call_function"].exec(ctx, map[string]interface{}{"target": "Beta", "function": "run_login_flow", "args": map[string]interface{}{"env": "qa"}}); err == nil || !strings.Contains(err.Error(), "$.build: required") {
		t.Fatalf("invalid args: err = %v", err)
	}

	go func() {
		call := waitForCall(t, "run_login_flow")
		if _, err := env.alpha["return_function_result"].exec(ctx, map[string]interface{}{"call_id": call.ID, "result": map[string]interface{}{"passed": true}}); err == nil {
			t.Error("the caller must not be able to return the target's result")
		}
		if _, err := env.beta["report_function_progress"].exec(ctx, map[string]interface{}{"call_id": call.ID, "message": "login page loaded", "percent": float64(40)}); err != nil {
			t.Errorf("progress: %v", err)
		}
		if _, err := env.beta["return_function_result"].exec(ctx, map[string]interface{}{"call_id": call.ID, "result": map[string]interface{}{"passed": "yes"}}); err == nil || !strings.Contains(err.Error(), "$.passed: expected boolean") {
			t.Errorf("invalid result: err = %v", err)
		}
		if _, err := env.beta["return_function_result"].exec(ctx, map[string]interface{}{"call_id": call.ID, "result": map[string]interface{}{"passed": false, "failed_step": float64(3)}}); err != nil {
			t.Errorf("valid result: %v", err)
		}
	}()
	out, err := env.alpha["call_function"].exec(ctx, map[string]interface{}{"target": "Beta", "function": "run_login_flow", "args": map[string]interface{}{"build": "812", "env": "staging"}})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	result := decodeToolJSON(t, out)
	if result["status"] != "completed" {
		t.Fatalf("call = %v", result)
	}
	if got, _ := json.Marshal(result["result"]); string(got) != `{"failed_step":3,"passed":false}` {
		t.Fatalf("result = %s", got)
	}
	progress, _ := result["progress"].([]interface{})
	if len(progress) != 1 || !strings.Contains(out, "login page loaded") {
		t.Fatalf("progress = %v", result["progress"])
	}
	// The delivery carried the call ID, the validated args and the contract.
	callID, _ := result["call_id"].(string)
	env.mock.mu.Lock()
	var delivery string
	for path, content := range env.mock.files {
		if strings.HasPrefix(path, linkBetaPath+"/triggers/deliveries/") && strings.Contains(content, callID) {
			delivery = content
		}
	}
	env.mock.mu.Unlock()
	for _, want := range []string{`"build":"812"`, "return_function_result", "report_function_progress"} {
		if !strings.Contains(delivery, want) {
			t.Fatalf("delivery missing %q: %s", want, delivery)
		}
	}
}

// A call slower than the fast window returns running and resumes the
// caller's chat with the result; get_function_call shows progress while it
// runs; a Crew target accepts update questions only once its run started.
func TestCrewFunctionSlowPathAutoNotifiesAndReportsProgress(t *testing.T) {
	env := newCrewFunctionEnv(t)
	crewFunctionFastWait = 20 * time.Millisecond
	ctx := context.Background()
	if _, err := env.alpha["define_function"].exec(ctx, loginFlowArgs); err != nil {
		t.Fatal(err)
	}
	out, err := env.alpha["call_function"].exec(ctx, map[string]interface{}{"target": "Beta", "function": "run_login_flow", "args": map[string]interface{}{"build": "900"}})
	if err != nil {
		t.Fatal(err)
	}
	running := decodeToolJSON(t, out)
	callID, _ := running["call_id"].(string)
	notify, ok := running["auto_notification"].(map[string]interface{})
	if running["status"] != "running" || callID == "" || !ok {
		t.Fatalf("slow call = %v", running)
	}
	executionID, _ := notify["execution_id"].(string)

	if _, err := env.alpha["ask_function_update"].exec(ctx, map[string]interface{}{"call_id": callID, "question": "how far along?"}); err == nil || !strings.Contains(err.Error(), "has not started yet") {
		t.Fatalf("update before start: err = %v", err)
	}
	if _, err := env.beta["report_function_progress"].exec(ctx, map[string]interface{}{"call_id": callID, "message": "step 2 of 5"}); err != nil {
		t.Fatal(err)
	}
	polled, err := env.alpha["get_function_call"].exec(ctx, map[string]interface{}{"call_id": callID})
	if err != nil || !strings.Contains(polled, "step 2 of 5") {
		t.Fatalf("poll = %s, %v", polled, err)
	}
	if _, err := env.beta["return_function_result"].exec(ctx, map[string]interface{}{"call_id": callID, "result": map[string]interface{}{"passed": true}}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		snapshot := env.api.bgAgentRegistry.Get("sess-caller", executionID).GetSnapshot()
		if snapshot.Status == BGAgentCompleted {
			if !strings.Contains(snapshot.Result, `"passed": true`) || !strings.Contains(snapshot.Result, "completed") {
				t.Fatalf("notification = %q", snapshot.Result)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("notification not delivered: %+v", snapshot)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// A Crew run that ends without return_function_result gets one retry turn;
// if that also ends without a result the call fails with the final answer.
func TestCrewFunctionRunWithoutResultRetriesThenFails(t *testing.T) {
	env := newCrewFunctionEnv(t)
	crewFunctionFastWait = 20 * time.Millisecond
	ctx := context.Background()
	if _, err := env.alpha["define_function"].exec(ctx, loginFlowArgs); err != nil {
		t.Fatal(err)
	}
	out, err := env.alpha["call_function"].exec(ctx, map[string]interface{}{"target": "Beta", "function": "run_login_flow", "args": map[string]interface{}{"build": "1"}, "notify": false})
	if err != nil {
		t.Fatal(err)
	}
	call := lookupCrewFunctionCall(decodeToolJSON(t, out)["call_id"].(string))
	runs := agentProfileRuntimeWorkspace("owner", linkBetaPath)
	finishRun := func(runID, answer string) {
		if err := UpdateScheduleRunFinalResponse(ctx, runs, runID, answer); err != nil {
			t.Fatal(err)
		}
		if err := UpdateScheduleRun(ctx, runs, runID, "success", "", nil, "", "sess-beta"); err != nil {
			t.Fatal(err)
		}
	}
	call.mu.Lock()
	first := call.RunID
	call.mu.Unlock()
	finishRun(first, "I ran it, all good.")
	deadline := time.Now().Add(5 * time.Second)
	var retry string
	for retry == "" {
		call.mu.Lock()
		if call.RunID != first {
			retry = call.RunID
		}
		call.mu.Unlock()
		if time.Now().After(deadline) {
			t.Fatal("no retry turn was dispatched")
		}
		time.Sleep(5 * time.Millisecond)
	}
	finishRun(retry, "Still no result, sorry.")
	select {
	case <-call.done:
	case <-time.After(5 * time.Second):
		t.Fatal("call did not fail after the retry")
	}
	snapshot := call.snapshot()
	if snapshot["status"] != "failed" || !strings.Contains(snapshot["error"].(string), "without returning a valid result") || !strings.Contains(snapshot["error"].(string), "Still no result") {
		t.Fatalf("snapshot = %v", snapshot)
	}
}

// Self-calls, cycles and deep chains are refused; mid-run questions are for
// Crew targets only; generated tools appear for tagged Crews.
func TestCrewFunctionGuardsAndGeneratedTools(t *testing.T) {
	env := newCrewFunctionEnv(t)
	ctx := context.Background()
	if _, err := env.alpha["define_function"].exec(ctx, loginFlowArgs); err != nil {
		t.Fatal(err)
	}
	own := map[string]interface{}{"name": "own_fn", "description": "d", "instructions": "i"}
	if _, err := env.alpha["define_function"].exec(ctx, own); err != nil {
		t.Fatalf("define own: %v", err)
	}
	if _, err := env.alpha["call_function"].exec(ctx, map[string]interface{}{"target": "Alpha", "function": "own_fn"}); err == nil || !strings.Contains(err.Error(), "cannot call itself") && !strings.Contains(err.Error(), "own function") {
		t.Fatalf("self call: err = %v", err)
	}

	// Alpha is currently running a call that Beta made: Alpha calling Beta
	// back would loop.
	inflight := &crewFunctionCall{ID: "fn-inflight", Function: "x", TargetKind: triggerCallerCrew, TargetID: "alpha", CallerKind: triggerCallerCrew, CallerID: "beta",
		Chain: []string{"crew:beta", "crew:alpha"}, Root: "fn-inflight", Status: "running", done: make(chan struct{})}
	crewFunctionCalls.Lock()
	crewFunctionCalls.m[inflight.ID] = inflight
	crewFunctionCalls.Unlock()
	t.Cleanup(func() {
		crewFunctionCalls.Lock()
		delete(crewFunctionCalls.m, inflight.ID)
		crewFunctionCalls.Unlock()
	})
	if _, err := env.alpha["call_function"].exec(ctx, map[string]interface{}{"target": "Beta", "function": "run_login_flow", "args": map[string]interface{}{"build": "1"}}); err == nil || !strings.Contains(err.Error(), "already in this call chain") {
		t.Fatalf("cycle: err = %v", err)
	}
	inflight.mu.Lock()
	inflight.Chain = []string{"crew:x1", "crew:x2", "crew:x3", "crew:alpha"}
	inflight.mu.Unlock()
	if _, err := env.alpha["call_function"].exec(ctx, map[string]interface{}{"target": "Beta", "function": "run_login_flow", "args": map[string]interface{}{"build": "1"}}); err == nil || !strings.Contains(err.Error(), "depth limit") {
		t.Fatalf("depth: err = %v", err)
	}
	inflight.finish("completed", nil, "")

	// Workflow targets take no mid-run questions.
	wfCall := &crewFunctionCall{ID: "fn-wf", Function: "report", TargetKind: triggerCallerWorkflow, TargetID: "wf", CallerKind: triggerCallerCrew, CallerID: "alpha", Status: "running", done: make(chan struct{})}
	crewFunctionCalls.Lock()
	crewFunctionCalls.m[wfCall.ID] = wfCall
	crewFunctionCalls.Unlock()
	if _, err := env.alpha["ask_function_update"].exec(ctx, map[string]interface{}{"call_id": "fn-wf", "question": "status?"}); err == nil || !strings.Contains(err.Error(), "workflow runs do not accept") {
		t.Fatalf("workflow update: err = %v", err)
	}
	if _, err := env.beta["ask_function_update"].exec(ctx, map[string]interface{}{"call_id": "fn-wf", "question": "status?"}); err == nil || !strings.Contains(err.Error(), "only the caller") {
		t.Fatalf("non-caller update: err = %v", err)
	}

	tagged := env.functionTools(t, linkAlphaPath, "sess-caller-2", []string{linkBetaPath})
	generated, ok := tagged["beta__run_login_flow"]
	if !ok {
		t.Fatalf("generated tool missing: %v", crewFunctionToolKeys(tagged))
	}
	if _, err := generated.exec(ctx, map[string]interface{}{"env": "prod"}); err == nil || !strings.Contains(err.Error(), "$.build: required") {
		t.Fatalf("generated tool validation: err = %v", err)
	}
}

func crewFunctionToolKeys(tools map[string]recordedTool) []string {
	out := make([]string, 0, len(tools))
	for name := range tools {
		out = append(out, name)
	}
	return out
}
