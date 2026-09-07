package sparkquillproduct

import (
	"strings"
	"testing"
)

// Quill can recall earlier conversations — its own with the parent and each
// activity's with the tutor — through the platform's chat logs; the child,
// confined to one activity folder, never sees the family's logs at all.
func TestOnlyTheParentCanRecallConversations(t *testing.T) {
	profiles := map[string]bool{}
	for _, p := range BuiltinAgentProfiles() {
		profiles[p.ID] = true
		hasSkill := false
		for _, skill := range p.Skills {
			if skill == "recall-conversations" {
				hasSkill = true
			}
		}
		switch p.ID {
		case ParentProfileID:
			if p.Runtime.Sandbox.ChatHistoryDenied() {
				t.Fatal("parent profile cannot read the chat logs, want access for recall")
			}
			if !hasSkill {
				t.Fatal("parent profile lacks the recall-conversations skill")
			}
			if !strings.Contains(WorkspaceLayout(), "chat_history") {
				t.Fatal("workspace layout never tells Quill where the conversation logs are")
			}
		case ChildProfileID:
			if !p.Runtime.Sandbox.ChatHistoryDenied() {
				t.Fatal("child profile can read the family's chat logs, want sandbox.chat_history: none")
			}
			if hasSkill {
				t.Fatal("child profile has the recall-conversations skill, want none")
			}
		}
	}
	if !profiles[ParentProfileID] || !profiles[ChildProfileID] {
		t.Fatalf("expected both profiles, got %v", profiles)
	}
}
