package server

import (
	"strings"
	"testing"
)

func TestSkillSelectionHintUsesProductSpecificConfiguration(t *testing.T) {
	work := skillSelectionHint("work", "It")
	for _, want := range []string{"read_skill", "Setup > Skills", "workflow.json"} {
		if !strings.Contains(work, want) {
			t.Fatalf("Work skill hint is missing %q: %q", want, work)
		}
	}
	if strings.Contains(work, "update_workflow_config") {
		t.Fatalf("Work skill hint advertised an AgentWorks-only tool: %q", work)
	}

	agentWorks := skillSelectionHint("", "It")
	if !strings.Contains(agentWorks, "update_workflow_config") {
		t.Fatalf("AgentWorks skill hint lost workflow selection guidance: %q", agentWorks)
	}
}
