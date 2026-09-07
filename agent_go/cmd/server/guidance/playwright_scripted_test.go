package guidance

import (
	"strings"
	"testing"
)

func TestPlaywrightScriptedGuidancePinsIsolationAndFailureEvidence(t *testing.T) {
	doc, err := renderFromRegistry("playwright-scripted", tmplData{}, referenceKinds)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"fresh browser process and fresh context for every case",
		"Do not add a `shared_browser` flag",
		"wall-clock watchdog",
		"SHA-256 hash `main.py`",
		"did run; never report that the workspace “refused to start” it",
	} {
		if !strings.Contains(doc, want) {
			t.Fatalf("Playwright guidance missing %q", want)
		}
	}
}
