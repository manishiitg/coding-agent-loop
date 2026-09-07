package sparkquillproduct

import (
	"strings"
	"testing"

	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/agentprofiles"
)

// The parent's own WhatsApp reaches Quill: the parent profile is the
// account's default WhatsApp destination. The child's conversation is bound
// to one activity behind the parent's PIN; it is reached from WhatsApp only
// through the parent's "@child", never as a destination of its own.
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

// "@child" on WhatsApp is the tutor in the activity the child currently has
// — the same keyed conversation the app opens (its key is the activity
// slug) — with attachments going into that activity's attempts/ folder.
// "@parent" is Quill. Anything else is not SparkQuill's token.
func TestChannelRouteForParentAndChild(t *testing.T) {
	state := FamilyState{Child: &Child{Name: "Myra"}}
	const root = "Chats/SparkQuill"

	route, ok, err := ChannelRouteFor("parent", state, root, "activities/2026-09-01-fractions")
	if err != nil || !ok || route.ProfileID != ParentProfileID || route.ConversationKey != "" {
		t.Fatalf("@parent = (%+v, %v, %v), want Quill's own conversation", route, ok, err)
	}

	route, ok, err = ChannelRouteFor("Child", state, root, "activities/2026-09-01-fractions")
	if err != nil || !ok {
		t.Fatalf("@child = (%+v, %v, %v)", route, ok, err)
	}
	if route.ProfileID != ChildProfileID || route.ConversationKey != "2026-09-01-fractions" {
		t.Fatalf("@child conversation = %s/%q, want the child profile keyed by the activity slug", route.ProfileID, route.ConversationKey)
	}
	if route.UploadFolder != "Chats/SparkQuill/activities/2026-09-01-fractions/attempts" {
		t.Fatalf("@child attachments go to %q, want the activity's attempts/ folder", route.UploadFolder)
	}
	if !strings.Contains(route.Label, "Myra") {
		t.Fatalf("@child label = %q, want the child's name", route.Label)
	}

	// A pointer written with the family root on it resolves the same way.
	route, _, err = ChannelRouteFor("child", state, root, "Chats/SparkQuill/activities/2026-09-01-fractions/")
	if err != nil || route.ConversationKey != "2026-09-01-fractions" {
		t.Fatalf("rooted pointer: key=%q err=%v", route.ConversationKey, err)
	}

	// No handoff yet: SparkQuill knows the token but cannot serve it, and
	// says so in words the parent can act on.
	if _, ok, err := ChannelRouteFor("child", state, root, ""); !ok || err == nil || !strings.Contains(err.Error(), "Myra") {
		t.Fatalf("@child with no activity = (ok=%v, err=%v), want a named error", ok, err)
	}

	if _, ok, err := ChannelRouteFor("report", state, root, "activities/x"); ok || err != nil {
		t.Fatalf("@report = (ok=%v, err=%v), want not SparkQuill's", ok, err)
	}
}
