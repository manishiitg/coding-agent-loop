package server

import (
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"

	virtualtools "github.com/manishiitg/coding-agent-loop/agent_go/cmd/server/virtual-tools"
	"github.com/manishiitg/coding-agent-loop/agent_go/internal/workproduct"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/agentprofiles"
)

// Crew Run mode: one owner, read-only for everyone else. These tests pin
// the access rules (binding, reader detection, transcript and workflow
// privacy), the read-only tool surface, the mediated directory, and the
// proxy's cross-user block.

func TestIsCrewReaderTurn(t *testing.T) {
	ownerRoot := "_users/owner/Chats/Work/projects/alpha"
	for _, tt := range []struct {
		name   string
		req    QueryRequest
		caller string
		want   bool
	}{
		{"owner physical", QueryRequest{AgentProfileID: "work", SelectedFolder: ownerRoot}, "owner", false},
		{"reader physical", QueryRequest{AgentProfileID: "work", SelectedFolder: ownerRoot}, "reader", true},
		{"owner logical", QueryRequest{AgentProfileID: "work", SelectedFolder: "Chats/Work/projects/alpha"}, "owner", false},
		{"reader logical resolves to reader own", QueryRequest{AgentProfileID: "work", SelectedFolder: "Chats/Work/projects/alpha"}, "reader", false},
		{"landing chat", QueryRequest{AgentProfileID: "work", SelectedFolder: "Chats/Work"}, "reader", false},
		{"non-work profile", QueryRequest{AgentProfileID: "video-studio", SelectedFolder: ownerRoot}, "reader", false},
		{"no profile", QueryRequest{SelectedFolder: ownerRoot}, "reader", false},
		{"workflow folder", QueryRequest{SelectedFolder: "Workflow/acme"}, "reader", false},
	} {
		if got := isCrewReaderTurn(tt.req, tt.caller); got != tt.want {
			t.Fatalf("%s: isCrewReaderTurn = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestCrewProjectOwnershipHelpers(t *testing.T) {
	if owner, ok := crewProjectOwnerID("_users/owner/Chats/Work/projects/alpha"); !ok || owner != "owner" {
		t.Fatalf("owner = %q,%v", owner, ok)
	}
	if _, ok := crewProjectOwnerID("Chats/Work/projects/alpha"); ok {
		t.Fatal("logical path must not report an owner")
	}
	if _, ok := crewProjectOwnerID("Workflow/acme"); ok {
		t.Fatal("workflow path must not report an owner")
	}
	if !isCrewProjectPath("_users/owner/Chats/Work/projects/alpha") || !isCrewProjectPath("Chats/Work/projects/alpha") {
		t.Fatal("crew project paths not recognized")
	}
	if isCrewProjectPath("Chats/Work/projects") || isCrewProjectPath("Chats/Work") || isCrewProjectPath("Workflow/acme") {
		t.Fatal("non-project paths recognized as crew projects")
	}
	if !crewProjectOwnedByCaller("owner", "_users/owner/Chats/Work/projects/alpha") {
		t.Fatal("owner not recognized")
	}
	if crewProjectOwnedByCaller("reader", "_users/owner/Chats/Work/projects/alpha") {
		t.Fatal("reader recognized as owner")
	}
	if !crewProjectOwnedByCaller("reader", "Chats/Work/projects/alpha") {
		t.Fatal("logical path must address the caller's own tree")
	}
	read, write, blocked := crewReaderWorkspaceRoots("_users/owner/Chats/Work/projects/alpha", true)
	if len(write) != 0 || len(read) != 1 || len(blocked) != 1 || blocked[0] != read[0] {
		t.Fatalf("reader roots = %v/%v/%v", read, write, blocked)
	}
	read, write, blocked = crewReaderWorkspaceRoots("_users/owner/Chats/Work/projects/alpha", false)
	if len(read) != 1 || len(write) != 1 || len(blocked) != 0 || read[0] != write[0] {
		t.Fatalf("owner roots = %v/%v/%v", read, write, blocked)
	}
	if !crewBuilderDiskHiddenFromUser("reader", "_users/owner/Chats/Work/projects/alpha") {
		t.Fatal("owner transcripts must stay hidden from readers")
	}
	if crewBuilderDiskHiddenFromUser("owner", "_users/owner/Chats/Work/projects/alpha") {
		t.Fatal("owner must keep reading their own transcripts")
	}
	if crewBuilderDiskHiddenFromUser("reader", "Workflow/acme") {
		t.Fatal("workflow history must not be affected")
	}
}

func TestIsActiveWorkProjectWorkspaceAnyOwner(t *testing.T) {
	if !isActiveWorkProjectWorkspace("reader", "_users/owner/Chats/Work/projects/alpha") {
		t.Fatal("another owner's crew project was not recognized")
	}
	if !isActiveWorkProjectWorkspace("owner", "_users/owner/Chats/Work/projects/alpha") {
		t.Fatal("owned physical crew project was not recognized")
	}
	if !isActiveWorkProjectWorkspace("owner", "Chats/Work/projects/alpha") {
		t.Fatal("logical crew project was not recognized")
	}
	if isActiveWorkProjectWorkspace("reader", "Chats/Work/projects") {
		t.Fatal("projects root recognized as an active project")
	}
}

func TestCrewReaderSystemPrompt(t *testing.T) {
	withMemoryUserDirectory(t, `{"users":[{"id":"owner","username":"aman","can_create":true}]}`)
	prompt := crewReaderSystemPrompt("_users/owner/Chats/Work/projects/alpha")
	for _, marker := range []string{"read-only", "aman", "Mutate nothing", "belongs to the current user alone", "never to be printed"} {
		if !strings.Contains(prompt, marker) {
			t.Fatalf("reader prompt missing %q:\n%s", marker, prompt)
		}
	}
}

func TestCrewReaderDeniedToolsGate(t *testing.T) {
	gate := newProductToolGate(nil)
	gate.DenyReaderTools(crewReaderDeniedTools()...)
	for _, denied := range []string{
		"diff_patch_workspace_file", "image_gen", "image_edit",
		"attach_workflow_reference", "detach_workflow_reference",
		"create_project_schedule", "update_project_schedule", "delete_project_schedule", "trigger_project_schedule",
		"create_project_trigger", "update_project_trigger", "delete_project_trigger",
		"update_project_mcp_server_selection", "update_project_global_secret_selection", "update_project_skill_selection",
		"set_workflow_secret", "delete_workflow_secret", "manage_global_secret",
		"set_work_identity", "create_crew", "perform_ui_action",
	} {
		if gate.Admit(denied) {
			t.Fatalf("reader turn admitted mutating tool %q", denied)
		}
	}
	for _, allowed := range []string{
		"execute_shell_command", "read_image", "generate_text_llm", "search_web_llm",
		"get_file_link", "list_secrets", "list_accessible_workflows",
		"list_attached_workflows", "list_workflow_triggers", "run_workflow_trigger", "get_workflow_trigger_run",
		"list_project_schedules", "list_project_triggers",
		"human_feedback", "notify_user", "manage_custom_commands",
		"attach_work_folder", "list_work_folders",
	} {
		if !gate.Admit(allowed) {
			t.Fatalf("reader turn refused read/run tool %q", allowed)
		}
	}
}

func TestConfineSharedProjectPath(t *testing.T) {
	root := "_users/owner/Chats/Work/projects/alpha"
	full, ok := confineSharedProjectPath(root, "code/main.py")
	if !ok || full != root+"/code/main.py" {
		t.Fatalf("confine = %q,%v", full, ok)
	}
	for _, bad := range []string{"", ".", "builder/session-1-conversation.json", "builder/conversation/a.json", "db/app.sqlite", "..", "../.."} {
		if full, ok := confineSharedProjectPath(root, bad); ok {
			t.Fatalf("confine(%q) = %q, want refusal", bad, full)
		}
	}
	// Absolute inputs and .. segments collapse inside the root by
	// construction; they must never address outside it.
	for rel, want := range map[string]string{
		"../product.json": "product.json", "/product.json": "product.json",
		"code/../../MEMORY.md": "MEMORY.md", "code/../../../secret": "secret",
	} {
		if full, ok := confineSharedProjectPath(root, rel); !ok || full != root+"/"+want {
			t.Fatalf("confine(%q) = %q,%v want %q", rel, full, ok, root+"/"+want)
		}
	}
}

func TestFlattenSharedProjectFiles(t *testing.T) {
	root := "_users/owner/Chats/Work/projects/alpha"
	listing := virtualtools.WorkspaceFolderListing{{
		FilePath: root, Type: "folder",
		Children: []virtualtools.WorkspaceFolderItem{
			{FilePath: root + "/product.json", Type: "file"},
			{FilePath: root + "/builder", Type: "folder", Children: []virtualtools.WorkspaceFolderItem{
				{FilePath: root + "/builder/session-1-conversation.json", Type: "file"},
			}},
			{FilePath: root + "/db", Type: "folder", Children: []virtualtools.WorkspaceFolderItem{
				{FilePath: root + "/db/app.sqlite", Type: "file"},
			}},
			{FilePath: root + "/code", Type: "folder", Children: []virtualtools.WorkspaceFolderItem{
				{FilePath: root + "/code/main.py", Type: "file"},
			}},
		},
	}}
	entries, truncated := flattenSharedProjectFiles(root, listing)
	if truncated {
		t.Fatal("small tree reported truncated")
	}
	seen := map[string]string{}
	for _, entry := range entries {
		seen[entry.Path] = entry.Type
	}
	for _, want := range []string{"product.json", "code", "code/main.py"} {
		if _, ok := seen[want]; !ok {
			t.Fatalf("tree missing %q: %+v", want, entries)
		}
	}
	for path := range seen {
		if strings.HasPrefix(path, "builder") || strings.HasPrefix(path, "db") {
			t.Fatalf("private subtree leaked into tree: %q", path)
		}
	}
	if entries[0].Path != "code" || entries[len(entries)-1].Path != "product.json" {
		t.Fatalf("tree not sorted: %+v", entries)
	}
	if isSharedProjectBinaryContent("plain text") || !isSharedProjectBinaryContent("ab\x00cd") {
		t.Fatal("binary sniff mismatch")
	}
}

func TestWorkspaceProxyCrossUserBlock(t *testing.T) {
	proxyRequest := func(method, target, body string) *http.Request {
		var reader *strings.Reader
		if body == "" {
			reader = strings.NewReader("")
		} else {
			reader = strings.NewReader(body)
		}
		req := httptest.NewRequest(method, target, reader)
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		return req
	}
	blockedTargets := []struct {
		name string
		req  *http.Request
	}{
		{"url other user file", proxyRequest("GET", "/api/wp/api/documents/_users/owner/Chats/Work/projects/alpha/product.json", "")},
		{"url other user write", proxyRequest("PUT", "/api/wp/api/documents/_users/owner/Chats/secret.txt", `{"content":"x"}`)},
		{"url encoded", proxyRequest("GET", "/api/wp/api/documents/_users%2Fowner%2FChats/x", "")},
		{"url users root", proxyRequest("GET", "/api/wp/api/documents/_users", "")},
		{"query folder", proxyRequest("GET", "/api/wp/api/documents?folder=_users/owner/Chats&max_depth=1", "")},
		{"body folder create", proxyRequest("POST", "/api/wp/api/folders", `{"folder_path":"_users/owner/Chats/evil"}`)},
		{"body move destination", proxyRequest("POST", "/api/wp/api/documents/a/move", `{"source_path":"Chats/a","destination_path":"_users/owner/Chats/a"}`)},
		{"body guard paths", proxyRequest("POST", "/api/wp/api/execute", `{"command":"ls","folder_guard":{"write_paths":["_users/owner/Chats"]}}`)},
	}
	for _, tt := range blockedTargets {
		if blocked, reason := workspaceProxyCrossUserBlock(tt.req, "reader"); !blocked {
			t.Fatalf("%s: not blocked", tt.name)
		} else if reason == "" {
			t.Fatalf("%s: blocked without reason", tt.name)
		}
	}
	allowedTargets := []struct {
		name string
		req  *http.Request
	}{
		{"own logical file", proxyRequest("GET", "/api/wp/api/documents/Chats/Work/projects/beta/product.json", "")},
		{"own explicit file", proxyRequest("GET", "/api/wp/api/documents/_users/reader/Chats/x", "")},
		{"shared workflow file", proxyRequest("GET", "/api/wp/api/documents/Workflow/acme/workflow.json", "")},
		{"own query folder", proxyRequest("GET", "/api/wp/api/documents?folder=Chats&max_depth=1", "")},
		{"command text mentioning users is not a path", proxyRequest("POST", "/api/wp/api/execute", `{"command":"echo _users/owner/Chats"}`)},
		{"patch text mentioning users is not a path", proxyRequest("PUT", "/api/wp/api/documents/Chats/note.md", `{"content":"see _users/owner for details"}`)},
	}
	for _, tt := range allowedTargets {
		if blocked, reason := workspaceProxyCrossUserBlock(tt.req, "reader"); blocked {
			t.Fatalf("%s: blocked (%s)", tt.name, reason)
		}
	}
	// Unknown-length bodies cannot be inspected; URL and query checks
	// still apply.
	chunked := proxyRequest("POST", "/api/wp/api/folders", "")
	chunked.ContentLength = -1
	if blocked, _ := workspaceProxyCrossUserBlock(chunked, "reader"); blocked {
		t.Fatal("bodyless request blocked")
	}

	multipartRequest := func(fields map[string]string, fileName string, fileBody string) *http.Request {
		var buf strings.Builder
		writer := multipart.NewWriter(&buf)
		for name, value := range fields {
			if err := writer.WriteField(name, value); err != nil {
				t.Fatal(err)
			}
		}
		if fileName != "" {
			part, err := writer.CreateFormFile("file", fileName)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := part.Write([]byte(fileBody)); err != nil {
				t.Fatal(err)
			}
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodPost, "/api/wp/api/upload", strings.NewReader(buf.String()))
		req.Header.Set("Content-Type", writer.FormDataContentType())
		return req
	}
	if blocked, _ := workspaceProxyCrossUserBlock(multipartRequest(map[string]string{"folder_path": "_users/owner/Chats"}, "note.txt", "hi"), "reader"); !blocked {
		t.Fatal("cross-user multipart upload not blocked")
	}
	allowed := multipartRequest(map[string]string{"folder_path": "Chats/Work"}, "note.txt", "hi")
	if blocked, reason := workspaceProxyCrossUserBlock(allowed, "reader"); blocked {
		t.Fatalf("own multipart upload blocked (%s)", reason)
	}
	// Allowed multipart bodies replay byte-identical for the upstream.
	restored, err := io.ReadAll(allowed.Body)
	if err != nil || !strings.Contains(string(restored), "note.txt") || !strings.Contains(string(restored), "folder_path") {
		t.Fatal("multipart body not restored after scan")
	}

	for _, target := range []string{
		"/api/wp/api/versions/_users/owner/Chats/x",
		"/api/wp/api/restore/_users/owner/Chats/x",
		"/api/wp/api/documents/glob?pattern=_users/owner/**",
		"/api/wp/api/db/tables?db_path=_users/owner/db.sqlite",
	} {
		if blocked, _ := workspaceProxyCrossUserBlock(proxyRequest("GET", target, ""), "reader"); !blocked {
			t.Fatalf("%s: not blocked", target)
		}
	}
	if blocked, _ := workspaceProxyCrossUserBlock(proxyRequest("POST", "/api/wp/api/workspace/export", `{"workspace_path":"_users/owner/Chats"}`), "reader"); !blocked {
		t.Fatal("cross-user workspace export not blocked")
	}
}

// crewRunModeFixture is a two-user workspace: owner holds crew-aaa (with a
// runtime manifest, brief, files, a private transcript, and a database),
// reader holds their own crew-beta. Workflow/shared is visible to both;
// Workflow/private is owner-only.
type crewRunModeFixture struct {
	mock    *mockWorkspaceAPI
	files   map[string]string
	profile agentprofiles.Profile
	api     *StreamingAPI
}

const crewRunModeOwnerRoot = "_users/owner/Chats/Work/projects/alpha"

const crewRunModeOwnerProduct = `{"schema_version":1,"product":"work","id":"crew-aaa","title":"Alpha","session_id":"sess-owner","identity":{"name":"Alpha"},"created_at":"2026-09-01T00:00:00Z","updated_at":"2026-09-02T00:00:00Z"}`

const crewRunModeOwnerRuntime = `{
  "schema_version": 1, "product": "work", "id": "crew-aaa", "title": "Alpha",
  "capabilities": {
    "llm_config": {"connection_id": "owner-conn-1", "builder_llm": {"provider": "openai", "model_id": "gpt-4o", "connection_id": "owner-conn-1", "options": {"reasoning_effort": "high"}}},
    "selected_servers": ["github"],
    "selected_skills": ["code-review"],
    "selected_secrets": ["deploy-token"],
    "selected_global_secret_names": ["company-key"],
    "workflow_context_paths": ["Workflow/shared", "Workflow/private"]
  },
  "schedules": [{"id": "sched-1", "name": "Nightly", "enabled": true}],
  "triggers": [
    {"id": "trig-internal", "name": "Review", "enabled": true, "kind": "internal", "message": "Review", "caller": {"type": "workflow", "id": "release-pipeline"}},
    {"id": "trig-public", "name": "Hook", "enabled": true, "message": "Hook", "run_destination": "main", "webhook": {"auth_mode": "bearer", "encrypted_secret": "TOP-SECRET-CIPHERTEXT"}}
  ],
  "workflow_context_paths": ["Workflow/shared", "Workflow/private"]
}`

func newCrewRunModeFixture(t *testing.T) *crewRunModeFixture {
	t.Helper()
	t.Setenv("MULTI_USER_MODE", "true")
	withMemoryUserDirectory(t, `{"users":[{"id":"owner","username":"aman","can_create":true},{"id":"reader","username":"vaibhav","can_create":true},{"id":"stranger","username":"stranger","can_create":true}]}`)

	shared := NewWorkflowManifest("Shared")
	shared.CreatedBy = "owner"
	shared.Access = &WorkflowAccess{Owners: []string{"owner"}, Readers: []string{"reader"}}
	sharedRaw, _ := json.Marshal(shared)
	private := NewWorkflowManifest("Private")
	private.CreatedBy = "owner"
	private.Access = &WorkflowAccess{Owners: []string{"owner"}}
	privateRaw, _ := json.Marshal(private)

	files := map[string]string{
		crewRunModeOwnerRoot + "/product.json":                        crewRunModeOwnerProduct,
		crewRunModeOwnerRoot + "/workflow.json":                       crewRunModeOwnerRuntime,
		crewRunModeOwnerRoot + "/MEMORY.md":                           "# Alpha brief",
		crewRunModeOwnerRoot + "/code/main.py":                        "print('hi')",
		crewRunModeOwnerRoot + "/code/blob.bin":                       "ab\x00cd",
		crewRunModeOwnerRoot + "/builder/session-1-conversation.json": `{"private":true}`,
		crewRunModeOwnerRoot + "/db/app.sqlite":                       "sqlite\x00data",
		"_users/reader/Chats/Work/projects/beta/product.json":         `{"schema_version":1,"product":"work","id":"crew-bbb","title":"Beta","created_at":"2026-09-03T00:00:00Z","updated_at":"2026-09-03T00:00:00Z"}`,
		manifestPath("Workflow/shared"):                               string(sharedRaw),
		manifestPath("Workflow/private"):                              string(privateRaw),
	}
	mock := &mockWorkspaceAPI{files: files}
	ws := httptest.NewServer(mock)
	t.Cleanup(ws.Close)
	t.Setenv("WORKSPACE_API_URL", ws.URL)

	registry := agentprofiles.NewRegistry()
	profile := agentprofiles.Profile{
		ID: "work", Name: "Work", Version: 1, SystemPromptTemplate: "hi", BuiltIn: true, Product: "work",
		Runtime: agentprofiles.RuntimePolicy{
			Transport:    "auto",
			Conversation: agentprofiles.ConversationPolicy{Mode: agentprofiles.ConversationModeKeyed, KeyType: agentprofiles.ConversationKeyTypeProject},
			Workspace:    agentprofiles.WorkspacePolicy{Mode: agentprofiles.WorkspaceModeProject, Root: "Chats", ProjectsRoot: "Chats/Work/projects"},
		},
		UIPanels: agentprofiles.UIPanels{Schedules: true},
	}
	if err := registry.RegisterProfile(profile); err != nil {
		t.Fatal(err)
	}
	return &crewRunModeFixture{mock: mock, files: files, profile: profile, api: &StreamingAPI{agentProfiles: registry}}
}

func TestResolveCrewProjectBinding(t *testing.T) {
	fx := newCrewRunModeFixture(t)
	ctx := context.Background()

	owned, err := resolveCrewProjectBinding(ctx, "owner", fx.profile, "crew-aaa", crewRunModeOwnerRoot)
	if err != nil || !owned.OwnedByCaller || owned.OwnerID != "owner" {
		t.Fatalf("owner resolve = %+v err=%v", owned, err)
	}
	if owned.Binding.ManifestPath == "" || owned.Binding.AuthoritativeSessionID != "sess-owner" {
		t.Fatalf("owner binding lost manifest coupling: %+v", owned.Binding)
	}

	reader, err := resolveCrewProjectBinding(ctx, "reader", fx.profile, "crew-aaa", crewRunModeOwnerRoot)
	if err != nil || reader.OwnedByCaller || reader.OwnerID != "owner" {
		t.Fatalf("reader resolve = %+v err=%v", reader, err)
	}
	if reader.Binding.ManifestPath != "" || reader.Binding.AuthoritativeSessionID != "" {
		t.Fatalf("reader binding must not couple the owner's manifest: %+v", reader.Binding)
	}
	if reader.Binding.WorkspacePath != crewRunModeOwnerRoot {
		t.Fatalf("reader root = %q", reader.Binding.WorkspacePath)
	}

	// The open path carries no folder hint: the owner scan finds it.
	scanned, err := resolveCrewProjectBinding(ctx, "reader", fx.profile, "crew-aaa", "")
	if err != nil || scanned.Binding.WorkspacePath != crewRunModeOwnerRoot {
		t.Fatalf("scan resolve = %+v err=%v", scanned, err)
	}

	if _, err := resolveCrewProjectBinding(ctx, "reader", fx.profile, "crew-nope", ""); err == nil {
		t.Fatal("unknown project resolved")
	}
	if _, err := resolveCrewProjectBinding(ctx, "reader", fx.profile, "", ""); err == nil {
		t.Fatal("empty project resolved")
	}
}

func TestResolveCrewProjectBindingAmbiguousAcrossOwners(t *testing.T) {
	t.Setenv("MULTI_USER_MODE", "true")
	withMemoryUserDirectory(t, `{"users":[{"id":"owner","username":"owner","can_create":true},{"id":"reader","username":"reader","can_create":true}]}`)
	mock := &mockWorkspaceAPI{files: map[string]string{
		"_users/owner/Chats/Work/projects/alpha/product.json":  `{"schema_version":1,"product":"work","id":"crew-dup","title":"A"}`,
		"_users/reader/Chats/Work/projects/clone/product.json": `{"schema_version":1,"product":"work","id":"crew-dup","title":"B"}`,
	}}
	ws := httptest.NewServer(mock)
	defer ws.Close()
	t.Setenv("WORKSPACE_API_URL", ws.URL)

	registry := agentprofiles.NewRegistry()
	profile := agentprofiles.Profile{
		ID: "work", Name: "Work", Version: 1, SystemPromptTemplate: "hi", BuiltIn: true, Product: "work",
		Runtime: agentprofiles.RuntimePolicy{
			Conversation: agentprofiles.ConversationPolicy{Mode: agentprofiles.ConversationModeKeyed, KeyType: agentprofiles.ConversationKeyTypeProject},
			Workspace:    agentprofiles.WorkspacePolicy{Mode: agentprofiles.WorkspaceModeProject, Root: "Chats", ProjectsRoot: "Chats/Work/projects"},
		},
	}
	if err := registry.RegisterProfile(profile); err != nil {
		t.Fatal(err)
	}
	// A third user scanning finds the same ID under two owners: fail closed.
	withMemoryUserDirectory(t, `{"users":[{"id":"owner","username":"owner","can_create":true},{"id":"reader","username":"reader","can_create":true},{"id":"stranger","username":"stranger","can_create":true}]}`)
	if _, err := resolveCrewProjectBinding(context.Background(), "stranger", profile, "crew-dup", ""); err == nil {
		t.Fatal("duplicate project ID across owners resolved")
	}
}

func TestConversationTargetAccessCrewReader(t *testing.T) {
	fx := newCrewRunModeFixture(t)
	claimsCtx := func(userID string) context.Context {
		return context.WithValue(context.Background(), UserContextKey, &UserClaims{UserID: userID, Username: userID})
	}
	ownerAccess, err := fx.api.conversationTargetAccess(claimsCtx("owner"), QueryRequest{AgentProfileID: "work", AgentProfileConversationKey: "crew-aaa", SelectedFolder: crewRunModeOwnerRoot})
	if err != nil || ownerAccess != WorkflowAccessOwner {
		t.Fatalf("owner access = %v err=%v", ownerAccess, err)
	}
	readerAccess, err := fx.api.conversationTargetAccess(claimsCtx("reader"), QueryRequest{AgentProfileID: "work", AgentProfileConversationKey: "crew-aaa", SelectedFolder: crewRunModeOwnerRoot})
	if err != nil || readerAccess != WorkflowAccessRead {
		t.Fatalf("reader access = %v err=%v", readerAccess, err)
	}
	if _, err := fx.api.conversationTargetAccess(claimsCtx("reader"), QueryRequest{AgentProfileID: "work", AgentProfileConversationKey: "crew-aaa", SelectedFolder: "_users/reader/Chats/Work/projects/beta"}); err == nil {
		t.Fatal("mismatched folder granted access")
	}
	if _, err := fx.api.conversationTargetAccess(claimsCtx("reader"), QueryRequest{AgentProfileID: "work", AgentProfileConversationKey: "crew-nope", SelectedFolder: crewRunModeOwnerRoot}); err == nil {
		t.Fatal("unknown project granted access")
	}
}

func TestReaderResolveLeavesOwnerManifestUntouched(t *testing.T) {
	fx := newCrewRunModeFixture(t)
	ctx := context.WithValue(context.Background(), UserContextKey, &UserClaims{UserID: "reader", Username: "reader"})

	binding, owned, err := resolveConversationBindingForUser(ctx, "reader", fx.profile, "crew-aaa")
	if err != nil || owned {
		t.Fatalf("reader binding = owned=%v err=%v", owned, err)
	}
	if binding.ManifestPath != "" || binding.AuthoritativeSessionID != "" {
		t.Fatalf("reader binding couples the owner manifest: %+v", binding)
	}
	store := defaultProductConversationRegistryStore()
	record, err := store.resolveOrCreate(ctx, "reader", fx.profile, binding, "")
	if err != nil {
		t.Fatalf("reader slot resolve: %v", err)
	}
	if record.WorkspacePath != crewRunModeOwnerRoot {
		t.Fatalf("reader slot root = %q", record.WorkspacePath)
	}
	if got := fx.files[crewRunModeOwnerRoot+"/product.json"]; got != crewRunModeOwnerProduct {
		t.Fatal("reader resolve rewrote the owner's manifest")
	}
	readerWrites := 0
	for path := range fx.files {
		if strings.HasPrefix(path, "_users/reader/") && path != "_users/reader/Chats/Work/projects/beta/product.json" {
			readerWrites++
		}
	}
	if readerWrites == 0 {
		t.Fatal("reader slot left no reader-scoped registry write")
	}
}

func TestListSharedProjects(t *testing.T) {
	fx := newCrewRunModeFixture(t)

	get := func(caller string) (int, []byte) {
		req := mux.SetURLVars(profileRouteRequest(http.MethodGet, "/api/agent-profiles/work/shared-projects", nil, caller), map[string]string{"id": "work"})
		rec := httptest.NewRecorder()
		fx.api.handleListSharedProjects(rec, req)
		return rec.Code, rec.Body.Bytes()
	}
	code, body := get("reader")
	if code != http.StatusOK {
		t.Fatalf("reader list status = %d: %s", code, body)
	}
	var decoded struct {
		Projects []sharedProjectSummary `json:"projects"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Projects) != 1 || decoded.Projects[0].ID != "crew-aaa" {
		t.Fatalf("reader projects = %+v", decoded.Projects)
	}
	row := decoded.Projects[0]
	if row.OwnerID != "owner" || row.OwnerUsername != "aman" || row.Title != "Alpha" {
		t.Fatalf("row = %+v", row)
	}
	if row.LLM == nil || row.LLM.Provider != "openai" || row.LLM.ModelID != "gpt-4o" || row.LLM.ReasoningEffort != "high" {
		t.Fatalf("llm = %+v", row.LLM)
	}
	if len(row.SelectedServers) != 1 || len(row.SelectedSecrets) != 1 || len(row.Schedules) != 1 || len(row.Triggers) != 2 {
		t.Fatalf("row selections = %+v", row)
	}
	if len(row.WorkflowContextPaths) != 1 || row.WorkflowContextPaths[0] != "Workflow/shared" {
		t.Fatalf("refs = %v, private workflow leaked or visible one dropped", row.WorkflowContextPaths)
	}
	raw := string(body)
	for _, leaked := range []string{"TOP-SECRET-CIPHERTEXT", "encrypted_secret", "connection_id", "owner-conn-1", "Workflow/private", "sess-owner"} {
		if strings.Contains(raw, leaked) {
			t.Fatalf("list leaked %q", leaked)
		}
	}

	// Visibility is universal for product holders: the owner sees the
	// reader's crew too, and a stranger with no shared workflows sees
	// both crews.
	code, body = get("owner")
	if code != http.StatusOK {
		t.Fatalf("owner list status = %d", code)
	}
	decoded.Projects = nil
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Projects) != 1 || decoded.Projects[0].ID != "crew-bbb" {
		t.Fatalf("owner projects = %+v", decoded.Projects)
	}
	code, body = get("stranger")
	if code != http.StatusOK {
		t.Fatalf("stranger list status = %d", code)
	}
	decoded.Projects = nil
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Projects) != 2 {
		t.Fatalf("stranger projects = %+v", decoded.Projects)
	}
}

func TestSharedProjectFilesAndFile(t *testing.T) {
	fx := newCrewRunModeFixture(t)
	filesReq := func(caller string) (int, []byte) {
		req := mux.SetURLVars(profileRouteRequest(http.MethodGet, "/api/agent-profiles/work/shared-projects/crew-aaa/files", nil, caller), map[string]string{"id": "work", "project_id": "crew-aaa"})
		rec := httptest.NewRecorder()
		fx.api.handleListSharedProjectFiles(rec, req)
		return rec.Code, rec.Body.Bytes()
	}
	code, body := filesReq("reader")
	if code != http.StatusOK {
		t.Fatalf("files status = %d: %s", code, body)
	}
	var tree struct {
		Files []sharedProjectFileEntry `json:"files"`
	}
	if err := json.Unmarshal(body, &tree); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, entry := range tree.Files {
		seen[entry.Path] = true
	}
	for _, want := range []string{"product.json", "workflow.json", "MEMORY.md", "code/main.py"} {
		if !seen[want] {
			t.Fatalf("tree missing %q: %+v", want, tree.Files)
		}
	}
	for path := range seen {
		if strings.HasPrefix(path, "builder") || strings.HasPrefix(path, "db") {
			t.Fatalf("private subtree in tree: %q", path)
		}
	}

	fileReq := func(caller, rel string) (int, []byte) {
		req := mux.SetURLVars(profileRouteRequest(http.MethodGet, "/api/agent-profiles/work/shared-projects/crew-aaa/file?path="+rel, nil, caller), map[string]string{"id": "work", "project_id": "crew-aaa"})
		rec := httptest.NewRecorder()
		fx.api.handleGetSharedProjectFile(rec, req)
		return rec.Code, rec.Body.Bytes()
	}
	if code, body := fileReq("reader", "code/main.py"); code != http.StatusOK || !strings.Contains(string(body), "print") {
		t.Fatalf("file status = %d: %s", code, body)
	}
	for _, bad := range []string{"builder/session-1-conversation.json", "db/app.sqlite", "missing.txt"} {
		if code, _ := fileReq("reader", bad); code != http.StatusNotFound {
			t.Fatalf("file(%q) status = %d, want 404", bad, code)
		}
	}
	// Escape attempts collapse inside the root: they serve the root file,
	// never anything outside it.
	if code, body := fileReq("reader", "../../product.json"); code != http.StatusOK || !strings.Contains(string(body), "crew-aaa") {
		t.Fatalf("collapsed file status = %d: %s", code, body)
	}
	if code, _ := fileReq("reader", "code/blob.bin"); code != http.StatusUnsupportedMediaType {
		t.Fatalf("binary file status = %d, want 415", code)
	}
	req := mux.SetURLVars(profileRouteRequest(http.MethodGet, "/api/agent-profiles/work/shared-projects/crew-nope/files", nil, "reader"), map[string]string{"id": "work", "project_id": "crew-nope"})
	rec := httptest.NewRecorder()
	fx.api.handleListSharedProjectFiles(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown project files status = %d", rec.Code)
	}
}

func TestRegisterWorkScheduleToolsReaderSplit(t *testing.T) {
	fx := newCrewRunModeFixture(t)
	api := &StreamingAPI{productSchedules: &ProductScheduleService{}}

	reader := &recordingRegistrar{}
	if err := api.registerWorkScheduleTools(reader, "reader", crewRunModeOwnerRoot, true); err != nil {
		t.Fatalf("reader register: %v", err)
	}
	for _, want := range []string{"list_project_schedules", "list_project_triggers"} {
		if _, ok := reader.tools[want]; !ok {
			t.Fatalf("reader missing %q: %v", want, keysOf(reader.tools))
		}
	}
	for name := range reader.tools {
		if strings.HasPrefix(name, "create_") || strings.HasPrefix(name, "update_") || strings.HasPrefix(name, "delete_") || name == "trigger_project_schedule" {
			t.Fatalf("reader registered mutation %q", name)
		}
	}

	owner := &recordingRegistrar{}
	if err := api.registerWorkScheduleTools(owner, "owner", crewRunModeOwnerRoot, false); err != nil {
		t.Fatalf("owner register: %v", err)
	}
	for _, want := range []string{"list_project_schedules", "create_project_schedule", "update_project_schedule", "delete_project_schedule", "trigger_project_schedule"} {
		if _, ok := owner.tools[want]; !ok {
			t.Fatalf("owner missing %q", want)
		}
	}
	_ = fx
}

func TestRegisterAgentProfileToolsReaderSplit(t *testing.T) {
	fx := newCrewRunModeFixture(t)
	registry := agentprofiles.NewRegistry()
	if err := workproduct.RegisterAgentProfileRuntime(registry, "http://127.0.0.1:0"); err != nil {
		t.Fatalf("register Work runtime: %v", err)
	}
	resolved := &resolvedAgentProfile{Definition: workproduct.BuiltinAgentProfile()}
	api := &StreamingAPI{agentProfiles: registry, productSchedules: &ProductScheduleService{}}
	api.scheduler = NewSchedulerService(api)

	registrar := &recordingRegistrar{}
	if err := api.registerAgentProfileTools(registrar, newProductToolGate(resolved), resolved, "reader", "session-1", crewRunModeOwnerRoot, true, QueryRequest{}); err != nil {
		t.Fatalf("reader register: %v", err)
	}
	for _, want := range []string{
		"list_accessible_workflows", "list_attached_workflows", "list_workflow_triggers", "run_workflow_trigger", "get_workflow_trigger_run",
		"list_project_schedules", "list_project_triggers",
		"manage_custom_commands", "list_work_folders", "attach_work_folder",
	} {
		if _, ok := registrar.tools[want]; !ok {
			t.Fatalf("reader missing %q: %v", want, keysOf(registrar.tools))
		}
	}
	for _, denied := range []string{
		"attach_workflow_reference", "detach_workflow_reference",
		"create_project_schedule", "update_project_schedule", "delete_project_schedule", "trigger_project_schedule",
		"create_project_trigger", "update_project_trigger", "delete_project_trigger",
		"update_project_mcp_server_selection", "update_project_global_secret_selection", "update_project_skill_selection",
		"set_work_identity", "create_crew", "perform_ui_action", "get_file_link",
	} {
		if _, ok := registrar.tools[denied]; ok {
			t.Fatalf("reader registered %q", denied)
		}
	}
	_ = fx
}

func keysOf(tools map[string]recordedTool) []string {
	out := make([]string, 0, len(tools))
	for name := range tools {
		out = append(out, name)
	}
	return out
}

func TestProjectManifestAnyOwner(t *testing.T) {
	fx := newCrewRunModeFixture(t)
	registry := agentprofiles.NewRegistry()
	profile := fx.profile
	profile.UIPanels = agentprofiles.UIPanels{Schedules: true}
	if err := registry.RegisterProfile(profile); err != nil {
		t.Fatal(err)
	}
	// Legacy shape: capabilities inline in product.json, no workflow.json.
	// The reader fallback must resolve it without migrating (writing).
	delete(fx.files, crewRunModeOwnerRoot+"/workflow.json")
	backing := map[string]string{}
	for path, content := range fx.files {
		backing[path] = content
	}
	svc := NewProductScheduleService(nil, registry)
	svc.readFile = func(_ context.Context, path string) (string, bool, error) {
		content, ok := backing[path]
		return content, ok, nil
	}
	svc.writeFile = func(_ context.Context, path, content string) error {
		backing[path] = content
		return nil
	}

	_, binding, manifest, ownerID, err := svc.projectManifestAnyOwner(context.Background(), "reader", "work", "crew-aaa")
	if err != nil {
		t.Fatalf("reader fallback: %v", err)
	}
	if ownerID != "owner" || binding.WorkspacePath != crewRunModeOwnerRoot || manifest.ID != "crew-aaa" {
		t.Fatalf("fallback = owner=%q root=%q id=%q", ownerID, binding.WorkspacePath, manifest.ID)
	}
	if _, migrated := fx.files[crewRunModeOwnerRoot+"/workflow.json"]; migrated {
		t.Fatal("reader fallback migrated the owner's manifests through the workspace API")
	}
	if len(backing) != len(fx.files) {
		t.Fatal("reader fallback migrated the owner's manifests through the service funcs")
	}
	for path, content := range backing {
		if fx.files[path] != content {
			t.Fatalf("reader fallback rewrote %q", path)
		}
	}
}

func TestListAttachedWorkflowsReaderFiltersInvisible(t *testing.T) {
	fx := newCrewRunModeFixture(t)
	api := &StreamingAPI{productSchedules: &ProductScheduleService{}}
	api.scheduler = NewSchedulerService(api)
	// Registration stamps the turn user (as the query path does per turn),
	// so register once per caller.
	execAs := func(userID string) []string {
		registrar := &recordingRegistrar{}
		if err := api.registerCrewWorkflowRunTools(registrar, userID, "session-1", crewRunModeOwnerRoot); err != nil {
			t.Fatalf("register for %s: %v", userID, err)
		}
		tool, ok := registrar.tools["list_attached_workflows"]
		if !ok {
			t.Fatal("list_attached_workflows not registered")
		}
		out, err := tool.exec(context.Background(), map[string]interface{}{})
		if err != nil {
			t.Fatalf("exec as %s: %v", userID, err)
		}
		var decoded struct {
			Workflows []struct {
				WorkspacePath string `json:"workspace_path"`
			} `json:"workflows"`
		}
		if err := json.Unmarshal([]byte(out), &decoded); err != nil {
			t.Fatal(err)
		}
		paths := []string{}
		for _, entry := range decoded.Workflows {
			paths = append(paths, entry.WorkspacePath)
		}
		return paths
	}
	readerPaths := execAs("reader")
	if len(readerPaths) != 1 || readerPaths[0] != "Workflow/shared" {
		t.Fatalf("reader refs = %v", readerPaths)
	}
	ownerPaths := execAs("owner")
	if len(ownerPaths) != 2 {
		t.Fatalf("owner refs = %v", ownerPaths)
	}
	_ = fx
}
