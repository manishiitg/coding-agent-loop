package step_based_workflow

import (
	"strings"
	"testing"
)

func TestCompletePlanChangelogEntryTypesLearningMutations(t *testing.T) {
	for _, tc := range []struct {
		tool, target, dependency string
	}{
		{"runtime_learning_update", "learnings/_global", "runtime_guidance"},
	} {
		entry := PlanChangelogEntry{
			Tool:    tc.tool,
			Changes: []PlanFieldChange{{Field: "description", OldValue: "before", NewValue: "after"}},
		}
		completePlanChangelogEntry(&entry)
		if entry.Target != tc.target || entry.DependencyClass != tc.dependency {
			t.Fatalf("%s classified as target=%q dependency=%q", tc.tool, entry.Target, entry.DependencyClass)
		}
		if !strings.HasPrefix(entry.BeforeRef, "sha256:") || !strings.HasPrefix(entry.AfterRef, "sha256:") || entry.BeforeRef == entry.AfterRef {
			t.Fatalf("%s refs are not immutable/distinct: before=%q after=%q", tc.tool, entry.BeforeRef, entry.AfterRef)
		}
		if entry.Actor != "managed_tool:"+tc.tool {
			t.Fatalf("%s actor = %q", tc.tool, entry.Actor)
		}
	}
}
