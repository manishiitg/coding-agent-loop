package server

import (
	"testing"

	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/agentprofiles"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/common"
)

func TestCrewBrowserIsolationPersistsPerUserProject(t *testing.T) {
	profile := &resolvedAgentProfile{Definition: agentprofiles.Profile{ID: "work"}}
	for _, sessionID := range []string{"crew-a-chat-one", "crew-a-chat-two", "crew-b-chat", "crew-a-other-user"} {
		t.Cleanup(func() { common.ClearSessionShellConfig(sessionID) })
	}

	bindConversationBrowserIsolation("crew-a-chat-one", "alice", "_users/alice/Chats/Work/projects/crew-a", profile)
	bindConversationBrowserIsolation("crew-a-chat-two", "alice", "Chats/Work/projects/crew-a", profile)
	bindConversationBrowserIsolation("crew-b-chat", "alice", "Chats/Work/projects/crew-b", profile)
	bindConversationBrowserIsolation("crew-a-other-user", "bob", "Chats/Work/projects/crew-a", profile)

	first := common.ResolveBrowserSessionID("crew-a-chat-one", "default")
	if got := common.ResolveBrowserSessionID("crew-a-chat-two", "another-label"); got != first {
		t.Fatalf("same Crew did not retain its browser: %q != %q", got, first)
	}
	if got := common.ResolveBrowserSessionID("crew-b-chat", "default"); got == first {
		t.Fatalf("different Crews shared browser %q", got)
	}
	if got := common.ResolveBrowserSessionID("crew-a-other-user", "default"); got == first {
		t.Fatalf("different users shared Crew browser %q", got)
	}
}

func TestWorkflowBrowserIsolationStillSharesAcrossAuthorizedUsers(t *testing.T) {
	for _, sessionID := range []string{"workflow-alice", "workflow-bob"} {
		t.Cleanup(func() { common.ClearSessionShellConfig(sessionID) })
	}
	bindConversationBrowserIsolation("workflow-alice", "alice", "Workflow/shared", nil)
	bindConversationBrowserIsolation("workflow-bob", "bob", "Workflow/shared", nil)
	if alice, bob := common.ResolveBrowserSessionID("workflow-alice", "default"), common.ResolveBrowserSessionID("workflow-bob", "default"); alice != bob {
		t.Fatalf("authorized workflow users got different browsers: %q != %q", alice, bob)
	}
}
