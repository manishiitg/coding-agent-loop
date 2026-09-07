package server

import (
	"testing"

	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/agentprofiles"
)

func whatsappTestProfile(capability agentprofiles.CapabilityRequirement, root string) agentprofiles.Profile {
	profile := routeTestProfile("sparkquill", true, "")
	profile.Name = "SparkQuill"
	profile.Runtime.Capabilities.WhatsApp = capability
	profile.Runtime.Workspace.Root = root
	return profile
}

// Only a profile that declared the whatsapp capability in its product.yaml
// can be a pairing's default destination.
func TestProfileTakesWhatsAppFollowsTheDeclaredCapability(t *testing.T) {
	for _, tc := range []struct {
		requirement agentprofiles.CapabilityRequirement
		want        bool
	}{
		{"", false},
		{agentprofiles.CapabilityDisabled, false},
		{agentprofiles.CapabilityPreferred, true},
		{agentprofiles.CapabilityRequired, true},
	} {
		if got := profileTakesWhatsApp(whatsappTestProfile(tc.requirement, "Chats/SparkQuill")); got != tc.want {
			t.Errorf("whatsapp=%q: takes WhatsApp = %v, want %v", tc.requirement, got, tc.want)
		}
	}
}

// A product's attachment folder is kept inside its own fixed workspace root,
// where its folder guard lets the profile read what arrives.
func TestWhatsAppUploadFolderStaysInsideTheProfileWorkspace(t *testing.T) {
	profile := whatsappTestProfile(agentprofiles.CapabilityPreferred, "Chats/SparkQuill")
	for _, tc := range []struct {
		requested string
		want      string
		wantErr   bool
	}{
		{"", "", false},
		{"inbox", "Chats/SparkQuill/inbox", false},
		{"/inbox/", "Chats/SparkQuill/inbox", false},
		{"Chats/SparkQuill/inbox", "Chats/SparkQuill/inbox", false},
		{"Chats/SparkQuill", "Chats/SparkQuill", false},
		{"../Other", "", true},
		{"inbox/../../Other", "", true},
		{"..", "", true},
	} {
		got, err := whatsappUploadFolderFor(profile, tc.requested)
		if tc.wantErr {
			if err == nil {
				t.Errorf("requested %q: got %q, want an error", tc.requested, got)
			}
			continue
		}
		if err != nil || got != tc.want {
			t.Errorf("requested %q: got (%q, %v), want %q", tc.requested, got, err, tc.want)
		}
	}
	noRoot := whatsappTestProfile(agentprofiles.CapabilityPreferred, "")
	if _, err := whatsappUploadFolderFor(noRoot, "inbox"); err == nil {
		t.Fatal("a profile with no fixed root accepted an upload folder, want an error")
	}
}

// A WhatsApp turn runs on the engine the conversation is already bound to,
// else the profile's default option — so a first turn from WhatsApp binds
// the conversation exactly as the app's first turn would.
func TestWhatsAppEngineFollowsTheConversationBinding(t *testing.T) {
	profile := whatsappTestProfile(agentprofiles.CapabilityPreferred, "Chats/SparkQuill")
	profile.Runtime.ProviderOptions = []agentprofiles.ProviderOption{
		{ID: "claude-code", Provider: "claude-code", ModelID: "claude-fable-5-1", Default: true},
		{ID: "codex-cli", Provider: "codex-cli", ModelID: "gpt-6-astra"},
	}
	if option, ok := whatsappEngineFor(profile, ProductConversationRecord{}); !ok || option.ID != "claude-code" {
		t.Fatalf("unbound conversation engine = (%q, %v), want the default claude-code", option.ID, ok)
	}
	if option, ok := whatsappEngineFor(profile, ProductConversationRecord{Provider: "codex-cli"}); !ok || option.ID != "codex-cli" {
		t.Fatalf("codex-bound conversation engine = (%q, %v), want codex-cli", option.ID, ok)
	}
	profile.Runtime.ProviderOptions = nil
	if _, ok := whatsappEngineFor(profile, ProductConversationRecord{}); ok {
		t.Fatal("a profile with no provider options offered an engine")
	}
}
