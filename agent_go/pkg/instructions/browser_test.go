package instructions

import (
	"strings"
	"testing"
)

func TestBuildBrowserInstructionsListsAuthorizedCDPProfiles(t *testing.T) {
	t.Setenv("CDP_HOST", "localhost")
	got := BuildBrowserInstructions(BrowserConfig{
		HasAgentBrowser: true,
		Mode:            "cdp",
		CdpPort:         9222,
		CdpPorts:        []int{9222, 9333},
	})
	for _, want := range []string{"http://localhost:9222", "http://localhost:9333", "multi-login testing"} {
		if !strings.Contains(got, want) {
			t.Fatalf("browser instructions missing %q", want)
		}
	}
}

func TestBuildBrowserInstructionsKeepsAutoModeDynamic(t *testing.T) {
	t.Setenv("CDP_HOST", "localhost")
	got := BuildBrowserInstructions(BrowserConfig{
		HasAgentBrowser: true,
		Mode:            "auto",
		CdpPort:         9222,
		CdpPorts:        []int{9222},
	})
	for _, want := range []string{"agent_browser", "status", "effective_mode", "http://localhost:9222", "never taken from saved conversation"} {
		if !strings.Contains(got, want) {
			t.Fatalf("auto browser instructions missing %q", want)
		}
	}
	if strings.Contains(got, "Browser Mode: Headless") || strings.Contains(got, "Browser Mode: CDP") {
		t.Fatalf("auto browser instructions must not persist a resolved mode: %s", got)
	}
}
