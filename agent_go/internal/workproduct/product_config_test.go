package workproduct

import (
	"strings"
	"testing"

	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/agentprofiles"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/skills"
)

func TestWorkManifestDeclaresProjectScopeAndCodingAllowlist(t *testing.T) {
	manifest, err := WorkManifest()
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Profile.ID != "work" {
		t.Fatalf("unexpected profile id: %q", manifest.Profile.ID)
	}
	if len(manifest.Dependencies.Skills) != 0 {
		t.Fatalf("Crew must not install external skills merely because a project opens: %+v", manifest.Dependencies.Skills)
	}
	// Project, not global -- same reasoning as Finance/Dominion: global
	// scope makes provider_options non-authoritative and skips this
	// profile's own prompt.file in favor of the dynamic delegation
	// prompt. Crew's prompt (plain coding work, no workflow vocabulary)
	// must actually reach the model.
	if manifest.Profile.Scope != agentprofiles.ProfileScopeProject {
		t.Fatalf("work must declare scope: project, got %q", manifest.Profile.Scope)
	}
	for _, want := range []string{"agent-browser", "code-reviewer", "work-integrations", "work-workflow-files", "work-skills", "work-schedules-and-bots", "work-dashboard", "ui-ux-pro-max", "background-work"} {
		if !contains(manifest.Profile.Skills, want) {
			t.Fatalf("work feature bundles omitted skill %q: %v", want, manifest.Profile.Skills)
		}
	}
	if len(manifest.Profile.ResolvedFeatures) == 0 {
		t.Fatal("Crew must expose resolved feature bundles to the shared product UI")
	}
	for _, name := range manifest.Profile.Skills {
		if name == "skill-creator" {
			t.Fatal("skill-creator must be selected on demand; attaching it to every Crew chat replaces Crew's general-purpose identity with Skill Builder mode")
		}
	}
	// Runtime choice reuses AgentWorks' complete coding-agent provider set and
	// maps each choice to its globally installed CLI.
	if manifest.Profile.Runtime.Provider != "claude-code" || manifest.Profile.Runtime.ModelID != "claude-sonnet-5" {
		t.Fatalf("work must report its default provider/model to the shared composer, got provider=%q model_id=%q", manifest.Profile.Runtime.Provider, manifest.Profile.Runtime.ModelID)
	}
	options := manifest.Profile.Runtime.ProviderOptions
	wantProviders := []string{"claude-code", "codex-cli", "cursor-cli", "pi-cli", "muse-cli"}
	if len(options) != len(wantProviders) {
		t.Fatalf("work runtime options = %+v, want all AgentWorks coding CLIs", options)
	}
	for i, want := range wantProviders {
		if options[i].Provider != want {
			t.Fatalf("work runtime option %d provider = %q, want %q", i, options[i].Provider, want)
		}
	}
	if !options[0].Default {
		t.Fatal("Claude Code must remain Crew's default coding CLI")
	}
	// Crew uses the shared AgentWorks execution policy instead of defining a
	// product-specific transport policy.
	if manifest.Profile.Runtime.Transport != "auto" {
		t.Fatalf("work must use the shared runtime transport policy, got transport=%q", manifest.Profile.Runtime.Transport)
	}
	if manifest.Profile.Runtime.Workspace.ProjectsRoot != "Chats/Work/projects" {
		t.Fatalf("work projects root = %q, want Chats/Work/projects", manifest.Profile.Runtime.Workspace.ProjectsRoot)
	}
	if manifest.Profile.Runtime.Workspace.Mode != agentprofiles.WorkspaceModeProject || manifest.Profile.Runtime.Conversation.Mode != agentprofiles.ConversationModeKeyed {
		t.Fatalf("work must use keyed session conversations: workspace=%+v conversation=%+v", manifest.Profile.Runtime.Workspace, manifest.Profile.Runtime.Conversation)
	}
	if !strings.EqualFold(manifest.Profile.Runtime.AgentTools.Mode, "mcp_only") {
		t.Fatalf("work must route coding operations through governed MCP tools, got agent_tools.mode=%q", manifest.Profile.Runtime.AgentTools.Mode)
	}
	if !manifest.Profile.Runtime.Sandbox.IsStrict() || manifest.Profile.Runtime.Sandbox.ChatHistory != agentprofiles.SandboxChatHistoryNone {
		t.Fatalf("work MCP tools must be strictly workspace-confined: %+v", manifest.Profile.Runtime.Sandbox)
	}
	if !manifest.Profile.ToolPolicy.IsAllowlist() {
		t.Fatal("work must declare tool_policy.mode: allowlist -- fail-open would silently reach workflow/schedule/pulse tools")
	}
	// Crew reuses platform coding tools plus its deliberately small,
	// project-scoped schedule surface. Workflow routes and execution remain out.
	wantEnabled := map[string]bool{
		"get_file_link":                       false,
		"get_report_link":                     false,
		"diff_patch_workspace_file":           false,
		"execute_shell_command":               false,
		"agent_browser":                       false,
		"read_image":                          false,
		"search_web_llm":                      false,
		"list_secrets":                        false,
		"set_workflow_secret":                 false,
		"delete_workflow_secret":              false,
		"set_user_secret":                     false,
		"delete_user_secret":                  false,
		"list_skills":                         false,
		"search_skills":                       false,
		"install_skill":                       false,
		"import_skill":                        false,
		"uninstall_skill":                     false,
		"list_mcp_servers":                    false,
		"search_mcp_catalog":                  false,
		"install_mcp_server":                  false,
		"add_mcp_server":                      false,
		"remove_mcp_server":                   false,
		"get_mcp_server_logs":                 false,
		"trigger_mcp_discovery":               false,
		"update_project_mcp_server_selection": false,
		"list_work_folders":                   false,
		"attach_work_folder":                  false,
		"detach_work_folder":                  false,
		"list_accessible_workflows":           false,
		"attach_workflow_reference":           false,
		"detach_workflow_reference":           false,
		"list_project_schedules":              false,
		"create_project_schedule":             false,
		"update_project_schedule":             false,
		"delete_project_schedule":             false,
		"trigger_project_schedule":            false,
		"list_project_triggers":               false,
		"create_project_trigger":              false,
		"update_project_trigger":              false,
		"delete_project_trigger":              false,
		"google_workspace_cli":                false,
		"list_gmail_connections":              false,
		"update_gmail_connection_grants":      false,
		"query_workflow_db":                   false,
		"mutate_workflow_db":                  false,
		"apply_workflow_db_migration":         false,
		"create_workflow_database_snapshot":   false,
		"validate_report_html":                false,
		"preview_report":                      false,
		"run_in_background":                   false,
		"query_agent":                         false,
		"list_agents":                         false,
		"terminate_agent":                     false,
		"open_workspace_view":                 false,
		"refresh_workspace_view":              false,
		"list_ui_capabilities":                false,
		"get_ui_state":                        false,
		"perform_ui_action":                   false,
		"get_ui_action_result":                false,
	}
	for _, name := range manifest.Profile.ToolPolicy.Enabled {
		if _, expected := wantEnabled[name]; !expected {
			t.Fatalf("unexpected tool in allowlist: %q -- work chat is plain coding tools only", name)
		}
		wantEnabled[name] = true
	}
	for name, found := range wantEnabled {
		if !found {
			t.Fatalf("expected tool_policy.enabled to include %q", name)
		}
	}
	if len(manifest.Profile.Tools) != 2 || manifest.Profile.Tools[0].ID != "work.set-identity" || manifest.Profile.Tools[1].ID != "work.custom-commands" {
		t.Fatalf("work must expose its identity and custom-command tools, got %+v", manifest.Profile.Tools)
	}
	identityInteraction := manifest.Profile.Tools[0].Interaction
	if identityInteraction == nil || identityInteraction.Kind != "identity_updated" || identityInteraction.Render != "product.refresh" {
		t.Fatalf("work identity interaction must be declared in product.yaml, got %+v", identityInteraction)
	}
	if !manifest.Profile.UIPanels.Schedules {
		t.Fatal("Crew must expose its project schedule panel")
	}
	caps := manifest.Profile.Runtime.Capabilities
	if caps.WorkflowExecution != agentprofiles.CapabilityDisabled {
		t.Fatalf("work must declare capabilities.workflow_execution: disabled, got %q", caps.WorkflowExecution)
	}
	if caps.RawTerminal == agentprofiles.CapabilityDisabled {
		t.Fatalf("work must expose the selected coding CLI terminal, got %q", caps.RawTerminal)
	}
	if caps.MCPSelection == agentprofiles.CapabilityDisabled || caps.SkillSelection == agentprofiles.CapabilityDisabled {
		t.Fatalf("work must expose shared MCP and skill selection, got %+v", caps)
	}
	if caps.WorkflowReferences == agentprofiles.CapabilityDisabled {
		t.Fatalf("work must expose shared read-only workflow references, got %+v", caps)
	}
	if manifest.UI.Surface != "work" {
		t.Fatalf("unexpected ui.surface: %q", manifest.UI.Surface)
	}
	if len(manifest.Workflows.Enabled) != 0 {
		t.Fatalf("work has no fixed pipelines; workflows.enabled should stay empty: %v", manifest.Workflows.Enabled)
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestWorkPlatformSkillsRegisterAndLoad(t *testing.T) {
	if err := RegisterProductSkills(); err != nil {
		t.Fatalf("RegisterProductSkills: %v", err)
	}
	checks := map[string][]string{
		"work-mcp":                {"list_mcp_servers", "Setup > MCP", "platform-level", "trigger_mcp_discovery", "update_project_mcp_server_selection", "next user message"},
		"work-integrations":       {"set_workflow_secret", "available to shell", "do not ask the user to start", "list_work_folders", "Setup > Models"},
		"work-workflow-files":     {"list_accessible_workflows", "WORK_FOLDER_<ALIAS>", "workflow.json", "knowledgebase/", "learnings/", "db/db.sqlite", "db/reports/", "runs/run_index.json", "sqlite3 -readonly", "get_file_link", "get_report_link", "same signed-in Crew account"},
		"work-skills":             {"list_skills", "search_skills", "skills/custom/<skill-name>/SKILL.md", "same topic", "independently reusable topics", "catch-all", "150 lines or fewer", "references/", "scripts/", "skill authoring is a capability", "Setup > Skills"},
		"work-schedules-and-bots": {"list_project_schedules", "five-field cron", "list_project_triggers", "Project webhook triggers", "Setup > Bots", "Slack", "WhatsApp", "list_gmail_connections", "google_workspace_cli", "gmail.readonly"},
		"work-dashboard":          {"db/reports/index.html", "window.report.sendChatMessage", "query_workflow_db", "validate_report_html", "get_report_link"},
		"background-work":         {"run_in_background", "[AUTO-NOTIFICATION]", "query_agent"},
	}
	for name, required := range checks {
		if !skills.IsBuiltinSkill(name) {
			t.Fatalf("%s did not register as a built-in Crew skill", name)
		}
		attached := skills.LoadAttachable("", []string{name})
		if len(attached) != 1 {
			t.Fatalf("LoadAttachable(%s) = %v, want one skill", name, attached)
		}
		for _, text := range required {
			if !strings.Contains(attached[0].Content, text) {
				t.Fatalf("%s skill is missing %q", name, text)
			}
		}
	}
}

func TestBuiltinAgentProfileValidatesAndResolvesProjectScope(t *testing.T) {
	profile := BuiltinAgentProfile()
	if err := agentprofiles.Validate(profile); err != nil {
		t.Fatalf("built-in profile failed validation: %v", err)
	}
	if profile.EffectiveScope() != agentprofiles.ProfileScopeProject {
		t.Fatalf("EffectiveScope() = %q, want project", profile.EffectiveScope())
	}
	if profile.SystemPromptTemplate == "" {
		t.Fatal("expected a non-empty rendered system prompt template")
	}
	if !profile.BuiltIn {
		t.Fatal("expected BuiltIn to be true")
	}
}

func TestBuiltinAgentProfilesReturnsExactlyOneVersion(t *testing.T) {
	profiles := BuiltinAgentProfiles()
	if len(profiles) != 1 {
		t.Fatalf("expected exactly one built-in profile, got %d", len(profiles))
	}
	if profiles[0].Version != 1 {
		t.Fatalf("expected version 1, got %d", profiles[0].Version)
	}
}

func TestRenderPromptSucceedsAgainstAPromptContext(t *testing.T) {
	profile := BuiltinAgentProfile()
	rendered, err := agentprofiles.RenderPrompt(profile, agentprofiles.PromptContext{
		ProjectTitle:  "Crew",
		LocalDateTime: "Monday, 1 January 2026 at 9:00 AM UTC",
	})
	if err != nil {
		t.Fatalf("RenderPrompt failed: %v", err)
	}
	if rendered == "" {
		t.Fatal("expected non-empty rendered prompt")
	}
	for _, required := range []string{
		"general-purpose, chat-first agent",
		"Coding is a first-class capability",
		"not the only kind of work",
		"questions, research, analysis, writing, planning",
		"## Coding rules",
		"platform skills for their precise setup",
		"message schedules, project-chat bots",
		"workflow selected with `#` is",
		"general-purpose",
		"tasks, notes, plans, status, research",
		"## Persistent project memory",
		"MEMORY.md",
		"without waiting for the user to repeat a request",
		"Never turn unverified research",
		"one durable memory store",
		"Do not create or",
		"update a skill as a side effect of learning something",
		"mention `MEMORY.md`",
		`"remember this"`,
		`"forget this"`,
		"reverse chronological",
		"## YYYY-MM-DD — Topic",
		"place the newest entry first",
		"remove the stale entry",
	} {
		if !strings.Contains(rendered, required) {
			t.Fatalf("rendered Crew prompt is missing %q", required)
		}
	}
}

func TestRegisterAgentProfileRuntimeRegistersIdentityTool(t *testing.T) {
	registry := agentprofiles.NewRegistry()
	if err := RegisterAgentProfileRuntime(registry, "http://127.0.0.1:0"); err != nil {
		t.Fatalf("RegisterAgentProfileRuntime failed: %v", err)
	}
	tool, err := registry.BuildTool(agentprofiles.ToolBinding{ID: "work.set-identity"}, agentprofiles.ToolRuntimeContext{
		UserID: "user-1", SessionID: "session-1", WorkspacePath: "Chats/Work/projects/demo",
	})
	if err != nil {
		t.Fatalf("BuildTool failed: %v", err)
	}
	if tool.Name != "set_work_identity" || tool.Execute == nil {
		t.Fatalf("unexpected identity tool: %+v", tool)
	}
}

func TestWorkPromptIncludesConfiguredIdentity(t *testing.T) {
	profile := BuiltinAgentProfile()
	rendered, err := agentprofiles.RenderPrompt(profile, agentprofiles.PromptContext{
		ProjectTitle: "Demo",
		Product:      map[string]string{"WORK_IDENTITY": "Name: Nova\nRole: Engineering partner"},
	})
	if err != nil {
		t.Fatalf("render Crew prompt: %v", err)
	}
	for _, required := range []string{"## Project agent identity", "changes behavior, never permissions", "Name: Nova", "Role: Engineering partner"} {
		if !strings.Contains(rendered, required) {
			t.Fatalf("rendered Crew prompt is missing %q", required)
		}
	}
}
