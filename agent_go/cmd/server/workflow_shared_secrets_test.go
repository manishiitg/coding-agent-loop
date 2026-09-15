package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gorilla/mux"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/chathistory"
)

const sharedSecretsTestWorkflow = "Workflow/renewals"
const sharedSecretsTestProductWorkspace = "_users/a1/Chats/Work/projects/demo"

// sharedSecretsTestAPI builds a StreamingAPI over a filesystem chat store with
// one workflow whose manifest names alice (a1) as owner and carol (c3) as
// reader; dan (d4) is a member with no access to it.
func sharedSecretsTestAPI(t *testing.T) (*StreamingAPI, string) {
	t.Helper()
	t.Setenv("AUTH_SECRET", "plat-271-test-auth-secret-0123456789abcdef")
	t.Setenv("MULTI_USER_MODE", "true")
	withMemoryUserDirectory(t, `{"users":[
	  {"id":"a1","username":"alice","can_create":true,"products":[]},
	  {"id":"c3","username":"carol","can_create":false,"products":[]},
	  {"id":"d4","username":"dan","can_create":true,"products":[]}
	]}`)
	manifest := WorkflowManifest{ID: "wf_shared", Label: "renewals", CreatedBy: "a1", Access: &WorkflowAccess{Owners: []string{"a1"}, Readers: []string{"c3"}}}
	raw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	ws := httptest.NewServer(&mockWorkspaceAPI{files: map[string]string{
		manifestPath(sharedSecretsTestWorkflow):         string(raw),
		manifestPath(sharedSecretsTestProductWorkspace): string(raw),
	}})
	t.Cleanup(ws.Close)
	t.Setenv("WORKSPACE_API_URL", ws.URL)

	root := t.TempDir()
	store, err := chathistory.NewFilesystemStore(root)
	if err != nil {
		t.Fatalf("NewFilesystemStore: %v", err)
	}
	return &StreamingAPI{chatStore: store}, root
}

func TestProductRelativeSecretPathUsesRuntimeWorkspace(t *testing.T) {
	api, _ := sharedSecretsTestAPI(t)
	const browserPath = "Chats/Work/projects/demo"

	fromAlice, err := encryptSecretValueWithAAD("hunter2", []byte("a1"))
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	w := httptest.NewRecorder()
	body := storeSecretRequest{Name: "TOKEN", EncryptedValue: fromAlice, WorkspacePath: browserPath}
	api.handleStoreWorkflowSecret(w, sharedSecretsRequest(http.MethodPut, "/api/secrets/workflow/store", "a1", body))
	if w.Code != http.StatusOK {
		t.Fatalf("store using browser path: status %d body %s", w.Code, w.Body.String())
	}

	stored, err := api.chatStore.ListWorkflowSecrets(context.Background(), chathistory.SharedWorkflowSecretsUserID, sharedSecretsTestProductWorkspace)
	if err != nil || len(stored) != 1 || stored[0].Name != "TOKEN" {
		t.Fatalf("runtime workspace store = %+v err=%v", stored, err)
	}

	w = httptest.NewRecorder()
	api.handleListStoredWorkflowSecrets(w, sharedSecretsRequest(http.MethodGet, "/api/secrets/workflow/stored?workspace_path="+browserPath, "a1", nil))
	var listed []struct {
		Name           string `json:"name"`
		EncryptedValue string `json:"encrypted_value"`
	}
	if w.Code != http.StatusOK {
		t.Fatalf("list using browser path: status %d body %s", w.Code, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &listed); err != nil || len(listed) != 1 || listed[0].Name != "TOKEN" || listed[0].EncryptedValue == "" {
		t.Fatalf("listed secrets = %+v err=%v", listed, err)
	}

	w = httptest.NewRecorder()
	api.handleDecryptSecret(w, sharedSecretsRequest(http.MethodPost, "/api/secrets/decrypt", "a1", secretDecryptRequest{Encrypted: listed[0].EncryptedValue, WorkspacePath: browserPath}))
	var revealed secretDecryptResponse
	if w.Code != http.StatusOK {
		t.Fatalf("reveal using browser path: status %d body %s", w.Code, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &revealed); err != nil || revealed.Value != "hunter2" {
		t.Fatalf("revealed secret mismatch: err=%v", err)
	}
}

func sharedSecretsRequest(method, target, userID string, body interface{}) *http.Request {
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, target, &buf)
	req.Header.Set("Content-Type", "application/json")
	return req.WithContext(context.WithValue(req.Context(), UserContextKey, &UserClaims{UserID: userID}))
}

func TestSharedWorkflowSecretsAreGatedByWorkflowAccess(t *testing.T) {
	api, root := sharedSecretsTestAPI(t)

	// The pane encrypts against the caller before storing, exactly as the
	// browser does through /api/secrets/encrypt.
	fromAlice, err := encryptSecretValueWithAAD("hunter2", []byte("a1"))
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	storeBody := storeSecretRequest{Name: "TOKEN", EncryptedValue: fromAlice, WorkspacePath: sharedSecretsTestWorkflow}

	// dan has no access at all: refused before anything is written.
	w := httptest.NewRecorder()
	api.handleStoreWorkflowSecret(w, sharedSecretsRequest(http.MethodPut, "/api/secrets/workflow/store", "d4", storeBody))
	if w.Code != http.StatusForbidden {
		t.Fatalf("store as unrelated user: status %d, want 403", w.Code)
	}
	// carol can see the workflow but is a reader: no mutation.
	w = httptest.NewRecorder()
	api.handleStoreWorkflowSecret(w, sharedSecretsRequest(http.MethodPut, "/api/secrets/workflow/store", "c3", storeBody))
	if w.Code != http.StatusForbidden {
		t.Fatalf("store as reader: status %d, want 403", w.Code)
	}
	// alice owns it.
	w = httptest.NewRecorder()
	api.handleStoreWorkflowSecret(w, sharedSecretsRequest(http.MethodPut, "/api/secrets/workflow/store", "a1", storeBody))
	if w.Code != http.StatusOK {
		t.Fatalf("store as owner: status %d body %s", w.Code, w.Body.String())
	}

	// It landed in the workflow's shared document, not under alice.
	sharedDir := filepath.Join(root, "_users", chathistory.SharedWorkflowSecretsUserID, "workflow_secrets")
	entries, err := os.ReadDir(sharedDir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("shared store dir %s: entries=%v err=%v", sharedDir, entries, err)
	}
	if _, err := os.Stat(filepath.Join(root, "_users", "a1", "workflow_secrets")); !os.IsNotExist(err) {
		t.Fatalf("owner's per-user workflow store must stay empty, stat err=%v", err)
	}

	type listed struct {
		Name           string `json:"name"`
		EncryptedValue string `json:"encrypted_value"`
	}
	list := func(userID string) (int, []listed) {
		w := httptest.NewRecorder()
		api.handleListStoredWorkflowSecrets(w, sharedSecretsRequest(http.MethodGet, "/api/secrets/workflow/stored?workspace_path="+sharedSecretsTestWorkflow, userID, nil))
		var out []listed
		if w.Code == http.StatusOK {
			_ = json.Unmarshal(w.Body.Bytes(), &out)
		}
		return w.Code, out
	}
	if code, _ := list("d4"); code != http.StatusForbidden {
		t.Fatalf("list as unrelated user: status %d, want 403", code)
	}
	code, asReader := list("c3")
	if code != http.StatusOK || len(asReader) != 1 || asReader[0].Name != "TOKEN" {
		t.Fatalf("list as reader: status %d entries %+v", code, asReader)
	}
	if asReader[0].EncryptedValue != "" {
		t.Fatal("reader must receive names only, never ciphertext")
	}
	code, asOwner := list("a1")
	if code != http.StatusOK || len(asOwner) != 1 || asOwner[0].EncryptedValue == "" {
		t.Fatalf("list as owner: status %d entries %+v", code, asOwner)
	}

	// Reveal: the ciphertext is bound to the workflow, and only owners may open it.
	reveal := func(userID string) (int, string) {
		w := httptest.NewRecorder()
		api.handleDecryptSecret(w, sharedSecretsRequest(http.MethodPost, "/api/secrets/decrypt", userID, secretDecryptRequest{Encrypted: asOwner[0].EncryptedValue, WorkspacePath: sharedSecretsTestWorkflow}))
		var out secretDecryptResponse
		_ = json.Unmarshal(w.Body.Bytes(), &out)
		return w.Code, out.Value
	}
	if code, _ := reveal("c3"); code != http.StatusForbidden {
		t.Fatalf("reveal as reader: status %d, want 403", code)
	}
	if code, value := reveal("a1"); code != http.StatusOK || value != "hunter2" {
		t.Fatalf("reveal as owner: status %d value %q", code, value)
	}
	// The pre-PLAT-272 decrypt path (no workspace_path, caller-bound) cannot
	// open a workflow-bound blob even for the owner.
	w = httptest.NewRecorder()
	api.handleDecryptSecret(w, sharedSecretsRequest(http.MethodPost, "/api/secrets/decrypt", "a1", secretDecryptRequest{Encrypted: asOwner[0].EncryptedValue}))
	if w.Code != http.StatusForbidden {
		t.Fatalf("caller-bound decrypt of a workflow-bound blob: status %d, want 403", w.Code)
	}

	// Delete: reader refused, owner allowed.
	del := func(userID string) int {
		w := httptest.NewRecorder()
		req := sharedSecretsRequest(http.MethodDelete, "/api/secrets/workflow/store/TOKEN?workspace_path="+sharedSecretsTestWorkflow, userID, nil)
		req = mux.SetURLVars(req, map[string]string{"name": "TOKEN"})
		api.handleDeleteWorkflowSecret(w, req)
		return w.Code
	}
	if code := del("c3"); code != http.StatusForbidden {
		t.Fatalf("delete as reader: status %d, want 403", code)
	}
	if code := del("a1"); code != http.StatusOK {
		t.Fatalf("delete as owner: status %d", code)
	}
	if code, after := list("a1"); code != http.StatusOK || len(after) != 0 {
		t.Fatalf("after delete: status %d entries %+v", code, after)
	}
}

func TestLoadSelectedSecretsResolvesSharedWorkflowSecretsForEveryUserAndMigratesLegacy(t *testing.T) {
	api, _ := sharedSecretsTestAPI(t)
	ctx := context.Background()

	// A value stored before PLAT-272: in alice's own per-user document, bound
	// to alice. This is exactly what a reader or a scheduled run could never
	// resolve.
	legacy, err := encryptSecretValueWithAAD("legacy-value", []byte("a1"))
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if err := api.chatStore.UpsertWorkflowSecret(ctx, "a1", sharedSecretsTestWorkflow, "TOKEN", legacy); err != nil {
		t.Fatalf("seed legacy secret: %v", err)
	}

	// carol (reader) runs the workflow: the owner's legacy value is migrated
	// into the shared store on first touch and resolves for her.
	got := api.loadSelectedSecrets(ctx, "c3", sharedSecretsTestWorkflow, []string{"TOKEN"})
	if len(got) != 1 || got[0].Name != "TOKEN" || got[0].Value != "legacy-value" {
		t.Fatalf("reader resolution = %+v, want TOKEN=legacy-value", got)
	}

	shared, err := api.chatStore.ListWorkflowSecrets(ctx, chathistory.SharedWorkflowSecretsUserID, sharedSecretsTestWorkflow)
	if err != nil || len(shared) != 1 || shared[0].Name != "TOKEN" {
		t.Fatalf("shared store after migration: %+v err=%v", shared, err)
	}
	if _, err := decryptSharedWorkflowSecret(sharedSecretsTestWorkflow, shared[0]); err != nil {
		t.Fatalf("migrated value must be workflow-bound: %v", err)
	}
	remaining, err := api.chatStore.ListWorkflowSecrets(ctx, "a1", sharedSecretsTestWorkflow)
	if err != nil || len(remaining) != 0 {
		t.Fatalf("legacy per-user copy must be removed after migration: %+v err=%v", remaining, err)
	}

	// Every identity now resolves the same value: the owner, and an identity
	// with no record at all (the scheduler runs as the manifest's creator, but
	// resolution must not depend on who asks).
	for _, uid := range []string{"a1", "c3", "d4"} {
		got := api.loadSelectedSecrets(ctx, uid, sharedSecretsTestWorkflow, []string{"TOKEN"})
		if len(got) != 1 || got[0].Value != "legacy-value" {
			t.Fatalf("resolution as %s = %+v", uid, got)
		}
	}

	// A workflow-scoped value still wins over a same-named reusable user secret.
	personal, _ := encryptSecretValueWithAAD("personal-value", []byte("c3"))
	if err := api.chatStore.UpsertUserSecret(ctx, "c3", "TOKEN", personal); err != nil {
		t.Fatalf("seed user secret: %v", err)
	}
	got = api.loadSelectedSecrets(ctx, "c3", sharedSecretsTestWorkflow, []string{"TOKEN"})
	if len(got) != 1 || got[0].Value != "legacy-value" {
		t.Fatalf("workflow secret must take precedence over the user's reusable secret: %+v", got)
	}
}
