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
