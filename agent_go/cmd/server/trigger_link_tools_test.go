package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/agentprofiles"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/schedulerstate"
)

const (
	linkAlphaPath = "_users/owner/Chats/Work/projects/alpha"
	linkBetaPath  = "_users/owner/Chats/Work/projects/beta"
	linkGammaPath = "_users/other/Chats/Work/projects/gamma"
)

type triggerLinkEnv struct {
	api  *StreamingAPI
	svc  *ProductScheduleService
	mock *mockWorkspaceAPI
}

func newTriggerLinkEnv(t *testing.T) triggerLinkEnv {
	t.Helper()
	t.Setenv("MULTI_USER_MODE", "true")
	withMemoryUserDirectory(t, `{"users":[{"id":"owner","username":"owner","can_create":true},{"id":"other","username":"other","can_create":true}]}`)

	registry := agentprofiles.NewRegistry()
	if err := registry.RegisterProfile(agentprofiles.Profile{
		ID: "work", Name: "Work", Version: 1, SystemPromptTemplate: "hi", BuiltIn: true, Product: "work",
		Runtime: agentprofiles.RuntimePolicy{
			Transport:    "auto",
			Conversation: agentprofiles.ConversationPolicy{Mode: agentprofiles.ConversationModeKeyed, KeyType: agentprofiles.ConversationKeyTypeProject},
			Workspace:    agentprofiles.WorkspacePolicy{Mode: agentprofiles.WorkspaceModeProject, Root: "Chats", ProjectsRoot: "Chats/Work/projects"},
		},
		Features: []agentprofiles.FeatureBinding{{ID: "triggers"}},
		UIPanels: agentprofiles.UIPanels{Schedules: true},
	}); err != nil {
		t.Fatal(err)
	}

	workflow := func(label string, access *WorkflowAccess) string {
		manifest := NewWorkflowManifest(label)
		manifest.CreatedBy = access.Owners[0]
		manifest.Access = access
		raw, err := json.Marshal(manifest)
		if err != nil {
			t.Fatal(err)
		}
		return string(raw)
	}
	mock := &mockWorkspaceAPI{files: map[string]string{
		linkAlphaPath + "/product.json": `{"schema_version":1,"product":"work","id":"alpha","title":"Alpha","identity":{"name":"Alpha Bot"},"session_id":"sess-alpha","triggers":[]}`,
		linkBetaPath + "/product.json":  `{"schema_version":1,"product":"work","id":"beta","title":"Beta","session_id":"sess-beta","triggers":[]}`,
		linkGammaPath + "/product.json": `{"schema_version":1,"product":"work","id":"gamma","title":"Gamma","session_id":"sess-gamma","triggers":[]}`,

		manifestPath("Workflow/reports"):             workflow("Reports", &WorkflowAccess{Owners: []string{"owner"}}),
		"Workflow/reports/planning/plan.json":        `{"steps":[{"type":"regular","id":"work","title":"Work","description":"Work"}]}`,
		"Workflow/reports/variables/variables.json":  `{"variables":[],"groups":[{"name":"default"}]}`,
		manifestPath("Workflow/pipeline"):            workflow("Pipeline", &WorkflowAccess{Owners: []string{"owner"}}),
		"Workflow/pipeline/planning/plan.json":       `{"steps":[{"type":"regular","id":"work","title":"Work","description":"Work"}]}`,
		"Workflow/pipeline/variables/variables.json": `{"variables":[],"groups":[{"name":"default"}]}`,
		manifestPath("Workflow/shared"):              workflow("Shared", &WorkflowAccess{Owners: []string{"other"}, Readers: []string{"owner"}}),
	}}
	ws := httptest.NewServer(mock)
	t.Cleanup(ws.Close)
	t.Setenv("WORKSPACE_API_URL", ws.URL)

	svc := NewProductScheduleService(nil, registry)
	svc.readFile = func(_ context.Context, path string) (string, bool, error) {
		mock.mu.Lock()
		defer mock.mu.Unlock()
		content, ok := mock.files[path]
		return content, ok, nil
	}
	svc.writeFile = func(_ context.Context, path, content string) error {
		mock.mu.Lock()
		defer mock.mu.Unlock()
		mock.files[path] = content
		return nil
	}
	// Mark the completion loops started: the test reads the watch's terminal
	// state from the registry instead of resuming a live chat.
	api := &StreamingAPI{productSchedules: svc, bgAgentRegistry: NewBackgroundAgentRegistry(),
		completionLoopStarted: map[string]bool{"sess-caller": true, "sess-builder": true}}
	api.scheduler = NewSchedulerService(api)
	svc.api = api
	// Hold both Crew conversations so deliveries queue instead of starting
	// live agent turns: the tools' claim, binding and poll wiring is under test.
	svc.conversations = map[string]bool{"owner\x1fconversation:work:beta": true, "owner\x1fconversation:work:alpha": true}
	return triggerLinkEnv{api: api, svc: svc, mock: mock}
}

func (env triggerLinkEnv) crewTools(t *testing.T, crewPath string) map[string]recordedTool {
	t.Helper()
	reg := &recordingRegistrar{}
	if err := env.api.registerTriggerLinkTools(reg, "owner", "sess-caller", QueryRequest{SelectedFolder: crewPath}, crewTriggerLinkCaller(crewPath)); err != nil {
		t.Fatal(err)
	}
	return reg.tools
}

func (env triggerLinkEnv) workflowTools(t *testing.T, workflowPath string) map[string]recordedTool {
	t.Helper()
	reg := &recordingRegistrar{}
	if err := env.api.registerTriggerLinkTools(reg, "owner", "sess-builder", QueryRequest{SelectedFolder: workflowPath}, workflowTriggerLinkCaller(workflowPath)); err != nil {
		t.Fatal(err)
	}
	return reg.tools
}

func decodeToolJSON(t *testing.T, out string) map[string]interface{} {
	t.Helper()
	var decoded map[string]interface{}
	if err := json.Unmarshal([]byte(out), &decoded); err != nil {
		t.Fatalf("tool output is not JSON: %v\n%s", err, out)
	}
	return decoded
}

// Crew A connects to Crew B by the #crew tag: the created trigger is an
// ordinary internal trigger in B's own list, named for and bound to A, and a
// second connect reuses it.
func TestConnectCrewToCrewCreatesVisibleBoundTrigger(t *testing.T) {
	env := newTriggerLinkEnv(t)
	tools := env.crewTools(t, linkAlphaPath)
	ctx := context.Background()

	out, err := tools["connect_to_target"].exec(ctx, map[string]interface{}{"target": "#crew:Beta"})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	first := decodeToolJSON(t, out)
	if first["created"] != true || first["trigger_id"] == "" {
		t.Fatalf("first connect = %v", first)
	}
	triggers, err := env.svc.projectWebhookConfigs(ctx, "owner", "work", "beta")
	if err != nil || len(triggers) != 1 {
		t.Fatalf("beta triggers = %+v err=%v", triggers, err)
	}
	created := triggers[0]
	if !created.IsInternal() || !created.Enabled || created.Caller == nil || created.Caller.Type != triggerCallerCrew || created.Caller.ID != "alpha" {
		t.Fatalf("created trigger = %+v, want an enabled internal trigger bound to crew alpha", created)
	}
	if created.Name != "Called by Alpha Bot" || !strings.Contains(created.Message, "Alpha Bot") {
		t.Fatalf("trigger does not record its creator: name=%q message=%q", created.Name, created.Message)
	}
	if created.Webhook != nil {
		t.Fatalf("internal trigger carries secret material: %+v", created.Webhook)
	}

	again, err := tools["connect_to_target"].exec(ctx, map[string]interface{}{"target": "Beta"})
	if err != nil {
		t.Fatalf("second connect: %v", err)
	}
	if second := decodeToolJSON(t, again); second["created"] != false || second["trigger_id"] != first["trigger_id"] {
		t.Fatalf("second connect = %v, want reuse of %v", second, first["trigger_id"])
	}

	custom, err := tools["connect_to_target"].exec(ctx, map[string]interface{}{"target": "crew:beta", "name": "Nightly QA", "instructions": "Run the QA checklist."})
	if err != nil {
		t.Fatalf("custom connect: %v", err)
	}
	if decodeToolJSON(t, custom)["created"] != true {
		t.Fatalf("custom trigger not created: %s", custom)
	}
	triggers, _ = env.svc.projectWebhookConfigs(ctx, "owner", "work", "beta")
	if len(triggers) != 2 || triggers[1].Name != "Nightly QA" || !strings.HasPrefix(triggers[1].Message, "Run the QA checklist.") {
		t.Fatalf("custom trigger = %+v", triggers)
	}
}

func TestTriggerLinkPermissions(t *testing.T) {
	env := newTriggerLinkEnv(t)
	tools := env.crewTools(t, linkAlphaPath)
	ctx := context.Background()
	for _, tt := range []struct {
		name, target, want string
	}{
		{"another user's crew", "Gamma", "no Crew you own"},
		{"another user's crew by physical path", linkGammaPath, "no Crew you own"},
		{"read-only workflow", "workflow:Shared", "only have read access"},
		{"itself", "Alpha Bot", "cannot call itself"},
		{"unknown", "nope", "no Crew you own"},
	} {
		_, err := tools["connect_to_target"].exec(ctx, map[string]interface{}{"target": tt.target})
		if err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Fatalf("%s: err = %v, want %q", tt.name, err, tt.want)
		}
	}
	if triggers, _ := env.svc.projectWebhookConfigs(ctx, "other", "work", "gamma"); len(triggers) != 0 {
		t.Fatalf("a rejected connect wrote triggers on another user's crew: %+v", triggers)
	}
	// A crew caller on a crew trigger must be the requester's own crew.
	if _, _, err := env.svc.saveProductWebhookConfig(ctx, "owner", productWebhookRequest{
		ProfileID: "work", ProjectID: "beta", Name: "Squat", Message: "x", Enabled: true,
		Kind: triggerKindInternal, Caller: &triggerCaller{Type: triggerCallerCrew, ID: "gamma", ProfileID: "work"},
	}, ""); err == nil {
		t.Fatal("binding another user's crew as caller must fail")
	}
}

// The Work product gate admits the trigger-link tools through the
// workflow-references feature (readers are excluded at registration).
func TestTriggerLinkToolsDeclaredByFeature(t *testing.T) {
	names := []string{"connect_to_target", "call_target", "get_target_run", "send_to_target_run"}
	profile := agentprofiles.Profile{ID: "p", Features: []agentprofiles.FeatureBinding{{ID: "workflow-references"}}}
	if err := agentprofiles.ResolveFeatures(&profile); err != nil {
		t.Fatal(err)
	}
	var features []string
	for _, feature := range profile.ResolvedFeatures {
		features = append(features, feature.Tools...)
	}
	for _, name := range names {
		found := false
		for _, declared := range features {
			found = found || declared == name
		}
		if !found {
			t.Fatalf("%s is not declared by the workflow-references feature: %v", name, features)
		}
	}
}

// Crew A calls Crew B: the run ID returns at once, polling reads B's run,
// and when B finishes the caller's chat gets the auto-notification with B's
// final answer. A mid-run message is refused until the run is running.
func TestCallCrewTargetPollsAndAutoNotifies(t *testing.T) {
	previous := triggerTargetPollInterval
	triggerTargetPollInterval = 10 * time.Millisecond
	t.Cleanup(func() { triggerTargetPollInterval = previous })
	env := newTriggerLinkEnv(t)
	tools := env.crewTools(t, linkAlphaPath)
	ctx := context.Background()

	out, err := tools["call_target"].exec(ctx, map[string]interface{}{"target": "#crew:Beta", "task": "Summarise the open bugs.", "payload": map[string]interface{}{"repo": "rts"}})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	called := decodeToolJSON(t, out)
	runID, _ := called["run_id"].(string)
	triggerID, _ := called["trigger_id"].(string)
	if runID == "" || triggerID == "" || called["status"] != "queued" {
		t.Fatalf("call = %v", called)
	}
	notify, ok := called["auto_notification"].(map[string]interface{})
	if !ok {
		t.Fatalf("auto notification not registered: %v", called)
	}
	executionID, _ := notify["execution_id"].(string)

	payloadPath := linkBetaPath + "/triggers/deliveries/" + runID + ".json"
	env.mock.mu.Lock()
	payload := env.mock.files[payloadPath]
	env.mock.mu.Unlock()
	if !strings.Contains(payload, `"task":"Summarise the open bugs."`) || !strings.Contains(payload, `"repo":"rts"`) || !strings.Contains(payload, `"name":"Alpha Bot"`) {
		t.Fatalf("delivery payload = %s", payload)
	}

	polled, err := tools["get_target_run"].exec(ctx, map[string]interface{}{"target": "Beta", "trigger_id": triggerID, "run_id": runID})
	if err != nil {
		t.Fatalf("poll: %v", err)
	}
	if state := decodeToolJSON(t, polled); state["terminal"] != false || state["status"] != "queued" {
		t.Fatalf("poll = %v", state)
	}
	if _, err := tools["send_to_target_run"].exec(ctx, map[string]interface{}{"target": "Beta", "trigger_id": triggerID, "run_id": runID, "message": "Also include P2s."}); err == nil || !strings.Contains(err.Error(), "has not started yet") {
		t.Fatalf("mid-run message before start: err = %v", err)
	}

	runsWorkspace := agentProfileRuntimeWorkspace("owner", linkBetaPath)
	if err := UpdateScheduleRunFinalResponse(ctx, runsWorkspace, runID, "3 open bugs: A, B, C."); err != nil {
		t.Fatal(err)
	}
	if err := UpdateScheduleRun(ctx, runsWorkspace, runID, "success", "", nil, "", "sess-beta"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		agent := env.api.bgAgentRegistry.Get("sess-caller", executionID)
		if agent == nil {
			t.Fatal("watch execution disappeared")
		}
		snapshot := agent.GetSnapshot()
		if snapshot.Status == BGAgentCompleted {
			if !strings.Contains(snapshot.Result, "3 open bugs: A, B, C.") || !strings.Contains(snapshot.Result, "finished (success)") {
				t.Fatalf("notification result = %q", snapshot.Result)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("watch did not complete: %+v", snapshot)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := tools["send_to_target_run"].exec(ctx, map[string]interface{}{"target": "Beta", "trigger_id": triggerID, "run_id": runID, "message": "late"}); err == nil || !strings.Contains(err.Error(), "already finished") {
		t.Fatalf("mid-run message after finish: err = %v", err)
	}
}

func TestCallTargetFailureAndTimeoutNotify(t *testing.T) {
	previous := triggerTargetPollInterval
	triggerTargetPollInterval = 10 * time.Millisecond
	t.Cleanup(func() { triggerTargetPollInterval = previous })
	env := newTriggerLinkEnv(t)
	tools := env.crewTools(t, linkAlphaPath)
	ctx := context.Background()

	waitFor := func(executionID string) BackgroundAgentSnapshot {
		deadline := time.Now().Add(5 * time.Second)
		for {
			snapshot := env.api.bgAgentRegistry.Get("sess-caller", executionID).GetSnapshot()
			if snapshot.Status != BGAgentRunning {
				return snapshot
			}
			if time.Now().After(deadline) {
				t.Fatalf("watch still running: %+v", snapshot)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}

	out, err := tools["call_target"].exec(ctx, map[string]interface{}{"target": "Beta", "task": "fail please", "delivery_id": "fail-1"})
	if err != nil {
		t.Fatal(err)
	}
	called := decodeToolJSON(t, out)
	runID := called["run_id"].(string)
	executionID := called["auto_notification"].(map[string]interface{})["execution_id"].(string)
	if err := UpdateScheduleRun(ctx, agentProfileRuntimeWorkspace("owner", linkBetaPath), runID, "error", "model quota exhausted", nil, "", "sess-beta"); err != nil {
		t.Fatal(err)
	}
	if failed := waitFor(executionID); failed.Status != BGAgentFailed || !strings.Contains(failed.Error, "model quota exhausted") {
		t.Fatalf("failure notification = %+v", failed)
	}

	// A run that never finishes notifies a timeout.
	target, err := resolveTriggerTarget(context.WithValue(ctx, UserContextKey, &UserClaims{UserID: "owner"}), &UserClaims{UserID: "owner"}, "Beta")
	if err != nil {
		t.Fatal(err)
	}
	caller, err := crewTriggerLinkCaller(linkAlphaPath)(context.WithValue(ctx, UserContextKey, &UserClaims{UserID: "owner"}))
	if err != nil {
		t.Fatal(err)
	}
	triggerID := called["trigger_id"].(string)
	stuck, err := tools["call_target"].exec(ctx, map[string]interface{}{"target": "Beta", "task": "hang", "notify": false})
	if err != nil {
		t.Fatal(err)
	}
	stuckRun := decodeToolJSON(t, stuck)["run_id"].(string)
	timeoutID, err := env.api.startTriggerTargetWatch(QueryRequest{SelectedFolder: linkAlphaPath}, "sess-caller", "owner", caller, target, triggerID, stuckRun, 50*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if timedOut := waitFor(timeoutID); timedOut.Status != BGAgentFailed || !strings.Contains(timedOut.Error, "did not finish within") {
		t.Fatalf("timeout notification = %+v", timedOut)
	}
}

// Builder chat (a workflow) connects to a Crew (workflow→crew) and to
// another workflow (workflow→workflow); a workflow cannot call itself.
func TestWorkflowCallerConnectsToCrewAndWorkflow(t *testing.T) {
	env := newTriggerLinkEnv(t)
	tools := env.workflowTools(t, "Workflow/pipeline")
	ctx := context.Background()

	out, err := tools["connect_to_target"].exec(ctx, map[string]interface{}{"target": "#crew:Beta"})
	if err != nil {
		t.Fatalf("workflow→crew connect: %v", err)
	}
	if decodeToolJSON(t, out)["created"] != true {
		t.Fatalf("workflow→crew = %s", out)
	}
	triggers, _ := env.svc.projectWebhookConfigs(ctx, "owner", "work", "beta")
	if len(triggers) != 1 || triggers[0].Caller.Type != triggerCallerWorkflow || triggers[0].Name != "Called by Pipeline" {
		t.Fatalf("crew trigger for workflow caller = %+v", triggers)
	}

	out, err = tools["connect_to_target"].exec(ctx, map[string]interface{}{"target": "#workflow:Reports"})
	if err != nil {
		t.Fatalf("workflow→workflow connect: %v", err)
	}
	connected := decodeToolJSON(t, out)
	manifest, _, err := ReadWorkflowManifest(context.WithValue(ctx, UserContextKey, &UserClaims{UserID: "owner"}), "Workflow/reports")
	if err != nil {
		t.Fatal(err)
	}
	var bound *WorkflowSchedule
	for i := range manifest.Schedules {
		if manifest.Schedules[i].ID == connected["trigger_id"] {
			bound = &manifest.Schedules[i]
		}
	}
	if bound == nil || !bound.IsInternalTrigger() || bound.Caller == nil || bound.Caller.Type != triggerCallerWorkflow || bound.Name != "Called by Pipeline" {
		t.Fatalf("workflow binding = %+v", bound)
	}
	if _, err := tools["connect_to_target"].exec(ctx, map[string]interface{}{"target": "workflow:Pipeline"}); err == nil || !strings.Contains(err.Error(), "cannot call itself") {
		t.Fatalf("self connect err = %v", err)
	}
	if _, err := tools["send_to_target_run"].exec(ctx, map[string]interface{}{"target": "Reports", "trigger_id": "x", "run_id": "y", "message": "hi"}); err == nil || !strings.Contains(err.Error(), "workflow runs do not accept") {
		t.Fatalf("workflow mid-run message err = %v", err)
	}
}

// Workflow triggers accept a workflow caller on dispatch and on poll.
func TestInternalWorkflowTriggerAcceptsWorkflowCaller(t *testing.T) {
	ctx := context.Background()
	manifest := &WorkflowManifest{ID: "reports", Schedules: []WorkflowSchedule{
		{ID: "trig-wf", ScheduleType: "webhook", Enabled: true, Kind: "internal", Caller: &triggerCaller{Type: "workflow", ID: "pipeline"}},
	}}
	receiver := webhookReceiver{
		existing: func(context.Context, string) (schedulerstate.Run, error) {
			return schedulerstate.Run{}, schedulerstate.ErrRunNotFound
		},
		start: func(_ string, _ string, _ string, input *WorkflowWebhookDelivery) (string, error) {
			return input.RunID, nil
		},
	}
	call := internalWorkflowTriggerCall{WorkflowID: "reports", TriggerID: "trig-wf", Caller: triggerCaller{Type: "workflow", ID: "pipeline"}, DeliveryID: "d1", Payload: []byte(`{"task":"x"}`)}
	if result, err := receiver.dispatchInternal(ctx, "Workflow/reports", manifest, "trig-wf", call); err != nil || result.Status != "accepted" {
		t.Fatalf("workflow→workflow dispatch = %+v err=%v", result, err)
	}
	call.Caller = triggerCaller{Type: "crew", ID: "pipeline", ProfileID: "work"}
	if _, err := receiver.dispatchInternal(ctx, "Workflow/reports", manifest, "trig-wf", call); !errors.Is(err, ErrInternalCallerMismatch) {
		t.Fatalf("crew presenting a workflow binding: err = %v", err)
	}
}

func TestWorkflowContextPromptListsTypedTags(t *testing.T) {
	prompt := buildWorkflowContextPromptWithLabels(
		[]string{"_users/owner/Chats/Work/projects/beta", "Workflow/reports"},
		workflowContextLabels([]workflowContextRef{
			{Path: "Chats/Work/projects/beta", Label: "Beta `Bot`\n", Kind: "workflow"},
			{Path: "Workflow/reports/", Label: "Weekly Reports"},
		}),
	)
	for _, want := range []string{
		"**#crew:Beta Bot** (Crew) `_users/owner/Chats/Work/projects/beta/`",
		"**#workflow:Weekly Reports** (workflow) `Workflow/reports/`",
		"never a Slack channel",
		"connect_to_target",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
}
