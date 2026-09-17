package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadPlaybookCatalogFindsEngineeringPlaybooks(t *testing.T) {
	items, err := loadPlaybookCatalog()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 23 {
		t.Fatalf("catalog has %d playbooks, want 23", len(items))
	}
	intelligence, err := findPlaybook("engineering-operations-intelligence")
	if err != nil {
		t.Fatal(err)
	}
	if intelligence.TeamScope != "small_team" || intelligence.Category != "Engineering Operations Intelligence" {
		t.Fatalf("engineering intelligence = %+v", intelligence)
	}
	item, err := findPlaybook("application-security-assessment-remediation")
	if err != nil {
		t.Fatal(err)
	}
	if item.Category != "Security Engineering" {
		t.Fatalf("category = %q, want Security Engineering", item.Category)
	}
	for _, playbook := range items {
		if len(playbook.PulseFocus) != 1 || playbook.PulseFocus[0]["module"] != pulseModuleStrategicReview {
			t.Fatalf("playbook %s focus = %#v, want strategic only", playbook.ID, playbook.PulseFocus)
		}
	}
}

func TestStrategicPlaybookFocusIgnoresLegacyReviewerSpecialization(t *testing.T) {
	focuses := []map[string]interface{}{
		{"module": pulseModuleTechnicalReview, "label": "legacy technical"},
		{"module": pulseModuleArchitectureReview, "label": "legacy architecture"},
		{"module": pulseModuleStrategicReview, "label": "strategy"},
	}
	filtered := strategicPlaybookFocus(focuses)
	if len(filtered) != 1 || filtered[0]["label"] != "strategy" {
		t.Fatalf("filtered focus = %#v, want strategy only", filtered)
	}
}

func TestRewriteExternalPlaybookLinksMaterializesSharedReferences(t *testing.T) {
	item, err := findPlaybook("basic-browser-setup")
	if err != nil {
		t.Fatal(err)
	}
	skillPath := filepath.Join(item.SourceDir, "SKILL.md")
	content, err := os.ReadFile(skillPath)
	if err != nil {
		t.Fatal(err)
	}
	external := map[string]string{}
	rewritten := rewriteExternalPlaybookLinks(string(content), skillPath, item.SourceDir, external)
	if strings.Contains(rewritten, "](../references/") {
		t.Fatalf("shared links were not rewritten:\n%s", rewritten)
	}
	if len(external) != 3 {
		t.Fatalf("materialized references = %v, want three", external)
	}
}

func TestInstalledBuilderSkillNamesExcludesDisabledAndDuplicates(t *testing.T) {
	names := installedBuilderSkillNames([]InstalledPlaybook{
		{SkillName: "basic-browser-setup", Status: "draft"},
		{SkillName: "basic-browser-setup", Status: "ready"},
		{SkillName: "disabled-guide", Status: "disabled"},
		{SkillName: "", Status: "ready"},
	})
	if len(names) != 1 || names[0] != "basic-browser-setup" {
		t.Fatalf("builder skills = %v", names)
	}
}
