package agentprofiles

import (
	"strings"
	"testing"
	"testing/fstest"
)

func TestResolveFeaturesProjectsOneBundleIntoExistingProfileFields(t *testing.T) {
	profile := Profile{
		ToolPolicy: ToolPolicy{Mode: ToolPolicyModeAllowlist},
		Features:   []FeatureBinding{{ID: "dashboard"}, {ID: "browser"}},
	}
	if err := ResolveFeatures(&profile); err != nil {
		t.Fatal(err)
	}
	// dashboard pulls its database and files dependencies in before itself.
	wantOrder := []string{"database", "files", "dashboard", "browser"}
	if len(profile.ResolvedFeatures) != len(wantOrder) {
		t.Fatalf("resolved features = %+v", profile.ResolvedFeatures)
	}
	for i, want := range wantOrder {
		if profile.ResolvedFeatures[i].ID != want {
			t.Fatalf("resolved feature %d = %q, want %q", i, profile.ResolvedFeatures[i].ID, want)
		}
	}
	for _, tool := range []string{"query_workflow_db", "diff_patch_workspace_file", "validate_report_html", "agent_browser"} {
		if !containsString(profile.ToolPolicy.Enabled, tool) {
			t.Fatalf("feature projection omitted tool %q: %v", tool, profile.ToolPolicy.Enabled)
		}
	}
	if !containsString(profile.Skills, "work-dashboard") || !containsString(profile.Skills, "ui-ux-pro-max") || !containsString(profile.Skills, "agent-browser") {
		t.Fatalf("feature projection omitted skills: %v", profile.Skills)
	}
	if !profile.UIPanels.Files || profile.Runtime.Capabilities.Browser != CapabilityPreferred {
		t.Fatalf("feature projection omitted legacy fields: panels=%+v caps=%+v", profile.UIPanels, profile.Runtime.Capabilities)
	}
	if got := strings.Join(FeaturePromptExtensions(profile), "\n"); !strings.Contains(got, "Feature: dashboard") || !strings.Contains(got, "Feature: browser") ||
		!strings.Contains(got, "attached `work-dashboard` skill") || !strings.Contains(got, "attached `ui-ux-pro-max` skill") || !strings.Contains(got, "attached `agent-browser` skill") {
		t.Fatalf("prompt extensions = %q", got)
	}

	beforeTools, beforeSkills := strings.Join(profile.ToolPolicy.Enabled, ","), strings.Join(profile.Skills, ",")
	if err := ResolveFeatures(&profile); err != nil {
		t.Fatal(err)
	}
	if strings.Join(profile.ToolPolicy.Enabled, ",") != beforeTools || strings.Join(profile.Skills, ",") != beforeSkills {
		t.Fatal("feature resolution must be idempotent")
	}
}

func TestLoadProductManifestAcceptsScalarAndConfiguredFeatures(t *testing.T) {
	files := fstest.MapFS{
		"product.yaml": {Data: []byte(`schema_version: 2
dependencies: {}
prompt: {file: prompt.md}
profile:
  id: test-product
  name: Test
  version: 1
  features:
    - files
    - id: schedules
      options: {mode: message_only}
  tool_policy: {mode: allowlist}
  runtime: {transport: auto}
  built_in: true
`)},
		"prompt.md": {Data: []byte("Base product prompt")},
	}
	manifest, err := LoadProductManifest(files, "product.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Profile.ResolvedFeatures) != 2 || manifest.Profile.ResolvedFeatures[1].Options["mode"] != "message_only" {
		t.Fatalf("resolved features = %+v", manifest.Profile.ResolvedFeatures)
	}
	if !containsString(manifest.Profile.ToolPolicy.Enabled, "create_project_schedule") {
		t.Fatalf("schedule tools were not projected: %v", manifest.Profile.ToolPolicy.Enabled)
	}
}

func TestResolveFeaturesRejectsUnknownDuplicateAndDisabledDependency(t *testing.T) {
	disabled := false
	cases := []Profile{
		{Features: []FeatureBinding{{ID: "not-real"}}},
		{Features: []FeatureBinding{{ID: "files"}, {ID: "files"}}},
		{Features: []FeatureBinding{{ID: "files", Enabled: &disabled}, {ID: "dashboard"}}},
	}
	for i := range cases {
		if err := ResolveFeatures(&cases[i]); err == nil {
			t.Fatalf("case %d should fail", i)
		}
	}
}

func TestTriggersReuseSchedulesAndAddProductTools(t *testing.T) {
	profile := Profile{ToolPolicy: ToolPolicy{Mode: ToolPolicyModeAllowlist}, Features: []FeatureBinding{{ID: "triggers"}}}
	if err := ResolveFeatures(&profile); err != nil {
		t.Fatal(err)
	}
	if len(profile.ResolvedFeatures) != 2 || profile.ResolvedFeatures[0].ID != "schedules" || profile.ResolvedFeatures[1].ID != "triggers" {
		t.Fatalf("resolved features = %+v", profile.ResolvedFeatures)
	}
	for _, tool := range []string{"create_project_schedule", "create_project_trigger", "delete_project_trigger"} {
		if !containsString(profile.ToolPolicy.Enabled, tool) {
			t.Fatalf("missing %s in %v", tool, profile.ToolPolicy.Enabled)
		}
	}
}

func TestBotsProjectSharedGmailTools(t *testing.T) {
	profile := Profile{ToolPolicy: ToolPolicy{Mode: ToolPolicyModeAllowlist}, Features: []FeatureBinding{{ID: "bots"}}}
	if err := ResolveFeatures(&profile); err != nil {
		t.Fatal(err)
	}
	for _, tool := range []string{"google_workspace_cli", "list_gmail_connections", "update_gmail_connection_grants"} {
		if !containsString(profile.ToolPolicy.Enabled, tool) {
			t.Fatalf("bots feature omitted %s: %v", tool, profile.ToolPolicy.Enabled)
		}
	}
	for _, skill := range []string{"work-schedules-and-bots"} {
		if !containsString(profile.Skills, skill) {
			t.Fatalf("bots feature omitted operating skill %q: %v", skill, profile.Skills)
		}
	}
	if got := strings.Join(FeaturePromptExtensions(profile), "\n"); !strings.Contains(got, "install_skill") || !strings.Contains(got, "https://github.com/openclaw/gogcli") {
		t.Fatalf("bots feature does not tell the agent how to install versioned gog guidance on demand: %q", got)
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestBotsFeatureControlsSlackCredentialTools(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		profile := Profile{ToolPolicy: ToolPolicy{Mode: ToolPolicyModeAllowlist}}
		if enabled {
			profile.Features = []FeatureBinding{{ID: "bots"}}
		}
		if err := ResolveFeatures(&profile); err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"configure_slack_bot", "get_slack_bot_credentials", "test_slack_bot_connection", "create_slack_bot_route"} {
			if containsString(profile.ToolPolicy.Enabled, name) != enabled {
				t.Fatalf("%s admission does not follow bots feature", name)
			}
		}
	}
}
