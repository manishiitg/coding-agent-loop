package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWorkflowAccessResolution(t *testing.T) {
	t.Setenv("MULTI_USER_MODE", "true")
	withMemoryUserDirectory(t, `{"users":[
	  {"id":"a1","username":"alice","admin":true,"can_create":true,"products":[]},
	  {"id":"b2","username":"bob","can_create":true,"products":[]},
	  {"id":"c3","username":"carol","can_create":false,"products":[]},
	  {"id":"d4","username":"dan","can_create":true,"products":[]},
	  {"id":"e5","username":"erin","can_create":false,"can_edit":true,"products":["agentworks"]}
	]}`)
	alice, bob, carol, dan, erin := &UserClaims{UserID: "a1"}, &UserClaims{UserID: "b2"}, &UserClaims{UserID: "c3"}, &UserClaims{UserID: "d4"}, &UserClaims{UserID: "e5"}

	owned := &WorkflowManifest{ID: "w1", CreatedBy: "b2", Access: &WorkflowAccess{Owners: []string{"b2"}, Readers: []string{"c3"}}}
	if got := workflowAccessForManifest(bob, owned); got != WorkflowAccessOwner {
		t.Fatalf("owner: %s", got)
	}
	if got := workflowAccessForManifest(carol, owned); got != WorkflowAccessRead {
		t.Fatalf("reader: %s", got)
	}
	if got := workflowAccessForManifest(dan, owned); got != WorkflowAccessNone {
		t.Fatalf("unrelated member must not see it: %s", got)
	}
	if got := workflowAccessForManifest(alice, owned); got != WorkflowAccessOwner {
		t.Fatalf("admin is owner of everything: %s", got)
	}
	contributorOwned := &WorkflowManifest{ID: "w5", Access: &WorkflowAccess{Owners: []string{"e5"}}}
	if got := workflowAccessForManifest(erin, contributorOwned); got != WorkflowAccessOwner {
		t.Fatalf("contributor may own an assigned workflow: %s", got)
	}
	if userAccessForClaims(erin).CanCreate {
		t.Fatal("contributor must not be able to create workflows")
	}

	// created_by alone names the owner on manifests written before the access block existed.
	legacyCreated := &WorkflowManifest{ID: "w2", CreatedBy: "d4"}
	if got := workflowAccessForManifest(dan, legacyCreated); got != WorkflowAccessOwner {
		t.Fatalf("created_by owner: %s", got)
	}
	if got := workflowAccessForManifest(bob, legacyCreated); got != WorkflowAccessNone {
		t.Fatalf("created_by excludes others: %s", got)
	}

	// No ownership record at all: the account tier applies, as before phase 3.
	unowned := &WorkflowManifest{ID: "w3"}
	if got := workflowAccessForManifest(bob, unowned); got != WorkflowAccessWrite {
		t.Fatalf("unowned for a member: %s", got)
	}
	if got := workflowAccessForManifest(carol, unowned); got != WorkflowAccessRead {
		t.Fatalf("unowned for a read-only account: %s", got)
	}

	// A read-only account listed as owner is still only a reader.
	listedReadOnly := &WorkflowManifest{ID: "w4", Access: &WorkflowAccess{Owners: []string{"c3"}}}
	if got := workflowAccessForManifest(carol, listedReadOnly); got != WorkflowAccessRead {
		t.Fatalf("read-only account can never edit: %s", got)
	}
}

func TestFilterAnnotatesMyAccessAndHidesUnshared(t *testing.T) {
	t.Setenv("MULTI_USER_MODE", "true")
	withMemoryUserDirectory(t, `{"users":[{"id":"b2","username":"bob","can_create":true,"products":[]},{"id":"d4","username":"dan","can_create":true,"products":[]}]}`)
	discovered := []DiscoveredWorkflow{
		{WorkspacePath: "Workflow/mine", Manifest: &WorkflowManifest{ID: "mine", Access: &WorkflowAccess{Owners: []string{"b2"}}}},
		{WorkspacePath: "Workflow/shared", Manifest: &WorkflowManifest{ID: "shared", Access: &WorkflowAccess{Owners: []string{"d4"}, Readers: []string{"b2"}}}},
		{WorkspacePath: "Workflow/theirs", Manifest: &WorkflowManifest{ID: "theirs", Access: &WorkflowAccess{Owners: []string{"d4"}}}},
		{WorkspacePath: "Workflow/legacy", Manifest: &WorkflowManifest{ID: "legacy"}},
	}
	got := filterWorkflowManifestsForUser(&UserClaims{UserID: "b2"}, discovered)
	want := map[string]WorkflowAccessLevel{"Workflow/mine": WorkflowAccessOwner, "Workflow/shared": WorkflowAccessRead, "Workflow/legacy": WorkflowAccessWrite}
	if len(got) != len(want) {
		t.Fatalf("got %d workflows, want %d: %+v", len(got), len(want), got)
	}
	for _, w := range got {
		if want[w.WorkspacePath] != w.MyAccess {
			t.Fatalf("%s: my_access=%s want %s", w.WorkspacePath, w.MyAccess, want[w.WorkspacePath])
		}
	}
}

func TestContributorCanEditButCannotCreate(t *testing.T) {
	t.Setenv("MULTI_USER_MODE", "true")
	withMemoryUserDirectory(t, `{"users":[{"id":"e5","username":"erin","can_create":false,"can_edit":true,"products":["agentworks"]}]}`)
	claims := &UserClaims{UserID: "e5", Username: "erin"}

	owned := &WorkflowManifest{ID: "owned", Access: &WorkflowAccess{Owners: []string{"e5"}}}
	if got := workflowAccessForManifest(claims, owned); got != WorkflowAccessOwner {
		t.Fatalf("assigned contributor access = %s, want owner", got)
	}

	createCalled := false
	handler := requireWorkflowCreateAccess(func(w http.ResponseWriter, _ *http.Request) {
		createCalled = true
		w.WriteHeader(http.StatusCreated)
	})
	rec := httptest.NewRecorder()
	handler(rec, adminRequest(http.MethodPost, "/api/workflows/manifest", `{}`, claims, nil))
	if createCalled || rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), `"required_access":"create"`) {
		t.Fatalf("contributor create gate = code %d body %s called=%v", rec.Code, rec.Body.String(), createCalled)
	}

	writeCalled := false
	writeHandler := requireWorkflowWriteAccess(func(w http.ResponseWriter, _ *http.Request) {
		writeCalled = true
		w.WriteHeader(http.StatusNoContent)
	})
	rec = httptest.NewRecorder()
	writeHandler(rec, adminRequest(http.MethodPut, "/api/workflows/manifest", `{}`, claims, nil))
	if !writeCalled || rec.Code != http.StatusNoContent {
		t.Fatalf("contributor write gate = code %d called=%v", rec.Code, writeCalled)
	}
}

func TestWorkflowFolderFromProxyPath(t *testing.T) {
	cases := map[string]string{
		"api/documents/Workflow/abc/plan.json": "Workflow/abc",
		"api/folders/Workflow/abc":             "Workflow/abc",
		"api/documents/Workflow":               "",
		"api/documents/Chats/x":                "",
	}
	for in, want := range cases {
		if got := workflowFolderFromWorkspaceProxyPath(in); got != want {
			t.Fatalf("%s: got %q want %q", in, got, want)
		}
	}
}

func TestSessionVisibility(t *testing.T) {
	t.Setenv("MULTI_USER_MODE", "true")
	withMemoryUserDirectory(t, `{"users":[{"id":"a1","username":"alice","admin":true,"can_create":true,"products":[]},{"id":"b2","username":"bob","can_create":true,"products":[]}]}`)
	bob, alice := &UserClaims{UserID: "b2"}, &UserClaims{UserID: "a1"}
	if !sessionVisibleTo("b2", bob) || sessionVisibleTo("a1", bob) {
		t.Fatal("own session visible, someone else's not")
	}
	if sessionVisibleTo("", bob) {
		t.Fatal("an unowned session must not be visible to every member")
	}
	if !sessionVisibleTo("", alice) {
		t.Fatal("an unowned session is visible to an admin")
	}
	t.Setenv("MULTI_USER_MODE", "false")
	if !sessionVisibleTo("", &UserClaims{UserID: "default"}) {
		t.Fatal("single-user mode keeps unowned sessions visible")
	}
}

func TestSetWorkflowAccessValidation(t *testing.T) {
	t.Setenv("MULTI_USER_MODE", "true")
	withMemoryUserDirectory(t, `{"users":[{"id":"b2","username":"bob","email":"bob@x.io","can_create":true,"products":[]},{"id":"c3","username":"carol","can_create":false,"products":[]}]}`)
	api := &StreamingAPI{}
	// No workflow at the path (workspace API not reachable in tests) → 404 rather than a grant.
	rec := httptest.NewRecorder()
	api.handleSetWorkflowAccess(rec, adminRequest(http.MethodPut, "/api/workflow/access", `{"workspace_path":"Workflow/nope","owners":["bob"]}`, &UserClaims{UserID: "b2"}, nil))
	if rec.Code != http.StatusNotFound && rec.Code != http.StatusInternalServerError {
		t.Fatalf("missing workflow: %d %s", rec.Code, rec.Body.String())
	}
	// Directory picker lists enabled users only, never hashes or roles.
	rec = httptest.NewRecorder()
	api.handleUserDirectory(rec, adminRequest(http.MethodGet, "/api/users/directory", "", &UserClaims{UserID: "c3"}, nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"username":"bob"`) || strings.Contains(rec.Body.String(), "password") || strings.Contains(rec.Body.String(), "can_create") {
		t.Fatalf("directory: %d %s", rec.Code, rec.Body.String())
	}
}
