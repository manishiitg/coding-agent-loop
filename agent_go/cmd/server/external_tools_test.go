package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/agentworksclient"
	planops "github.com/manishiitg/coding-agent-loop/agent_go/pkg/orchestrator/agents/workflow/step_based_workflow"
	workspacehandlers "github.com/manishiitg/coding-agent-loop/workspace/handlers"
	"github.com/spf13/viper"
)

type externalToolsFixture struct {
	api           *StreamingAPI
	docs          string
	upstreamCalls atomic.Int64
}

func newExternalToolsFixture(t *testing.T) *externalToolsFixture {
	t.Helper()
	t.Setenv("MULTI_USER_MODE", "true")
	const workspaceToken = "external-tools-fixture-server-only-token"
	t.Setenv("WORKSPACE_API_TOKEN", workspaceToken)
	withMemoryUserDirectory(t, `{"users":[
 {"id":"owner","username":"owner","can_create":true,"products":["agentworks"]},
 {"id":"reader","username":"reader","can_create":false,"products":["agentworks"]},
 {"id":"readonly-owner","username":"readonly-owner","can_create":false,"products":["agentworks"]},
 {"id":"outsider","username":"outsider","can_create":true,"products":["agentworks"]}
 ]}`)
	f := &externalToolsFixture{api: &StreamingAPI{}, docs: t.TempDir()}
	t.Setenv("WORKSPACE_DOCS_PATH", f.docs)
	oldDocs := viper.Get("docs-dir")
	viper.Set("docs-dir", f.docs)
	t.Cleanup(func() { viper.Set("docs-dir", oldDocs) })
	f.write(t, "Workflow/invoices/workflow.json", `{"id":"invoices","label":"Invoice processing","created_by":"owner","access":{"owners":["owner","readonly-owner"],"readers":["reader"]},"capabilities":{"selected_servers":["accounting"]}}`)
	f.write(t, "Workflow/secret/workflow.json", `{"id":"secret","label":"Private research","created_by":"outsider","access":{"owners":["outsider"]}}`)
	f.write(t, "Workflow/invoices/planning/plan.json", `{"steps":[{"id":"fetch-invoices","type":"regular","title":"Fetch invoices","description":"Fetch invoices from accounting."}]}`)
	f.write(t, "Workflow/invoices/planning/step_config.json", `{"steps":[]}`)
	f.write(t, "Workflow/invoices/docs/process.md", "Invoices are reviewed weekly.\n")
	router := gin.New()
	router.POST("/api/workflow-files", func(c *gin.Context) {
		if c.GetHeader("X-Workspace-Token") != workspaceToken {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing server-only workspace token"})
			return
		}
		workspacehandlers.WorkflowFiles(c)
	})
	router.GET("/api/documents", func(c *gin.Context) {
		c.JSON(200, gin.H{"success": true, "data": []any{gin.H{"filepath": "Workflow", "type": "folder", "children": []any{gin.H{"filepath": "Workflow/invoices", "type": "folder"}, gin.H{"filepath": "Workflow/secret", "type": "folder"}}}}})
	})
	router.GET("/api/documents/*file", func(c *gin.Context) {
		rel := strings.TrimPrefix(c.Param("file"), "/")
		if rel != "Workflow/invoices/workflow.json" && rel != "Workflow/secret/workflow.json" {
			c.Status(404)
			return
		}
		data, err := os.ReadFile(filepath.Join(f.docs, filepath.FromSlash(rel)))
		if err != nil {
			c.Status(404)
			return
		}
		c.JSON(200, gin.H{"success": true, "data": gin.H{"content": string(data)}})
	})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { f.upstreamCalls.Add(1); router.ServeHTTP(w, r) }))
	t.Cleanup(upstream.Close)
	t.Setenv("WORKSPACE_API_URL", upstream.URL)
	return f
}
func (f *externalToolsFixture) write(t *testing.T, rel, content string) {
	t.Helper()
	p := filepath.Join(f.docs, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}
func (f *externalToolsFixture) read(t *testing.T, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(f.docs, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
func (f *externalToolsFixture) call(t *testing.T, user, name string, args map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	data, err := json.Marshal(map[string]any{"name": name, "arguments": args})
	if err != nil {
		t.Fatal(err)
	}
	var claims *UserClaims
	if user != "" {
		claims = &UserClaims{UserID: user, Username: user}
	}
	w := httptest.NewRecorder()
	f.api.handleExternalCall(w, adminRequest(http.MethodPost, "/api/external/call", string(data), claims, nil))
	return w
}
func externalTestBody(t *testing.T, w *httptest.ResponseRecorder, want int) map[string]any {
	t.Helper()
	if w.Code != want {
		t.Fatalf("status %d, want %d: %s", w.Code, want, w.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	return body
}
func (f *externalToolsFixture) planRevision(t *testing.T, user string) string {
	t.Helper()
	body := externalTestBody(t, f.call(t, user, "get_plan", map[string]any{"workflow_id": "invoices"}), 200)
	revision, _ := body["revision"].(string)
	if revision == "" {
		t.Fatal("missing plan revision")
	}
	return revision
}

func TestExternalToolsHTTPCatalogCompilesSchemasAndRequiresIdentity(t *testing.T) {
	api := &StreamingAPI{}
	for _, authenticated := range []bool{false, true} {
		var claims *UserClaims
		if authenticated {
			claims = &UserClaims{UserID: "catalog-reader"}
		}
		w := httptest.NewRecorder()
		api.handleExternalTools(w, adminRequest(http.MethodGet, "/api/external/tools", "", claims, nil))
		if !authenticated {
			externalTestBody(t, w, 401)
			continue
		}
		body := externalTestBody(t, w, 200)
		definitions, ok := body["tools"].([]any)
		if !ok || len(definitions) < 20 {
			t.Fatalf("missing catalog: %s", w.Body.String())
		}
		names := map[string]bool{}
		for _, raw := range definitions {
			tool := raw.(map[string]any)
			name := tool["name"].(string)
			if names[name] {
				t.Fatalf("duplicate %s", name)
			}
			names[name] = true
			schema := tool["inputSchema"].(map[string]any)
			if schema["additionalProperties"] != false {
				t.Fatalf("unknown fields permitted by %s", name)
			}
			if _, leaked := tool["validator"]; leaked {
				t.Fatal("private implementation field leaked")
			}
		}
		for _, name := range []string{"list_workflows", "get_plan", "update_scripted_step", "update_step_config", "read_file", "write_file", "patch_file", "builder_chat", "builder_status", "builder_cancel"} {
			if !names[name] {
				t.Fatalf("missing tool %s", name)
			}
		}
		for _, name := range []string{"run_full_workflow", "execute_step", "migrate_declared_execution_mode"} {
			if names[name] {
				t.Fatalf("internal tool exposed: %s", name)
			}
		}
	}
}

func TestExternalToolsHTTPPlanMutationPersistsAndRejectsStaleRevision(t *testing.T) {
	f := newExternalToolsFixture(t)
	revision := f.planRevision(t, "owner")
	args := map[string]any{"workflow_id": "invoices", "expected_revision": revision, "existing_step_id": "fetch-invoices", "title": "Fetch pending invoices", "reason": "Clarify pending invoice scope"}
	body := externalTestBody(t, f.call(t, "owner", "update_scripted_step", args), 200)
	changedRevision, _ := body["revision"].(string)
	if changedRevision == "" || changedRevision == revision {
		t.Fatalf("revision unchanged: %v", body)
	}
	data := f.read(t, "Workflow/invoices/planning/plan.json")
	var plan planops.PlanningResponse
	if err := json.Unmarshal([]byte(data), &plan); err != nil {
		t.Fatal(err)
	}
	if len(plan.Steps) != 1 || plan.Steps[0].GetTitle() != "Fetch pending invoices" {
		t.Fatalf("unexpected persisted plan %s", data)
	}
	matches, err := filepath.Glob(filepath.Join(f.docs, "Workflow/invoices/planning/changelog/*.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 {
		t.Fatalf("expected native changelog, found %v", matches)
	}
	raw, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	var changelog planops.PlanChangelog
	if err := json.Unmarshal(raw, &changelog); err != nil {
		t.Fatal(err)
	}
	if len(changelog.Entries) != 1 {
		t.Fatalf("unexpected changelog %s", raw)
	}
	entry := changelog.Entries[0]
	if entry.Tool != "update_scripted_step" || entry.Reason != "Clarify pending invoice scope" || entry.Origin.AgentName != "agentworks-api" || !strings.HasPrefix(entry.Origin.SessionID, "external-owner-") {
		t.Fatalf("missing native provenance %+v", entry)
	}
	if got := f.planRevision(t, "owner"); got != changedRevision {
		t.Fatalf("returned revision %s does not match fresh %s", changedRevision, got)
	}
	args["title"] = "Stale overwrite"
	stale := externalTestBody(t, f.call(t, "owner", "update_scripted_step", args), 409)
	if stale["error"].(map[string]any)["code"] != "revision_conflict" {
		t.Fatalf("wrong stale error %v", stale)
	}
	if got := f.read(t, "Workflow/invoices/planning/plan.json"); got != data {
		t.Fatal("stale request changed plan")
	}
	after, _ := os.ReadFile(matches[0])
	if string(after) != string(raw) {
		t.Fatal("stale request changed changelog")
	}
}

func TestExternalToolsHTTPWorkflowVisibilityAndReadonlyOwnerGuard(t *testing.T) {
	f := newExternalToolsFixture(t)
	body := externalTestBody(t, f.call(t, "owner", "list_workflows", nil), 200)
	workflows := body["workflows"].([]any)
	if len(workflows) != 1 || workflows[0].(map[string]any)["manifest"].(map[string]any)["id"] != "invoices" {
		t.Fatalf("unauthorized workflow visible: %v", body)
	}
	for _, tool := range []string{"get_workflow", "get_plan"} {
		externalTestBody(t, f.call(t, "owner", tool, map[string]any{"workflow_id": "secret"}), 404)
	}
	for _, user := range []string{"reader", "readonly-owner"} {
		revision := f.planRevision(t, user)
		externalTestBody(t, f.call(t, user, "update_scripted_step", map[string]any{"workflow_id": "invoices", "expected_revision": revision, "existing_step_id": "fetch-invoices", "title": "Denied", "reason": "Attempt change"}), 403)
		externalTestBody(t, f.call(t, user, "write_file", map[string]any{"workflow_id": "invoices", "path": "docs/new.md", "expected_revision": "missing", "content": "Denied"}), 403)
	}
	externalTestBody(t, f.call(t, "outsider", "get_plan", map[string]any{"workflow_id": "invoices"}), 404)
	externalTestBody(t, f.call(t, "", "list_workflows", nil), 401)
	if strings.Contains(f.read(t, "Workflow/invoices/planning/plan.json"), "Denied") {
		t.Fatal("unauthorized request changed plan")
	}
}

func TestExternalToolsHTTPRejectsUnknownArgumentsAndProtectsPlanFiles(t *testing.T) {
	f := newExternalToolsFixture(t)
	revision := f.planRevision(t, "owner")
	before := f.read(t, "Workflow/invoices/planning/plan.json")
	externalTestBody(t, f.call(t, "owner", "update_scripted_step", map[string]any{"workflow_id": "invoices", "expected_revision": revision, "existing_step_id": "fetch-invoices", "reason": "Attempt unknown field", "titel": "Misspelled"}), 400)
	externalTestBody(t, f.call(t, "owner", "read_file", map[string]any{"workflow_id": "invoices", "path": "docs/process.md", "managed": true}), 400)
	for _, tool := range []string{"write_file", "patch_file"} {
		for _, p := range []string{"planning/plan.json", "planning/step_config.json", "evaluation/evaluation_plan.json", "workflow.json"} {
			args := map[string]any{"workflow_id": "invoices", "path": p, "expected_revision": "missing"}
			if tool == "write_file" {
				args["content"] = "{}"
			} else {
				args["diff"] = "@@ -1 +1 @@\n-old\n+new\n"
			}
			body := externalTestBody(t, f.call(t, "owner", tool, args), 403)
			if body["error"].(map[string]any)["code"] != "protected_path" {
				t.Fatalf("wrong protected error: %v", body)
			}
		}
	}
	if f.read(t, "Workflow/invoices/planning/plan.json") != before {
		t.Fatal("invalid/protected request changed plan")
	}
}

func TestExternalToolsInternalFileEndpointCannotBeProxied(t *testing.T) {
	f := newExternalToolsFixture(t)
	proxy := workspaceProxyHandler()
	for _, target := range []string{"/api/wp/api/workflow-files", "/api/wp/api/workflow-files/", "/api/wp/api/nested/../workflow-files", "/api/wp//api//workflow-files", "/api/wp/api/nested/%2e%2e/workflow-files", "/api/wp/api/%77orkflow-files"} {
		t.Run(fmt.Sprintf("path=%s", target), func(t *testing.T) {
			before := f.upstreamCalls.Load()
			w := httptest.NewRecorder()
			proxy.ServeHTTP(w, adminRequest(http.MethodPost, target, `{"root":"Workflow/secret","operation":"commit","managed":true}`, &UserClaims{UserID: "owner"}, nil))
			if w.Code != 404 {
				t.Fatalf("internal endpoint proxy status %d: %s", w.Code, w.Body.String())
			}
			if f.upstreamCalls.Load() != before {
				t.Fatal("internal endpoint reached workspace service")
			}
		})
	}
}

// This uses the same transport client consumed by CLI and MCP against real
// authentication, external HTTP handlers, native plan tools, and disk-backed
// workspace operations. Only workflow discovery is stubbed by the fixture.
func TestExternalToolsClientTransportThroughJWTAndWorkspace(t *testing.T) {
	f := newExternalToolsFixture(t)
	t.Setenv("AUTH_SECRET", "external-tools-transport-auth-secret-for-tests")
	router := http.NewServeMux()
	router.HandleFunc("GET /api/external/v1/tools", f.api.handleExternalTools)
	router.HandleFunc("POST /api/external/v1/call", f.api.handleExternalCall)
	server := httptest.NewServer(AuthMiddleware(router))
	t.Cleanup(server.Close)

	token, err := GenerateJWT("owner", "owner", "")
	if err != nil {
		t.Fatal(err)
	}
	client, err := agentworksclient.New(server.URL, token)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	catalog, err := client.Tools(ctx)
	if err != nil {
		t.Fatalf("authenticated catalog: %v", err)
	}
	found := false
	for _, tool := range catalog {
		if tool.Name == "update_scripted_step" {
			found = true
		}
	}
	if !found {
		t.Fatal("native plan mutation missing from client catalog")
	}
	raw, err := client.Call(ctx, "get_plan", map[string]any{"workflow_id": "invoices"})
	if err != nil {
		t.Fatalf("get_plan: %v", err)
	}
	var before struct {
		Revision string `json:"revision"`
	}
	if err := json.Unmarshal(raw, &before); err != nil || before.Revision == "" {
		t.Fatalf("invalid get_plan result %s: %v", raw, err)
	}
	raw, err = client.Call(ctx, "update_scripted_step", map[string]any{
		"workflow_id": "invoices", "expected_revision": before.Revision,
		"existing_step_id": "fetch-invoices", "title": "Fetch invoices through authenticated client",
		"reason": "Clarify invoice fetching through the external client",
	})
	if err != nil {
		t.Fatalf("native update through authenticated transport: %v", err)
	}
	var updated struct {
		Revision string `json:"revision"`
	}
	if err := json.Unmarshal(raw, &updated); err != nil || updated.Revision == "" || updated.Revision == before.Revision {
		t.Fatalf("invalid mutation result %s: %v", raw, err)
	}
	var plan planops.PlanningResponse
	data := f.read(t, "Workflow/invoices/planning/plan.json")
	if err := json.Unmarshal([]byte(data), &plan); err != nil {
		t.Fatal(err)
	}
	if len(plan.Steps) != 1 || plan.Steps[0].GetTitle() != "Fetch invoices through authenticated client" {
		t.Fatalf("transport mutation did not persist: %s", data)
	}

	// Missing and expired credentials must fail before workflow discovery or
	// any workspace operation. The client sends a real bearer JWT when present.
	workspaceCalls := f.upstreamCalls.Load()
	missing, err := http.Get(server.URL + "/api/external/v1/tools")
	if err != nil {
		t.Fatal(err)
	}
	missing.Body.Close()
	if missing.StatusCode != http.StatusUnauthorized {
		t.Fatalf("missing JWT status = %d", missing.StatusCode)
	}
	expired, err := jwt.NewWithClaims(jwt.SigningMethodHS256, &UserClaims{
		UserID: "owner", Username: "owner", RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Minute)),
		},
	}).SignedString(GetAuthSecret())
	if err != nil {
		t.Fatal(err)
	}
	expiredClient, err := agentworksclient.New(server.URL, expired)
	if err != nil {
		t.Fatal(err)
	}
	_, err = expiredClient.Tools(ctx)
	var apiErr *agentworksclient.APIError
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusUnauthorized {
		t.Fatalf("expired JWT must return client APIError 401, got %v", err)
	}
	_, err = expiredClient.Call(ctx, "get_plan", map[string]any{"workflow_id": "invoices"})
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusUnauthorized {
		t.Fatalf("expired JWT call must return client APIError 401, got %v", err)
	}
	if f.upstreamCalls.Load() != workspaceCalls {
		t.Fatal("unauthenticated requests reached the workspace")
	}
}
