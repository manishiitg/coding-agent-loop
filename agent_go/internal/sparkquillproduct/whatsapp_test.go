package sparkquillproduct

import (
	"testing"

	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/agentprofiles"
)

// The parent's own WhatsApp reaches Quill: the parent profile is the
// account's default WhatsApp destination. The child's conversation is bound
// to one activity behind the parent's PIN and must never take WhatsApp
// messages.
func TestOnlyTheParentProfileTakesWhatsApp(t *testing.T) {
	got := map[string]agentprofiles.CapabilityRequirement{}
	for _, p := range BuiltinAgentProfiles() {
		got[p.ID] = p.Runtime.Capabilities.WhatsApp
	}
	if got["sparkquill"] == "" || got["sparkquill"] == agentprofiles.CapabilityDisabled {
		t.Fatalf("parent profile whatsapp = %q, want enabled", got["sparkquill"])
	}
	if got["sparkquill-child"] != "" && got["sparkquill-child"] != agentprofiles.CapabilityDisabled {
		t.Fatalf("child profile whatsapp = %q, want absent or disabled", got["sparkquill-child"])
	}
}
