package guidance

import (
	"strings"
	"testing"
)

func TestReportingPolicyUsesManagedLazyMediaURLs(t *testing.T) {
	rendered, err := renderFromRegistry("reporting-policy", tmplData{}, referenceKinds)
	if err != nil {
		t.Fatalf("render reporting-policy: %v", err)
	}

	for _, want := range []string{
		"window.report.mediaUrl(path)",
		`preload="metadata"`,
		"never call an internal report-preview/report-media HTTP endpoint directly",
		"never",
		"persist the workspace-relative path",
	} {
		if !strings.Contains(strings.Join(strings.Fields(rendered), " "), want) {
			t.Fatalf("reporting policy missing managed media guidance %q", want)
		}
	}
}
