package server

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPlaybookReinstallPreservesProgressAndUpdateArchivesEvidence(t *testing.T) {
	item, err := findPlaybook("website-growth-loop")
	if err != nil {
		t.Fatal(err)
	}
	mock := &mockWorkspaceAPI{files: map[string]string{}}
	ws := httptest.NewServer(mock)
	defer ws.Close()
	t.Setenv("WORKSPACE_API_URL", ws.URL)
	ctx := context.Background()
	const workspace = "Workflow/growth"
	const skill = "agentworks-playbook-website-growth-loop"
	const setupPath = workspace + "/skills/" + skill + "/SETUP.json"
	originalHash, err := installPlaybookSkill(ctx, workspace, skill, item)
	if err != nil {
		t.Fatal(err)
	}
	var progress map[string]interface{}
	if err := json.Unmarshal([]byte(mock.files[setupPath]), &progress); err != nil {
		t.Fatal(err)
	}
	progress["completed_steps"] = []string{"goal_owner"}
	progress["evidence"] = map[string]string{"goal_owner": "Owner reviewed the actual website and goal"}
	saved, _ := json.Marshal(progress)
	mock.files[setupPath] = string(saved)
	retryHash, err := installPlaybookSkill(ctx, workspace, skill, item)
	if err != nil {
		t.Fatal(err)
	}
	if retryHash != originalHash || mock.files[setupPath] != string(saved) {
		t.Fatal("reinstall changed the source hash or erased customer setup progress")
	}
	// Emulate an older installed version; update must preserve the evidence
	// before replacing the working checklist with the current source.
	progress["playbook_version"] = "0.2.0"
	saved, _ = json.Marshal(progress)
	mock.files[setupPath] = string(saved)
	if _, err := installPlaybookSkill(ctx, workspace, skill, item); err != nil {
		t.Fatal(err)
	}
	var updated struct {
		Previous  string   `json:"previous_setup_path"`
		Completed []string `json:"completed_steps"`
	}
	if err := json.Unmarshal([]byte(mock.files[setupPath]), &updated); err != nil {
		t.Fatal(err)
	}
	if updated.Previous == "" || mock.files[updated.Previous] != string(saved) || len(updated.Completed) != 0 {
		t.Fatal("update lost earlier evidence or carried unreviewed checks into the new version")
	}
}

func TestLoadPlaybookCatalogFindsEngineeringPlaybooks(t *testing.T) {
	items, err := loadPlaybookCatalog()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 24 {
		t.Fatalf("catalog has %d playbooks, want 24", len(items))
	}
	growth, err := findPlaybook("website-growth-loop")
	if err != nil {
		t.Fatal(err)
	}
	if growth.Category != "Website Growth" || growth.Version != "0.3.0" {
		t.Fatalf("website growth loop = %+v", growth)
	}
	setupSource, err := os.ReadFile(filepath.Join(growth.SourceDir, "SETUP.json"))
	if err != nil {
		t.Fatal(err)
	}
	var setup struct {
		PlaybookID      string `json:"playbook_id"`
		PlaybookVersion string `json:"playbook_version"`
		Checks          []struct {
			ID string `json:"id"`
		} `json:"checks"`
	}
	if err := json.Unmarshal(setupSource, &setup); err != nil {
		t.Fatal(err)
	}
	if setup.PlaybookID != growth.ID || setup.PlaybookVersion != growth.Version || len(setup.Checks) != len(growth.SetupChecks) {
		t.Fatalf("website growth setup = %+v", setup)
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
