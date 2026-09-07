package server

import (
	"testing"

	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/agentprofiles"
)

// A product profile's shell sees the account's chat_history/ by default (so
// an assistant can recall earlier chats); a profile that declares
// sandbox.chat_history: none never does.
func TestChatHistoryGrantFollowsTheSandboxPolicy(t *testing.T) {
	const folder = "_users/default/chat_history/"
	if got := agentProfileChatHistoryGrants(agentprofiles.SandboxPolicy{Mode: "strict"}, folder); len(got) != 1 || got[0] != folder {
		t.Fatalf("default policy grants %v, want %q", got, folder)
	}
	if got := agentProfileChatHistoryGrants(agentprofiles.SandboxPolicy{Mode: "strict", ChatHistory: "none"}, folder); len(got) != 0 {
		t.Fatalf("chat_history: none grants %v, want nothing", got)
	}
	if got := agentProfileChatHistoryGrants(agentprofiles.SandboxPolicy{}, ""); len(got) != 0 {
		t.Fatalf("no chat_history folder grants %v, want nothing", got)
	}
}
