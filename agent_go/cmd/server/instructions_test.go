package server

import (
	"strings"
	"testing"
)

func TestWorkspaceMapForbidsWebFetchForLocalArtifacts(t *testing.T) {
	out := GetWorkspaceMap("/tmp/workspace-docs", "_users/default/Chats", "_users/default/memories")

	mustContain := []string{
		"pulse/goals.html",
		"LOCAL paths RELATIVE to the docs root",
		"Never use WebFetch/raw GitHub URLs for workspace artifacts, skills, or reference docs",
		"/tmp/workspace-docs/pulse/",
		`get_reference_doc(kind="...")`,
	}
	for _, s := range mustContain {
		if !strings.Contains(out, s) {
			t.Fatalf("workspace map missing local artifact guardrail %q", s)
		}
	}
}
