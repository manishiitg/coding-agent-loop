package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/accesstokens"
)

func tokenTestSetup(t *testing.T) *StreamingAPI {
	t.Helper()
	t.Setenv("AGENTWORKS_STATE_ROOT", filepath.Join(t.TempDir(), "state"))
	t.Setenv("MULTI_USER_MODE", "false")
	t.Setenv("AUTH_SECRET", "token-test-signing-secret")
	return &StreamingAPI{}
}
func TestAccessTokenHTTPManagementAndRestrictions(t *testing.T) {
	api := tokenTestSetup(t)
	jwt, err := GenerateJWT(GetDefaultUserID(), "owner", "")
	if err != nil {
		t.Fatal(err)
	}
	router := mux.NewRouter()
	router.HandleFunc("/api/auth/access-tokens", api.handleAccessTokens).Methods("GET", "POST")
	router.HandleFunc("/api/auth/access-tokens/{id}", api.handleAccessTokens).Methods("DELETE")
	router.HandleFunc("/api/external/v1/tools", api.handleExternalTools)
	router.HandleFunc("/api/external/v1/call", api.handleExternalCall)
	handler := AuthMiddleware(router)
	request := func(method, path, body, token string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	issued := request("POST", "/api/auth/access-tokens", `{"name":"Local CLI","scopes":["workflows:read","files:read"],"all_workflows":true,"expires_in_days":30}`, jwt)
	if issued.Code != 201 {
		t.Fatal(issued.Code, issued.Body)
	}
	if issued.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("token response may be cached")
	}
	var created struct {
		Token    string             `json:"token"`
		Metadata accesstokens.Token `json:"access_token"`
	}
	json.Unmarshal(issued.Body.Bytes(), &created)
	if created.Token == "" {
		t.Fatal("missing token")
	}
	list := request("GET", "/api/auth/access-tokens", "", jwt)
	if list.Code != 200 || strings.Contains(list.Body.String(), created.Token) {
		t.Fatal("token disclosed in list")
	}
	catalog := request("GET", "/api/external/v1/tools", "", created.Token)
	if catalog.Code != 200 || !strings.Contains(catalog.Body.String(), `"read_file"`) || strings.Contains(catalog.Body.String(), `"write_file"`) || strings.Contains(catalog.Body.String(), `"builder_chat"`) {
		t.Fatal(catalog.Code, catalog.Body)
	}
	for _, path := range []string{"/api/auth/access-tokens", "/api/auth/password", "/api/wp/api/documents", "/api/query"} {
		w := request("POST", path, `{}`, created.Token)
		if w.Code != 403 {
			t.Fatal("PAT escaped external API", path, w.Code)
		}
	}
	denied := request("POST", "/api/external/v1/call", `{"name":"write_file","arguments":{}}`, created.Token)
	if denied.Code != 403 || !strings.Contains(denied.Body.String(), "insufficient_scope") {
		t.Fatal(denied.Code, denied.Body)
	}
	query := httptest.NewRequest("GET", "/api/external/v1/tools?token="+created.Token, nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, query)
	if w.Code != 401 {
		t.Fatal("PAT accepted from URL")
	}
	revoked := request("DELETE", "/api/auth/access-tokens/"+created.Metadata.ID, "", jwt)
	if revoked.Code != 204 {
		t.Fatal(revoked.Code, revoked.Body)
	}
	if w = request("GET", "/api/external/v1/tools", "", created.Token); w.Code != 401 {
		t.Fatal("revocation not enforced", w.Code)
	}
}
func TestAccessTokenIdentityFailsClosed(t *testing.T) {
	tokenTestSetup(t)
	t.Setenv("MULTI_USER_MODE", "true")
	content := withMemoryUserDirectory(t, `{"users":[{"id":"owner","username":"owner","can_create":true}]}`)
	token := accesstokens.Token{UserID: "owner", Username: "owner"}
	if _, err := accessTokenClaims(token); err != nil {
		t.Fatal(err)
	}
	*content = `{"users":[{"id":"owner","username":"owner","disabled":true}]}`
	if _, err := accessTokenClaims(token); err == nil {
		t.Fatal("disabled account accepted")
	}
	*content = `{"users":[]}`
	if _, err := accessTokenClaims(token); err == nil {
		t.Fatal("removed account accepted")
	}
	*content = `broken`
	if _, err := accessTokenClaims(token); err == nil {
		t.Fatal("directory failure accepted")
	}
}
func TestAccessTokenCannotInheritOtherBuilderSession(t *testing.T) {
	api := tokenTestSetup(t)
	claims := &UserClaims{UserID: "owner", AccessToken: &accesstokens.Token{ID: "one", AllWorkflows: true, Scopes: accesstokens.Scopes}}
	for _, sid := range []string{"browser-session", "pat-two-session"} {
		r := httptest.NewRequest("POST", "/", nil)
		r = r.WithContext(context.WithValue(r.Context(), UserContextKey, claims))
		w := httptest.NewRecorder()
		api.externalBuilderCall(w, r, "builder_status", map[string]interface{}{"session_id": sid}, DiscoveredWorkflow{WorkspacePath: "Workflow/test"})
		if w.Code != 404 {
			t.Fatal("PAT inherited a session", w.Code)
		}
	}
}
func TestAccessTokenStateOutsideWorkspace(t *testing.T) {
	tokenTestSetup(t)
	root := t.TempDir()
	t.Setenv("WORKSPACE_DOCS_PATH", root)
	t.Setenv("AGENTWORKS_STATE_ROOT", filepath.Join(root, "state"))
	if store, err := openAccessTokens(); err == nil {
		store.Close()
		t.Fatal("token store exposed in workspace")
	}
}
func TestAccessTokenScopesNeverTrustJWTJSON(t *testing.T) {
	var claims UserClaims
	if err := json.Unmarshal([]byte(`{"user_id":"owner","AccessToken":{"AllWorkflows":true},"access_token":{}}`), &claims); err != nil {
		t.Fatal(err)
	}
	if claims.AccessToken != nil {
		t.Fatal("claims deserialized PAT authority")
	}
}
func TestAccessTokenExpiry(t *testing.T) {
	api := tokenTestSetup(t)
	store, err := openAccessTokens()
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	old := time.Now().Add(-2 * time.Hour)
	_, raw, err := store.Issue(context.Background(), accesstokens.Token{Name: "expired", UserID: GetDefaultUserID(), AllWorkflows: true, Scopes: []string{"workflows:read"}, ExpiresAt: old.Add(time.Hour)}, old)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "/api/external/v1/tools", nil)
	r.Header.Set("Authorization", "Bearer "+raw)
	w := httptest.NewRecorder()
	AuthMiddleware(http.HandlerFunc(api.handleExternalTools)).ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatal("expired PAT accepted", w.Code)
	}
}

func TestAccessTokenWorkflowFilterAndAccountIntersection(t *testing.T) {
	tokenTestSetup(t)
	f := newExternalToolsFixture(t)
	// Give owner ordinary access to both workflows; the PAT narrows this to one.
	f.write(t, "Workflow/secret/workflow.json", `{"id":"secret","label":"Private research","access":{"owners":["owner"]}}`)
	claims := &UserClaims{UserID: "owner", Username: "owner", AccessToken: &accesstokens.Token{Scopes: []string{"workflows:read", "plan:write"}, WorkflowIDs: []string{"invoices"}}}
	call := func(name string, args map[string]any) *httptest.ResponseRecorder {
		body, _ := json.Marshal(map[string]any{"name": name, "arguments": args})
		w := httptest.NewRecorder()
		f.api.handleExternalCall(w, adminRequest("POST", "/api/external/v1/call", string(body), claims, nil))
		return w
	}
	w := call("list_workflows", map[string]any{})
	if w.Code != 200 || strings.Contains(w.Body.String(), `"secret"`) {
		t.Fatal("workflow scope ignored", w.Code, w.Body)
	}
	w = call("get_workflow", map[string]any{"workflow_id": "secret"})
	if w.Code != 404 {
		t.Fatal("out-of-scope workflow accessible", w.Code)
	}
	claims.UserID = "reader"
	claims.Username = "reader"
	w = call("update_scripted_step", map[string]any{"workflow_id": "invoices", "expected_revision": "anything", "existing_step_id": "fetch-invoices", "title": "no", "reason": "must fail"})
	if w.Code != 403 {
		t.Fatal("token elevated read-only account", w.Code, w.Body)
	}
}

func TestAccessTokenRuntimeLeaseAndCancellation(t *testing.T) {
	tokenTestSetup(t)
	f := newExternalToolsFixture(t)
	store, err := openAccessTokens()
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Now()
	token, _, err := store.Issue(context.Background(), accesstokens.Token{Name: "Builder", UserID: "owner", Username: "owner", AllWorkflows: true, Scopes: accesstokens.Scopes, ExpiresAt: now.Add(time.Hour)}, now)
	if err != nil {
		t.Fatal(err)
	}
	entry := accessTokenSession{token.ID, "pat-test", "Workflow/invoices", WorkflowAccessOwner}
	if !accessTokenSessionAllowed(context.Background(), entry) {
		t.Fatal("valid runtime lease denied")
	}
	if err = store.Revoke(context.Background(), token.ID, "owner", now); err != nil {
		t.Fatal(err)
	}
	if accessTokenSessionAllowed(context.Background(), entry) {
		t.Fatal("revoked runtime lease accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f.api.stoppedSessions = map[string]bool{}
	f.api.sessionBusy = map[string]bool{}
	f.api.agentCancelFuncs = map[string]context.CancelFunc{"pat-test": cancel}
	f.api.accessTokenSessions.sessions = map[string]accessTokenSession{"pat-test": entry}
	f.api.cancelAccessTokenSessions(token.ID)
	if ctx.Err() == nil {
		t.Fatal("revocation did not cancel active turn")
	}
	if !f.api.isSessionMarkedStopped("pat-test") {
		t.Fatal("runtime not stopped against new work")
	}
}
