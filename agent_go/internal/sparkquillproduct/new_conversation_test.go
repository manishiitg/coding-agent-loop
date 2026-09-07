package sparkquillproduct

import (
	"testing"

	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/agentprofiles"
)

// Neither profile offers the generic composer "new chat" control: the
// parent's chats rail has its own New chat button (dispatching the same
// agentworks:product-new-conversation event directly, no capability check
// needed), and the child's conversation is bound to an activity with its own
// "start fresh" flow.
func TestNeitherProfileOffersTheGenericNewConversationControl(t *testing.T) {
	for _, p := range BuiltinAgentProfiles() {
		if p.Runtime.Capabilities.NewConversation != "" && p.Runtime.Capabilities.NewConversation != agentprofiles.CapabilityDisabled {
			t.Fatalf("%s profile new_conversation = %q, want absent or disabled", p.ID, p.Runtime.Capabilities.NewConversation)
		}
	}
}
