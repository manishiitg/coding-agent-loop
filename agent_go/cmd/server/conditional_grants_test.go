package server

import (
	"strings"
	"testing"
)

func TestWorkProfileCanAuthorCustomSkillsWithoutSkillBuilderIdentity(t *testing.T) {
	grants := resolveConditionalGrants(QueryRequest{AgentProfileID: "work"})

	if !grants.HasGrant("work-custom-skills") {
		t.Fatal("Work profile did not receive its custom-skill authoring grant")
	}
	if grants.HasGrant("skill-creator") {
		t.Fatal("Work profile must not enter the separate Skill Builder mode")
	}
	if len(grants.WriteFolders) != 1 || grants.WriteFolders[0] != "skills/custom/" {
		t.Fatalf("Work custom-skill write folders = %v, want [skills/custom/]", grants.WriteFolders)
	}
	prompt := strings.Join(grants.PromptSections, "\n")
	if !strings.Contains(prompt, "explicitly asks") || !strings.Contains(prompt, "normal Work identity") || !strings.Contains(prompt, "never store secrets") {
		t.Fatalf("Work custom-skill prompt is missing its authorization, identity, or security boundary: %q", prompt)
	}
}

func TestNonWorkProfileDoesNotReceiveCustomSkillAuthoringGrant(t *testing.T) {
	grants := resolveConditionalGrants(QueryRequest{AgentProfileID: "agentworks"})
	if grants.HasGrant("work-custom-skills") {
		t.Fatal("non-Work profile unexpectedly received Work's custom-skill authoring grant")
	}
}
