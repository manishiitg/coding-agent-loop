package agentworksproduct

import "testing"

func TestChatPolicyManifestAuthority(t *testing.T) {
	for _, mode := range []string{"builder", "run"} {
		for _, origin := range []string{"interactive", "scheduled", "pulse", "child", "bot", "notification", "unknown"} {
			for _, readOnly := range []bool{false, true} {
				got := ChatCapabilities(mode, origin, readOnly)
				want := mode == "builder" && origin == "interactive" && !readOnly
				if got["mcp_management"] != want {
					t.Fatalf("MCP admission %s/%s readOnly=%v: %v", mode, origin, readOnly, got)
				}
				if readOnly && got["plan_authoring"] {
					t.Fatal("read-only user can author")
				}
			}
		}
	}
}

func TestChatPolicyManifestRejectsUnknownCapabilities(t *testing.T) {
	m, err := AgentWorksManifest()
	if err != nil {
		t.Fatal(err)
	}
	// Copy policy/maps: never mutate the cached product manifest.
	p := *m.ChatPolicy
	p.Modes = map[string][]string{"builder": {"mcp_managment"}, "run": {}}
	m.ChatPolicy = &p
	if validateChatPolicy(m) == nil {
		t.Fatal("typo silently accepted")
	}
}
