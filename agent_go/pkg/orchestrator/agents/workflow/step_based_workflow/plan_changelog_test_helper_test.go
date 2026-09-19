package step_based_workflow

import (
	"encoding/json"
	"strings"
	"testing"
)

// findChangelogEntry scans the in-memory workspace for the planning/changelog
// file this test run wrote and returns the entry for the given tool.
func findChangelogEntry(t *testing.T, workspacePath string, files map[string]string, tool string) PlanChangelogEntry {
	t.Helper()
	prefix := workspacePath + "/planning/changelog/"
	for path, content := range files {
		if !strings.HasPrefix(path, prefix) {
			continue
		}
		var clog PlanChangelog
		if err := json.Unmarshal([]byte(content), &clog); err != nil {
			continue
		}
		for _, entry := range clog.Entries {
			if entry.Tool == tool {
				return entry
			}
		}
	}
	t.Fatalf("no changelog entry found for tool %q", tool)
	return PlanChangelogEntry{}
}
