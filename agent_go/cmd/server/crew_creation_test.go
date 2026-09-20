package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/agentprofiles"
)

func newCrewCreationTestEnv(t *testing.T) (*ProductScheduleService, *mockWorkspaceAPI, context.Context) {
	t.Helper()
	return newCrewCreationTestEnvWithOptions(t, nil)
}

func registerWorkCrewProfile(t *testing.T, registry *agentprofiles.Registry) {
	t.Helper()
	if err := registry.RegisterProfile(workCrewTestProfile(nil)); err != nil {
		t.Fatal(err)
	}
}

func workCrewTestProfile(providerOptions []agentprofiles.ProviderOption) agentprofiles.Profile {
	return agentprofiles.Profile{
		ID: "work", Name: "Work", Version: 2, SystemPromptTemplate: "hi", BuiltIn: true, Product: "work",
		Runtime: agentprofiles.RuntimePolicy{
			Transport:       "auto",
			Conversation:    agentprofiles.ConversationPolicy{Mode: agentprofiles.ConversationModeKeyed, KeyType: agentprofiles.ConversationKeyTypeProject},
			Workspace:       agentprofiles.WorkspacePolicy{Mode: agentprofiles.WorkspaceModeProject, Root: "Chats", ProjectsRoot: "Chats/Work/projects"},
			ProviderOptions: providerOptions,
		},
		Features: []agentprofiles.FeatureBinding{{ID: "triggers"}},
		UIPanels: agentprofiles.UIPanels{Schedules: true},
	}
}

func newCrewCreationTestEnvWithOptions(t *testing.T, providerOptions []agentprofiles.ProviderOption) (*ProductScheduleService, *mockWorkspaceAPI, context.Context) {
	t.Helper()
	t.Setenv("MULTI_USER_MODE", "true")
	withMemoryUserDirectory(t, `{"users":[{"id":"owner","username":"owner","can_create":true}]}`)
	registry := agentprofiles.NewRegistry()
	profile := workCrewTestProfile(providerOptions)
	if err := registry.RegisterProfile(profile); err != nil {
		t.Fatal(err)
	}
	svc := NewProductScheduleService(nil, registry)
	manifest := NewWorkflowManifest("Builder pipeline")
	manifest.CreatedBy = "owner"
	manifest.Access = &WorkflowAccess{Owners: []string{"owner"}}
	manifestRaw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	mock := &mockWorkspaceAPI{files: map[string]string{manifestPath("Workflow/build"): string(manifestRaw)}}
	server := httptest.NewServer(mock)
	t.Cleanup(server.Close)
	t.Setenv("WORKSPACE_API_URL", server.URL)
	ctx := context.WithValue(context.Background(), UserContextKey, &UserClaims{UserID: "owner"})
	return svc, mock, ctx
}

func TestCreateCrewProjectWritesUILayout(t *testing.T) {
	svc, mock, ctx := newCrewCreationTestEnv(t)
	created, err := svc.CreateCrewProject(ctx, CreateCrewRequest{
		UserID: "owner", WorkflowPath: "Workflow/build", Title: "Release Reviewer",
		Description: "Reviews release PRs", Purpose: "Own release quality",
		StepInstruction: "Review the release.", IdempotencyKey: "proposal-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.CrewID == "" || created.SessionID != "work:project:"+created.CrewID {
		t.Fatalf("crew identity = %+v", created)
	}
	if !strings.HasPrefix(created.WorkspacePath, "_users/owner/Chats/Work/projects/release-reviewer-") {
		t.Fatalf("workspace = %q", created.WorkspacePath)
	}
	productRaw, ok := mock.files[created.WorkspacePath+"/product.json"]
	if !ok {
		t.Fatal("product.json was not written")
	}
	var product map[string]interface{}
	if err := json.Unmarshal([]byte(productRaw), &product); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"schema_version", "product", "id", "title", "description", "session_id", "created_at", "updated_at", "identity"} {
		if _, ok := product[key]; !ok {
			t.Fatalf("product.json missing %q: %v", key, product)
		}
	}
	if product["product"] != "work" || product["id"] != created.CrewID || product["title"] != "Release Reviewer" {
		t.Fatalf("product.json = %v", product)
	}
	runtimeRaw, ok := mock.files[created.WorkspacePath+"/workflow.json"]
	if !ok {
		t.Fatal("workflow.json was not written")
	}
	var runtime map[string]interface{}
	if err := json.Unmarshal([]byte(runtimeRaw), &runtime); err != nil {
		t.Fatal(err)
	}
	caps, _ := runtime["capabilities"].(map[string]interface{})
	// The wiring trigger save round-trips the manifest through the Go
	// struct, so empty plain-slice selections serialize as absent (the same
	// as any trigger save); populated selections and scalar defaults
	// survive, and pointer-held secret lists keep their explicit empties.
	if caps["browser_mode"] != "auto" {
		t.Fatalf("browser_mode = %v, want auto", caps["browser_mode"])
	}
	for _, key := range []string{"selected_secrets", "selected_global_secret_names"} {
		raw, _ := caps[key].([]interface{})
		if len(raw) != 0 {
			t.Fatalf("%s = %v, want explicit empty", key, raw)
		}
	}
	for _, key := range []string{"selected_servers", "selected_skills", "selected_tools", "use_code_execution_mode"} {
		if raw, ok := caps[key]; ok && fmt.Sprint(raw) != "[]" && fmt.Sprint(raw) != "false" && fmt.Sprint(raw) != "" {
			t.Fatalf("%s = %v, want absent or empty", key, raw)
		}
	}
	contexts, _ := runtime["workflow_context_paths"].([]interface{})
	if len(contexts) != 1 || contexts[0] != "Workflow/build" {
		t.Fatalf("workflow_context_paths = %v, want the creating workflow", contexts)
	}
	if !mock.hasFolder(created.WorkspacePath + "/code") {
		t.Fatal("code/ folder was not created")
	}
}

func TestCreateCrewProjectIsIdempotent(t *testing.T) {
	svc, mock, ctx := newCrewCreationTestEnv(t)
	req := CreateCrewRequest{
		UserID: "owner", WorkflowPath: "Workflow/build", Title: "Release Reviewer",
		Purpose: "Own release quality", StepInstruction: "Review the release.",
		IdempotencyKey: "proposal-1",
	}
	first, err := svc.CreateCrewProject(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if first.Duplicate {
		t.Fatal("first creation reported duplicate")
	}
	filesAfterFirst := len(mock.files)
	second, err := svc.CreateCrewProject(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if !second.Duplicate || second.CrewID != first.CrewID || second.WorkspacePath != first.WorkspacePath {
		t.Fatalf("retry = %+v, want the original crew %+v", second, first)
	}
	if len(mock.files) != filesAfterFirst {
		t.Fatal("retry created additional files")
	}
	// A new key mints a distinct crew; it needs its own title because
	// attachment aliases are unique per workflow.
	third, err := svc.CreateCrewProject(ctx, CreateCrewRequest{
		UserID: "owner", WorkflowPath: "Workflow/build", Title: "Release Reviewer Two",
		Purpose: "Own release quality", StepInstruction: "Review the release.",
		IdempotencyKey: "proposal-2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if third.Duplicate || third.CrewID == first.CrewID || third.AttachmentAlias == first.AttachmentAlias {
		t.Fatalf("new key = %+v, want a distinct crew", third)
	}
}

func TestCreateCrewProjectValidatesInput(t *testing.T) {
	svc, _, ctx := newCrewCreationTestEnv(t)
	valid := CreateCrewRequest{
		UserID: "owner", WorkflowPath: "Workflow/build", Title: "Release Reviewer",
		Purpose: "Own release quality", StepInstruction: "Review the release.",
		IdempotencyKey: "proposal-1",
	}
	for name, mutate := range map[string]func(*CreateCrewRequest){
		"empty title":              func(r *CreateCrewRequest) { r.Title = " " },
		"long title":               func(r *CreateCrewRequest) { r.Title = strings.Repeat("a", 61) },
		"long description":         func(r *CreateCrewRequest) { r.Description = strings.Repeat("a", 2001) },
		"long icon":                func(r *CreateCrewRequest) { r.Icon = strings.Repeat("a", 9) },
		"empty key":                func(r *CreateCrewRequest) { r.IdempotencyKey = "" },
		"bad workflow":             func(r *CreateCrewRequest) { r.WorkflowPath = "Chats/other" },
		"other profile":            func(r *CreateCrewRequest) { r.ProfileID = "crewx" },
		"missing step instruction": func(r *CreateCrewRequest) { r.StepInstruction = " " },
		"missing trigger text": func(r *CreateCrewRequest) {
			r.Purpose, r.Instructions, r.TriggerMessage = "", "", ""
		},
	} {
		req := valid
		mutate(&req)
		if _, err := svc.CreateCrewProject(ctx, req); err == nil {
			t.Fatalf("%s: expected rejection", name)
		}
	}
	if _, err := svc.CreateCrewProject(ctx, CreateCrewRequest{
		UserID: "owner", WorkflowPath: "Workflow/missing", Title: "Ghost",
		IdempotencyKey: "proposal-1",
	}); err == nil {
		t.Fatal("missing workflow: expected rejection")
	}
}

func TestCreateCrewProjectAvoidsOccupiedPath(t *testing.T) {
	svc, mock, ctx := newCrewCreationTestEnv(t)
	slug := slugifyCrewTitle("Release Reviewer")
	occupied := "_users/owner/Chats/Work/projects/" + slug + "-" + crewCreationSuffix("proposal-1")
	mock.files[occupied+"/product.json"] = `{"schema_version":1,"product":"work","id":"someone-else","title":"Other","session_id":"work:project:someone-else"}`
	created, err := svc.CreateCrewProject(ctx, CreateCrewRequest{
		UserID: "owner", WorkflowPath: "Workflow/build", Title: "Release Reviewer",
		Purpose: "Own release quality", StepInstruction: "Review the release.",
		IdempotencyKey: "proposal-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.Duplicate || created.WorkspacePath == occupied || created.CrewID == "someone-else" {
		t.Fatalf("collision = %+v, want a fresh crew elsewhere", created)
	}
	if _, err := uuid.Parse(created.CrewID); err != nil {
		t.Fatalf("fallback id = %q, want a uuid: %v", created.CrewID, err)
	}
}

func TestCreateCrewProjectSeedsStarterAndSelections(t *testing.T) {
	svc, mock, ctx := newCrewCreationTestEnv(t)
	mock.files["skills/reviewer/SKILL.md"] = "---\nname: reviewer\ndescription: test reviewer\n---\n# Reviewer\n"
	created, err := svc.CreateCrewProject(ctx, CreateCrewRequest{
		UserID: "owner", WorkflowPath: "Workflow/build", Title: "Release Reviewer",
		Purpose: "Own release quality", Instructions: "Check the changelog first.",
		Skills: []string{"reviewer"}, Servers: []string{"github"},
		Secrets: []string{"GH_TOKEN"}, GlobalSecrets: []string{"SHARED_GH"},
		StepInstruction: "Review the release.", IdempotencyKey: "proposal-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	memory, ok := mock.files[created.WorkspacePath+"/MEMORY.md"]
	if !ok {
		t.Fatal("MEMORY.md was not seeded")
	}
	for _, want := range []string{"# Release Reviewer", "Crew brief", "Workflow/build", "Own release quality", "Check the changelog first."} {
		if !strings.Contains(memory, want) {
			t.Fatalf("MEMORY.md missing %q:\n%s", want, memory)
		}
	}
	var runtime map[string]interface{}
	if err := json.Unmarshal([]byte(mock.files[created.WorkspacePath+"/workflow.json"]), &runtime); err != nil {
		t.Fatal(err)
	}
	caps, _ := runtime["capabilities"].(map[string]interface{})
	assertStringSet := func(key string, want ...string) {
		t.Helper()
		raw, _ := caps[key].([]interface{})
		if len(raw) != len(want) {
			t.Fatalf("%s = %v, want %v", key, raw, want)
		}
		for i, w := range want {
			if raw[i] != w {
				t.Fatalf("%s = %v, want %v", key, raw, want)
			}
		}
	}
	assertStringSet("selected_skills", "reviewer")
	assertStringSet("selected_servers", "github")
	assertStringSet("selected_secrets", "GH_TOKEN")
	assertStringSet("selected_global_secret_names", "SHARED_GH")
	// Re-entry converges without duplicating selections.
	if _, err := svc.CreateCrewProject(ctx, CreateCrewRequest{
		UserID: "owner", WorkflowPath: "Workflow/build", Title: "Release Reviewer",
		Purpose: "Own release quality", Instructions: "Check the changelog first.",
		Skills: []string{"reviewer"}, Servers: []string{"github"},
		Secrets: []string{"GH_TOKEN"}, GlobalSecrets: []string{"SHARED_GH"},
		StepInstruction: "Review the release.", IdempotencyKey: "proposal-1",
	}); err != nil {
		t.Fatal(err)
	}
	runtime = nil
	if err := json.Unmarshal([]byte(mock.files[created.WorkspacePath+"/workflow.json"]), &runtime); err != nil {
		t.Fatal(err)
	}
	caps, _ = runtime["capabilities"].(map[string]interface{})
	assertStringSet("selected_skills", "reviewer")
	assertStringSet("selected_servers", "github")
}

func TestCreateCrewProjectSkipsEmptyStarter(t *testing.T) {
	svc, mock, ctx := newCrewCreationTestEnv(t)
	created, err := svc.CreateCrewProject(ctx, CreateCrewRequest{
		UserID: "owner", WorkflowPath: "Workflow/build", Title: "Bare Crew",
		TriggerMessage: "Review releases.", StepInstruction: "Review the release.",
		IdempotencyKey: "proposal-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := mock.files[created.WorkspacePath+"/MEMORY.md"]; ok {
		t.Fatal("MEMORY.md seeded without a brief")
	}
}

func TestCreateCrewProjectRejectsUnknownSkillAndBadNames(t *testing.T) {
	svc, _, ctx := newCrewCreationTestEnv(t)
	if _, err := svc.CreateCrewProject(ctx, CreateCrewRequest{
		UserID: "owner", WorkflowPath: "Workflow/build", Title: "Ghost Skills",
		Skills: []string{"ghost"}, IdempotencyKey: "proposal-1",
	}); err == nil || !strings.Contains(err.Error(), "not installed") {
		t.Fatalf("unknown skill err = %v, want install-first failure", err)
	}
	for name, req := range map[string]CreateCrewRequest{
		"secret path":   {UserID: "owner", WorkflowPath: "Workflow/build", Title: "T", Secrets: []string{"a/b"}, IdempotencyKey: "k"},
		"server dotdot": {UserID: "owner", WorkflowPath: "Workflow/build", Title: "T", Servers: []string{".."}, IdempotencyKey: "k"},
		"empty global":  {UserID: "owner", WorkflowPath: "Workflow/build", Title: "T", GlobalSecrets: []string{" "}, IdempotencyKey: "k"},
	} {
		if _, err := svc.CreateCrewProject(ctx, req); err == nil {
			t.Fatalf("%s: expected rejection", name)
		}
	}
}

func TestCreateCrewProjectDefaultLLMConfig(t *testing.T) {
	svc, mock, ctx := newCrewCreationTestEnvWithOptions(t, []agentprofiles.ProviderOption{
		{ID: "codex-cli", Label: "Codex", Provider: "codex-cli", ModelID: "gpt-6", ReasoningEfforts: []string{"high"}},
		{ID: "claude-code", Label: "Claude", Provider: "claude-code", ModelID: "claude-1", Default: true, Options: map[string]interface{}{"reasoning_effort": "medium"}},
	})
	created, err := svc.CreateCrewProject(ctx, CreateCrewRequest{
		UserID: "owner", WorkflowPath: "Workflow/build", Title: "Modeled Crew",
		Purpose: "Own release quality", StepInstruction: "Review the release.",
		IdempotencyKey: "proposal-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	var runtime map[string]interface{}
	if err := json.Unmarshal([]byte(mock.files[created.WorkspacePath+"/workflow.json"]), &runtime); err != nil {
		t.Fatal(err)
	}
	caps, _ := runtime["capabilities"].(map[string]interface{})
	llm, _ := caps["llm_config"].(map[string]interface{})
	builder, _ := llm["builder_llm"].(map[string]interface{})
	if llm["mode"] != "explicit" || builder["provider"] != "claude-code" || builder["model_id"] != "claude-1" {
		t.Fatalf("llm_config = %v, want the default provider option", llm)
	}
	options, _ := builder["options"].(map[string]interface{})
	if options["reasoning_effort"] != "medium" {
		t.Fatalf("llm options = %v", options)
	}
}

func TestCreateCrewProjectWiring(t *testing.T) {
	svc, mock, ctx := newCrewCreationTestEnv(t)
	mock.files["Workflow/build/planning/plan.json"] = `{"objective":"t","steps":[]}`
	created, err := svc.CreateCrewProject(ctx, CreateCrewRequest{
		UserID: "owner", WorkflowPath: "Workflow/build", Title: "Release Reviewer",
		Purpose: "Own release quality", StepInstruction: "Review the release.",
		ContextDependencies: []string{"release-notes"}, IdempotencyKey: "proposal-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.TriggerID == "" || created.AttachmentAlias != "release-reviewer" {
		t.Fatalf("wiring = %+v", created)
	}
	triggers, err := svc.projectWebhookConfigs(ctx, "owner", "work", created.CrewID)
	if err != nil {
		t.Fatal(err)
	}
	if len(triggers) != 1 {
		t.Fatalf("triggers = %+v, want one", triggers)
	}
	trigger := triggers[0]
	if trigger.ID != created.TriggerID || !trigger.IsInternal() || !trigger.Enabled ||
		trigger.RunDestination != runDestinationIsolated || trigger.Caller == nil ||
		trigger.Caller.Type != triggerCallerWorkflow {
		t.Fatalf("trigger = %+v, want enabled isolated internal binding", trigger)
	}
	var manifest WorkflowManifest
	if err := json.Unmarshal([]byte(mock.files[manifestPath("Workflow/build")]), &manifest); err != nil {
		t.Fatal(err)
	}
	if trigger.Caller.ID != manifest.ID {
		t.Fatalf("trigger caller = %+v, want workflow %q", trigger.Caller, manifest.ID)
	}
	if len(manifest.CrewAttachments) != 1 {
		t.Fatalf("attachments = %+v, want one", manifest.CrewAttachments)
	}
	attachment := manifest.CrewAttachments[0]
	if attachment.Alias != "release-reviewer" || attachment.CrewProjectID != created.CrewID ||
		attachment.CrewWorkspacePath != created.WorkspacePath {
		t.Fatalf("attachment = %+v", attachment)
	}
	step := created.Step
	if step.StepID != "crew-release-reviewer" || step.CrewProjectID != created.CrewID ||
		step.TriggerID != created.TriggerID || step.Instruction != "Review the release." ||
		len(step.ContextDependencies) != 1 || step.ContextDependencies[0] != "release-notes" {
		t.Fatalf("step config = %+v", step)
	}
	// The created wiring satisfies the hardened run preflight: the stored
	// root equals the freshly authorized binding.
	if err := preflightCrewSteps(ctx, svc, "owner", "Workflow/build"); err != nil {
		t.Fatalf("preflight on created wiring: %v", err)
	}
}

func TestCreateCrewProjectWiringIdempotent(t *testing.T) {
	svc, mock, ctx := newCrewCreationTestEnv(t)
	req := CreateCrewRequest{
		UserID: "owner", WorkflowPath: "Workflow/build", Title: "Release Reviewer",
		Purpose: "Own release quality", StepInstruction: "Review the release.",
		IdempotencyKey: "proposal-1",
	}
	first, err := svc.CreateCrewProject(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.CreateCrewProject(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if !second.Duplicate || second.TriggerID != first.TriggerID || second.Step.StepID != first.Step.StepID {
		t.Fatalf("retry = %+v, want adopted wiring %+v", second, first)
	}
	triggers, err := svc.projectWebhookConfigs(ctx, "owner", "work", first.CrewID)
	if err != nil {
		t.Fatal(err)
	}
	if len(triggers) != 1 {
		t.Fatalf("triggers = %d, want 1", len(triggers))
	}
	var manifest WorkflowManifest
	if err := json.Unmarshal([]byte(mock.files[manifestPath("Workflow/build")]), &manifest); err != nil {
		t.Fatal(err)
	}
	if len(manifest.CrewAttachments) != 1 {
		t.Fatalf("attachments = %d, want 1", len(manifest.CrewAttachments))
	}
}

func TestCrewManifestRewritePreservesContextPaths(t *testing.T) {
	svc, mock, ctx := newCrewCreationTestEnv(t)
	created, err := svc.CreateCrewProject(ctx, CreateCrewRequest{
		UserID: "owner", WorkflowPath: "Workflow/build", Title: "Release Reviewer",
		Purpose: "Own release quality", StepInstruction: "Review the release.",
		IdempotencyKey: "proposal-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	// A later trigger save rewrites the runtime manifest through the Go
	// struct; the seeded context paths and UI-written capability keys must
	// survive the round-trip.
	if _, _, err := svc.saveProductWebhookConfig(ctx, "owner", productWebhookRequest{
		ProfileID: "work", ProjectID: created.CrewID, Name: "Second trigger",
		Message: "Another entrypoint.", Enabled: false,
	}, ""); err != nil {
		t.Fatal(err)
	}
	var runtime map[string]interface{}
	if err := json.Unmarshal([]byte(mock.files[created.WorkspacePath+"/workflow.json"]), &runtime); err != nil {
		t.Fatal(err)
	}
	contexts, _ := runtime["workflow_context_paths"].([]interface{})
	if len(contexts) != 1 || contexts[0] != "Workflow/build" {
		t.Fatalf("workflow_context_paths = %v after rewrite", contexts)
	}
	caps, _ := runtime["capabilities"].(map[string]interface{})
	if caps["browser_mode"] != "auto" {
		t.Fatalf("browser_mode = %v after rewrite", caps["browser_mode"])
	}
	triggers, err := svc.projectWebhookConfigs(ctx, "owner", "work", created.CrewID)
	if err != nil {
		t.Fatal(err)
	}
	if len(triggers) != 2 {
		t.Fatalf("triggers = %d, want both", len(triggers))
	}
}

func TestCreateCrewProjectReservedAlias(t *testing.T) {
	svc, _, ctx := newCrewCreationTestEnv(t)
	created, err := svc.CreateCrewProject(ctx, CreateCrewRequest{
		UserID: "owner", WorkflowPath: "Workflow/build", Title: "Planning",
		Purpose: "Plan releases", StepInstruction: "Plan the release.",
		IdempotencyKey: "proposal-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.AttachmentAlias != "planning-crew" {
		t.Fatalf("alias = %q, want the reserved word suffixed", created.AttachmentAlias)
	}
}

func TestCreateCrewProjectResolves(t *testing.T) {
	svc, _, ctx := newCrewCreationTestEnv(t)
	created, err := svc.CreateCrewProject(ctx, CreateCrewRequest{
		UserID: "owner", WorkflowPath: "Workflow/build", Title: "Release Reviewer",
		Purpose: "Own release quality", StepInstruction: "Review the release.",
		IdempotencyKey: "proposal-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	profile, binding, manifest, err := svc.projectManifest(ctx, "owner", "work", created.CrewID)
	if err != nil {
		t.Fatalf("created crew does not resolve: %v", err)
	}
	if binding.WorkspacePath != created.WorkspacePath || manifest.Title != "Release Reviewer" {
		t.Fatalf("binding = %+v manifest = %+v", binding, manifest)
	}
	if len(binding.ProjectWorkflowContextPaths) != 1 || binding.ProjectWorkflowContextPaths[0] != "Workflow/build" {
		t.Fatalf("context paths = %v", binding.ProjectWorkflowContextPaths)
	}
	_ = profile
}
