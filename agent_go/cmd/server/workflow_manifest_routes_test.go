package server

import (
	"context"
	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"
	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
	"strings"
	"testing"
)

func TestMCPManagementRegistrationFollowsChatPolicy(t *testing.T) {
	for _, tc := range []struct {
		name, mode, session string
		req                 QueryRequest
		active              *ActiveSessionInfo
		readOnly, want      bool
	}{
		{name: "Builder", mode: "workshop", want: true},
		{name: "legacy Builder", want: true},
		{name: "Run chat", mode: "run"},
		{name: "manual workflow execution", req: QueryRequest{AgentMode: "workflow"}},
		{name: "cron shares builder mode", mode: "workshop", req: QueryRequest{TriggeredBy: "cron"}},
		{name: "retained schedule", mode: "workshop", session: "schedule-digest_123"},
		{name: "restored origin", mode: "workshop", active: &ActiveSessionInfo{TriggeredBy: "cron"}},
		{name: "read only Builder", mode: "workshop", readOnly: true},
		{name: "Pulse maintenance", mode: "workshop", req: QueryRequest{TriggeredBy: "cron", PulseLifecycleTurn: true}},
		{name: "Pulse reviewer", mode: "workshop", req: QueryRequest{SessionKind: "pulse_reviewer", ParentSessionID: "parent"}},
		{name: "restored child", mode: "workshop", active: &ActiveSessionInfo{ParentSessionID: "parent"}},
		{name: "notification", mode: "workshop", req: QueryRequest{IsAutoNotification: true}},
		{name: "bot", mode: "workshop", req: QueryRequest{BotPlatform: "slack"}},
		{name: "promoted schedule", mode: "workshop", session: "schedule-digest_123", req: QueryRequest{UserInteractiveContinuation: true}, want: true},
		{name: "promotion cannot elevate child", mode: "workshop", req: QueryRequest{UserInteractiveContinuation: true, SessionKind: "pulse_reviewer"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			api := &StreamingAPI{}
			reg := &recordingRegistrar{}
			policy := resolveWorkflowChatPolicy(tc.mode, tc.session, tc.req, tc.active, tc.readOnly)
			if err := api.registerMCPToolsForChat(reg, policy, nil); err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"search_mcp_catalog", "install_mcp_server", "add_mcp_server", "remove_mcp_server", "trigger_mcp_discovery"} {
				_, got := reg.tools[name]
				if got != tc.want {
					t.Fatalf("%s registered=%v want %v (%+v)", name, got, tc.want, policy)
				}
			}
			if tc.want {
				// Exercise the real handler without making a network request.
				result, err := reg.tools["install_mcp_server"].exec(context.Background(), map[string]interface{}{})
				if err != nil || !strings.Contains(result, "name is required") {
					t.Fatalf("missing required name: %q %v", result, err)
				}
			}
		})
	}
}

func TestPulseMaintenanceRetainsApprovedImprovementAuthority(t *testing.T) {
	p := resolveWorkflowChatPolicy("workshop", "schedule-pulse_123", QueryRequest{TriggeredBy: "cron", PulseLifecycleTurn: true}, nil, false)
	if p.Origin != "pulse" || !p.allows("plan_authoring") || p.allows("mcp_management") || p.allows("workspace_ui") {
		t.Fatalf("Pulse authority changed: %+v", p)
	}
}

// Exercise the production phase setup entry point. Stop before opening a live
// workflow controller: admission and registration must already be complete.
type chatPolicyTestDefinition struct{ recordingRegistrar }

func (*chatPolicyTestDefinition) AttachSkill(*llmtypes.Skill) error { return nil }
func (*chatPolicyTestDefinition) AttachedSkills() []*llmtypes.Skill { return nil }

func TestBuilderPhaseActuallyRegistersMCPManagement(t *testing.T) {
	for _, readOnly := range []bool{false, true} {
		api := &StreamingAPI{logger: loggerv2.NewNoop(), stoppedSessions: map[string]bool{"policy-chat": true}}
		reg := &chatPolicyTestDefinition{}
		err := api.installWorkflowPhaseTools(context.Background(), reg, "policy-chat", "test-user", "workflow-builder", "", "", map[string]string{"WorkshopMode": "workshop"}, nil, nil, nil, nil, nil, QueryRequest{}, readOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"search_mcp_catalog", "install_mcp_server", "add_mcp_server"} {
			_, exists := reg.tools[name]
			if exists == readOnly {
				t.Fatalf("phase setup %s present=%v readOnly=%v", name, exists, readOnly)
			}
		}
	}
}

func TestChatPolicyReconnectPreservesCompatibleHistory(t *testing.T) {
	for _, tc := range []struct {
		name          string
		coding, known bool
		previous      string
		saved         *ChatHistoryAgentRuntime
		want          bool
	}{
		{name: "fresh API chat"},
		{name: "changed policy on API chat", known: true, previous: "old"},
		{name: "fresh coding chat", coding: true},
		{name: "unchanged live coding chat", coding: true, known: true, previous: "current"},
		{name: "changed live coding chat", coding: true, known: true, previous: "old", want: true},
		{name: "retained compatible after restart", coding: true, saved: &ChatHistoryAgentRuntime{ExternalSessionID: "native-id", ChatPolicyKey: "current"}},
		{name: "legacy retained needs new catalog", coding: true, saved: &ChatHistoryAgentRuntime{ExternalSessionID: "native-id"}, want: true},
		{name: "retained changed policy", coding: true, saved: &ChatHistoryAgentRuntime{ExternalSessionID: "native-id", ChatPolicyKey: "old"}, want: true},
		{name: "saved API history has no native session", coding: true, saved: &ChatHistoryAgentRuntime{Kind: "llm_agent"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := chatPolicyRequiresReconnect(tc.coding, tc.previous, "current", tc.known, tc.saved); got != tc.want {
				t.Fatalf("reconnect=%v want=%v", got, tc.want)
			}
		})
	}
	api := &StreamingAPI{lastChatPolicyBySession: map[string]string{"chat": "current"}}
	runtime := api.captureChatHistoryAgentRuntime("chat", "cursor-cli", "test-model", "Workflow/test", nil)
	if runtime.ChatPolicyKey != "current" {
		t.Fatal("native runtime did not persist its policy fingerprint")
	}
}
