package server

import (
	"context"
	"strings"
	"testing"

	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/workflowtypes"
)

func TestWorkflowUIRegistrationFollowsCallerNotSharedBuilderPhase(t *testing.T) {
	for _, tc := range []struct {
		name, session, phase string
		req                  QueryRequest
		active               *ActiveSessionInfo
		want                 bool
	}{
		{name: "interactive builder", session: "chat-1", want: true},
		// No WorkshopMode/read-only flag is consulted: all human Builders keep
		// presentation tools, even when they lack plan mutation authority.
		{name: "manual builder", session: "chat-1", req: QueryRequest{TriggeredBy: "manual"}, want: true},
		{name: "cron same builder phase", session: "opaque-session", req: QueryRequest{TriggeredBy: "cron"}},
		{name: "manual schedule trigger", session: "schedule-manual--digest_123", req: QueryRequest{TriggeredBy: "manual"}},
		{name: "restored scheduled identity", session: "schedule-digest_123"},
		{name: "stored cron origin survives missing request metadata", session: "opaque-session", active: &ActiveSessionInfo{TriggeredBy: "cron"}},
		{name: "scheduled native process retained", session: "schedule-digest_123", req: QueryRequest{KeepNativeSessionAlive: true}},
		{name: "scheduled trigger variant", session: "opaque-session", req: QueryRequest{TriggeredBy: "workflow_schedule"}},
		{name: "Pulse child", session: "child", req: QueryRequest{ParentSessionID: "chat-1", SessionKind: "pulse_reviewer"}},
		{name: "restored child", session: "child", active: &ActiveSessionInfo{ParentSessionID: "chat-1"}},
		{name: "bot", session: "bot", req: QueryRequest{BotPlatform: "slack"}},
		{name: "product", session: "product", req: QueryRequest{AgentProfileID: "video-studio"}},
		{name: "auto notification", session: "chat-1", req: QueryRequest{IsAutoNotification: true}},
		{name: "non builder phase", session: "chat-1", phase: "execution"},
		{name: "explicit promotion", session: "schedule-digest_123", req: QueryRequest{UserInteractiveContinuation: true}, active: &ActiveSessionInfo{TriggeredBy: "cron"}, want: true},
		{name: "promotion cannot elevate Pulse child", session: "child", req: QueryRequest{UserInteractiveContinuation: true, SessionKind: "pulse_reviewer"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			phase := tc.phase
			if phase == "" {
				phase = workflowtypes.WorkflowStatusWorkflowBuilder
			}
			api := &StreamingAPI{activeSessions: map[string]*ActiveSessionInfo{}}
			if tc.active != nil {
				api.activeSessions[tc.session] = tc.active
			}
			reg := &recordingRegistrar{}
			if err := api.registerWorkflowUIForCaller(reg, phase, tc.session, "Workflow/test", tc.req, false); err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"open_workspace_view", "refresh_workspace_view", "list_ui_capabilities", "get_ui_state", "perform_ui_action", "get_ui_action_result"} {
				_, exists := reg.tools[name]
				if exists != tc.want {
					t.Fatalf("%s present=%v want=%v", name, exists, tc.want)
				}
			}
			if !tc.want && api.uiBroker().scope(tc.session) != "" {
				t.Fatal("excluded caller can bind a browser")
			}
		})
	}
}

func TestWorkflowUIScheduleReclassificationRevokesOldLease(t *testing.T) {
	api := &StreamingAPI{activeSessions: map[string]*ActiveSessionInfo{}}
	phase := workflowtypes.WorkflowStatusWorkflowBuilder
	initial := &recordingRegistrar{}
	if err := api.registerWorkflowUIForCaller(initial, phase, "same-session", "Workflow/test", QueryRequest{}, false); err != nil {
		t.Fatal(err)
	}
	b := api.uiBroker()
	client, _ := b.bind("same-session")
	a, _, err := b.submit("same-session", "notify", "open", "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := api.registerWorkflowUIForCaller(&recordingRegistrar{}, phase, "same-session", "Workflow/test", QueryRequest{TriggeredBy: "cron"}, false); err != nil {
		t.Fatal(err)
	}
	if _, err := b.syncClient("same-session", client.id, client.token, uiSnapshot{}); err == nil {
		t.Fatal("stale UI lease survived")
	}
	result, _ := b.result("same-session", a.RequestID)
	if result.Status != "cancelled" {
		t.Fatal(result)
	}
	for _, name := range []string{"open_workspace_view", "refresh_workspace_view"} {
		out, err := initial.tools[name].exec(context.Background(), map[string]interface{}{"view": "notify"})
		if err != nil || !strings.Contains(out, "inactive_scope") {
			t.Fatalf("retained %s remained callable: %s %v", name, out, err)
		}
	}
}

func TestWorkspacePresentationDoesNotGrantConnectionWriteAccess(t *testing.T) {
	for _, readOnly := range []bool{false, true} {
		reg := &recordingRegistrar{}
		api := &StreamingAPI{}
		if err := api.registerWorkflowUIForCaller(reg, "workflow-builder", "chat", "Workflow/test", QueryRequest{}, readOnly); err != nil {
			t.Fatal(err)
		}
		if _, exists := reg.tools["open_workspace_view"]; !exists {
			t.Fatal("read-only users must keep presentation")
		}
		_, exists := reg.tools["update_gmail_connection_grants"]
		if exists == readOnly {
			t.Fatalf("connection mutation present=%v readOnly=%v", exists, readOnly)
		}
	}
}
