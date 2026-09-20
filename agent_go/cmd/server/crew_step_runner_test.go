package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	stepworkflow "github.com/manishiitg/coding-agent-loop/agent_go/pkg/orchestrator/agents/workflow/step_based_workflow"
)

const crewRunnerTriggers = `[
	{"id":"trig-1","name":"Review","enabled":true,"message":"Review it","kind":"internal","caller":{"type":"workflow","id":"wf-1"}},
	{"id":"trig-off","name":"Off","enabled":false,"message":"Off","kind":"internal","caller":{"type":"workflow","id":"wf-1"}}
]`

func testCrewStepRequest() stepworkflow.CrewStepRequest {
	return stepworkflow.CrewStepRequest{
		WorkflowID: "wf-1", WorkflowRunFolder: "iteration-0", StepID: "crew-1",
		Group: "production", ProfileID: "crewx", ProjectID: "rts", TriggerID: "trig-1",
		Instruction: "Review it", Inputs: map[string]interface{}{"pr": map[string]interface{}{"n": float64(87)}},
		TimeoutSeconds: 30,
	}
}

func crewRunnerDeliveryBase(req stepworkflow.CrewStepRequest) string {
	return "crew-step:" + req.WorkflowID + ":" + req.WorkflowRunFolder + ":" + req.StepID
}

func TestRunCrewStepAdoptsSuccessfulDuplicate(t *testing.T) {
	svc, _ := newInternalDispatchCrew(t, crewRunnerTriggers)
	convKey := "owner\x1fconversation:crewx:rts"
	svc.conversations = map[string]bool{convKey: true}
	ctx := context.Background()
	req := testCrewStepRequest()
	runID := webhookDeliveryRunID("rts", "trig-1", crewRunnerDeliveryBase(req))

	// A prior attempt already completed this delivery; the step must adopt
	// it without invoking the trigger again.
	first, err := svc.dispatchInternalProductTrigger(ctx, internalCrewTriggerCall{
		UserID: "owner", ProfileID: "crewx", ProjectID: "rts", TriggerID: "trig-1",
		Caller:     triggerCaller{Type: "workflow", ID: "wf-1"},
		DeliveryID: crewRunnerDeliveryBase(req), Payload: []byte(`{}`),
	})
	if err != nil || first.RunID != runID {
		t.Fatalf("seed dispatch = %+v err=%v", first, err)
	}
	runsWorkspace := agentProfileRuntimeWorkspace("owner", "_users/owner/Chats/Work/projects/rts")
	if err := UpdateScheduleRun(ctx, runsWorkspace, runID, "success", "", nil, "", "sess-9"); err != nil {
		t.Fatal(err)
	}
	if err := UpdateScheduleRunFinalResponse(ctx, runsWorkspace, runID, "the verdict"); err != nil {
		t.Fatal(err)
	}

	runner := newCrewStepRunner(svc, "owner")
	runner.pollInterval = 5 * time.Millisecond
	result, err := runner.RunCrewStep(ctx, req)
	if err != nil {
		t.Fatalf("RunCrewStep: %v", err)
	}
	if result.FinalResponse != "the verdict" || result.Status != "success" || result.SessionID != "sess-9" || result.CrewRunID != runID {
		t.Fatalf("result = %+v", result)
	}
	svc.mu.Lock()
	depth := len(svc.queued[convKey])
	svc.mu.Unlock()
	if depth != 1 {
		t.Fatalf("queued depth = %d, want 1 (no second dispatch)", depth)
	}
}

func TestRunCrewStepReinvokesPastFailedDuplicate(t *testing.T) {
	svc, files := newInternalDispatchCrew(t, crewRunnerTriggers)
	convKey := "owner\x1fconversation:crewx:rts"
	svc.conversations = map[string]bool{convKey: true}
	ctx := context.Background()
	req := testCrewStepRequest()
	base := crewRunnerDeliveryBase(req)
	runID := webhookDeliveryRunID("rts", "trig-1", base)

	first, err := svc.dispatchInternalProductTrigger(ctx, internalCrewTriggerCall{
		UserID: "owner", ProfileID: "crewx", ProjectID: "rts", TriggerID: "trig-1",
		Caller:     triggerCaller{Type: "workflow", ID: "wf-1"},
		DeliveryID: base, Payload: []byte(`{}`),
	})
	if err != nil || first.RunID != runID {
		t.Fatalf("seed dispatch = %+v err=%v", first, err)
	}
	runsWorkspace := agentProfileRuntimeWorkspace("owner", "_users/owner/Chats/Work/projects/rts")
	if err := UpdateScheduleRun(ctx, runsWorkspace, runID, "error", "crew exploded", nil, "", ""); err != nil {
		t.Fatal(err)
	}

	runner := newCrewStepRunner(svc, "owner")
	runner.pollInterval = 5 * time.Millisecond
	deadline, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
	defer cancel()
	_, err = runner.RunCrewStep(deadline, req)
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("re-invoked run err = %v, want timeout", err)
	}
	retryRunID := webhookDeliveryRunID("rts", "trig-1", base+"#retry-1")
	entry, err := FindScheduleRun(ctx, runsWorkspace, retryRunID)
	if err != nil || entry.Status != "queued" {
		t.Fatalf("retry run = %+v err=%v, want queued", entry, err)
	}
	payloadPath := "_users/owner/Chats/Work/projects/rts/triggers/deliveries/" + retryRunID + ".json"
	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(files[payloadPath]), &payload); err != nil {
		t.Fatalf("payload: %v", err)
	}
	if payload["source"] != "workflow" || payload["workflow_step_id"] != "crew-1" || payload["instruction"] != "Review it" {
		t.Fatalf("payload = %v", payload)
	}
	inputs, _ := payload["inputs"].(map[string]interface{})
	pr, _ := inputs["pr"].(map[string]interface{})
	if pr["n"] != float64(87) {
		t.Fatalf("payload inputs = %v", inputs)
	}
}

func TestRunCrewStepRevokedAccess(t *testing.T) {
	svc, _ := newInternalDispatchCrew(t, crewRunnerTriggers)
	svc.conversations = map[string]bool{"owner\x1fconversation:crewx:rts": true}
	ctx := context.Background()
	req := testCrewStepRequest()
	req.TriggerID = "trig-off"
	runner := newCrewStepRunner(svc, "owner")
	_, err := runner.RunCrewStep(ctx, req)
	if !errors.Is(err, ErrInternalTriggerDisabled) {
		t.Fatalf("disabled trigger err = %v, want ErrInternalTriggerDisabled", err)
	}
	runsWorkspace := agentProfileRuntimeWorkspace("owner", "_users/owner/Chats/Work/projects/rts")
	if _, err := FindScheduleRun(ctx, runsWorkspace, webhookDeliveryRunID("rts", "trig-off", crewRunnerDeliveryBase(req))); err == nil {
		t.Fatal("revoked access must not dispatch")
	}
}

func TestRunCrewStepCanceled(t *testing.T) {
	svc, _ := newInternalDispatchCrew(t, crewRunnerTriggers)
	svc.conversations = map[string]bool{"owner\x1fconversation:crewx:rts": true}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()
	runner := newCrewStepRunner(svc, "owner")
	runner.pollInterval = 5 * time.Millisecond
	_, err := runner.RunCrewStep(ctx, testCrewStepRequest())
	if err == nil || !strings.Contains(err.Error(), "canceled") {
		t.Fatalf("canceled run err = %v", err)
	}
}

func TestPollCrewStepReturnsPartialOnFailure(t *testing.T) {
	svc, _ := newInternalDispatchCrew(t, crewRunnerTriggers)
	svc.conversations = map[string]bool{"owner\x1fconversation:crewx:rts": true}
	ctx := context.Background()
	req := testCrewStepRequest()
	runID := webhookDeliveryRunID("rts", "trig-1", "poll-failure")
	if _, err := svc.dispatchInternalProductTrigger(ctx, internalCrewTriggerCall{
		UserID: "owner", ProfileID: "crewx", ProjectID: "rts", TriggerID: "trig-1",
		Caller:     triggerCaller{Type: "workflow", ID: "wf-1"},
		DeliveryID: "poll-failure", Payload: []byte(`{}`),
	}); err != nil {
		t.Fatal(err)
	}
	runsWorkspace := agentProfileRuntimeWorkspace("owner", "_users/owner/Chats/Work/projects/rts")
	if err := UpdateScheduleRun(ctx, runsWorkspace, runID, "error", "crew exploded", nil, "", "sess-9"); err != nil {
		t.Fatal(err)
	}
	runner := newCrewStepRunner(svc, "owner")
	runner.pollInterval = 5 * time.Millisecond
	result, err := runner.pollCrewStep(ctx, req, triggerCaller{Type: "workflow", ID: "wf-1"}, runID)
	if err == nil || !strings.Contains(err.Error(), "ended error") {
		t.Fatalf("failed poll err = %v", err)
	}
	if result.CrewRunID != runID || result.Status != "error" || result.Error != "crew exploded" || result.SessionID != "sess-9" {
		t.Fatalf("partial result = %+v", result)
	}
	if result.StartedAt.IsZero() {
		t.Fatal("partial result lost timestamps")
	}
}

func TestPollCrewStepTimeoutKeepsRunID(t *testing.T) {
	svc, _ := newInternalDispatchCrew(t, crewRunnerTriggers)
	svc.conversations = map[string]bool{"owner\x1fconversation:crewx:rts": true}
	ctx := context.Background()
	req := testCrewStepRequest()
	runID := webhookDeliveryRunID("rts", "trig-1", "poll-timeout")
	if _, err := svc.dispatchInternalProductTrigger(ctx, internalCrewTriggerCall{
		UserID: "owner", ProfileID: "crewx", ProjectID: "rts", TriggerID: "trig-1",
		Caller:     triggerCaller{Type: "workflow", ID: "wf-1"},
		DeliveryID: "poll-timeout", Payload: []byte(`{}`),
	}); err != nil {
		t.Fatal(err)
	}
	runner := newCrewStepRunner(svc, "owner")
	runner.pollInterval = 5 * time.Millisecond
	deadline, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()
	result, err := runner.pollCrewStep(deadline, req, triggerCaller{Type: "workflow", ID: "wf-1"}, runID)
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("timeout err = %v", err)
	}
	if result.CrewRunID != runID || result.Status != "queued" {
		t.Fatalf("timeout partial = %+v", result)
	}
}

func TestCheckInternalCrewAccess(t *testing.T) {
	svc, _ := newInternalDispatchCrew(t, crewRunnerTriggers)
	ctx := context.Background()
	caller := triggerCaller{Type: "workflow", ID: "wf-1"}
	if err := svc.checkInternalCrewAccess(ctx, "owner", "crewx", "rts", "trig-1", caller); err != nil {
		t.Fatalf("bound access rejected: %v", err)
	}
	if err := svc.checkInternalCrewAccess(ctx, "owner", "crewx", "rts", "trig-1", triggerCaller{Type: "workflow", ID: "intruder"}); !errors.Is(err, ErrInternalCallerMismatch) {
		t.Fatalf("wrong caller err = %v", err)
	}
	if err := svc.checkInternalCrewAccess(ctx, "owner", "crewx", "rts", "trig-off", caller); !errors.Is(err, ErrInternalTriggerDisabled) {
		t.Fatalf("disabled trigger err = %v", err)
	}
}

const testPreflightManifestAttached = `{"id":"wf-1","label":"WF1","crew_attachments":[{"id":"att-1","alias":"rts","crew_profile_id":"crewx","crew_project_id":"rts","crew_workspace_path":"_users/owner/Chats/Work/projects/rts"}]}`

func testPreflightWorkspace(t *testing.T, plan string, manifestJSON string) *ProductScheduleService {
	t.Helper()
	svc, files := newInternalTriggerTestCrew(t)
	svc.api = &StreamingAPI{}
	crewPath := "_users/owner/Chats/Work/projects/rts/product.json"
	crewProduct := `{"schema_version":1,"product":"crewx","id":"rts","title":"RTS","session_id":"sess-1","triggers":` + crewRunnerTriggers + `}`
	files[crewPath] = crewProduct
	mockFiles := map[string]string{crewPath: crewProduct}
	if plan != "" {
		mockFiles["Workflow/wf1/planning/plan.json"] = plan
	}
	if manifestJSON != "" {
		mockFiles["Workflow/wf1/workflow.json"] = manifestJSON
	}
	mock := &mockWorkspaceAPI{files: mockFiles}
	ws := httptest.NewServer(mock)
	t.Cleanup(ws.Close)
	t.Setenv("WORKSPACE_API_URL", ws.URL)
	return svc
}

func TestPreflightCrewSteps(t *testing.T) {
	ctx := context.Background()
	bound := `{"objective":"t","steps":[{"type":"crew","id":"crew-1","title":"Review","crew_profile_id":"work","crew_project_id":"rts","trigger_id":"trig-1","instruction":"x"}]}`
	// NOTE: the crew profile in the fixture registry is "crewx", so the
	// bound plan uses it; a wrong profile fails the lookup.
	bound = strings.Replace(bound, `"work"`, `"crewx"`, 1)
	if err := preflightCrewSteps(ctx, testPreflightWorkspace(t, bound, testPreflightManifestAttached), "owner", "Workflow/wf1"); err != nil {
		t.Fatalf("bound plan rejected: %v", err)
	}
	broken := strings.Replace(bound, "trig-1", "trig-off", 1)
	if err := preflightCrewSteps(ctx, testPreflightWorkspace(t, broken, testPreflightManifestAttached), "owner", "Workflow/wf1"); err == nil || !strings.Contains(err.Error(), "crew-1") {
		t.Fatalf("broken plan err = %v, want crew step failure", err)
	}
	unattached := testPreflightWorkspace(t, bound, `{"id":"wf-1","label":"WF1"}`)
	if err := preflightCrewSteps(ctx, unattached, "owner", "Workflow/wf1"); err == nil || !strings.Contains(err.Error(), "not attached") {
		t.Fatalf("unattached plan err = %v, want attachment failure", err)
	}
	plain := `{"objective":"t","steps":[{"type":"regular","id":"a","title":"A"}]}`
	if err := preflightCrewSteps(ctx, testPreflightWorkspace(t, plain, ""), "owner", "Workflow/wf1"); err != nil {
		t.Fatalf("crew-free plan rejected: %v", err)
	}
	if err := preflightCrewSteps(ctx, testPreflightWorkspace(t, "", ""), "owner", "Workflow/wf1"); err != nil {
		t.Fatalf("missing plan rejected: %v", err)
	}
	if err := preflightCrewSteps(ctx, nil, "owner", "Workflow/wf1"); err != nil {
		t.Fatalf("nil service rejected: %v", err)
	}
}

func TestAppendCrewPollTransition(t *testing.T) {
	var timeline []stepworkflow.CrewPollTransition
	timeline = appendCrewPollTransition(timeline, "queued")
	timeline = appendCrewPollTransition(timeline, "queued")
	timeline = appendCrewPollTransition(timeline, "running")
	timeline = appendCrewPollTransition(timeline, "  ")
	timeline = appendCrewPollTransition(timeline, "success")
	if len(timeline) != 3 || timeline[0].Status != "queued" || timeline[1].Status != "running" || timeline[2].Status != "success" {
		t.Fatalf("timeline = %+v", timeline)
	}
	for _, transition := range timeline {
		if transition.At.IsZero() {
			t.Fatalf("transition missing timestamp: %+v", transition)
		}
	}
}
