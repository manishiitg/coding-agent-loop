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
	"github.com/manishiitg/coding-agent-loop/agent_go/internal/agentworksproduct"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/agentworksclient"
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
	router.POST("/api/shared-assets", func(c *gin.Context) {
		if c.GetHeader("X-Workspace-Token") != workspaceToken {
			c.AbortWithStatus(401)
			return
		}
		workspacehandlers.SharedAssets(c)
	})
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
		if !ok || len(definitions) != 15 {
			t.Fatalf("unexpected catalog size %d: %s", len(definitions), w.Body.String())
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
		for _, name := range agentworksproduct.RunExternalTools() {
			if !names[name] {
				t.Fatalf("missing tool %s", name)
			}
		}
		// v1 is read-only: mutations and Builder execution stay out of the
		// catalog alongside the internal workshop tools.
		for _, name := range []string{"run_full_workflow", "execute_step", "migrate_declared_execution_mode", "write_file", "patch_file", "update_scripted_step", "update_step_config", "builder_chat", "builder_status", "builder_reply_input", "builder_cancel"} {
			if names[name] {
				t.Fatalf("write tool exposed: %s", name)
			}
		}
	}
}

func TestExternalCatalogMatchesProductYAMLAdmission(t *testing.T) {
	catalog, err := externalTools()
	if err != nil {
		t.Fatal(err)
	}
	admitted := agentworksproduct.RunExternalTools()
	if len(catalog) != len(admitted) {
		t.Fatalf("catalog has %d tools, product.yaml admits %d", len(catalog), len(admitted))
	}
	for i, tool := range catalog {
		if tool.Name != admitted[i] {
			t.Fatalf("catalog[%d] = %s, product.yaml admits %s", i, tool.Name, admitted[i])
		}
		if tool.mutates {
			t.Fatalf("admitted tool %s mutates: v1 is read-only", tool.Name)
		}
	}
	// Golden pin: changing the exposed surface means editing product.yaml and
	// this list together, deliberately.
	want := []string{"list_workflows", "get_workflow", "list_files", "search_files", "get_file_link", "read_file", "get_plan", "get_agent_context", "list_guidance_topics", "get_guidance_topic", "list_workflow_knowledge", "read_workflow_knowledge", "list_runs", "get_run", "get_logs"}
	if len(admitted) != len(want) {
		t.Fatalf("admitted %d tools, want %d", len(admitted), len(want))
	}
	for i, name := range want {
		if admitted[i] != name {
			t.Fatalf("admitted[%d] = %s, want %s", i, admitted[i], name)
		}
	}
}

func TestExternalToolsHTTPMutationsAreNotExposed(t *testing.T) {
	f := newExternalToolsFixture(t)
	before := f.read(t, "Workflow/invoices/planning/plan.json")
	revision := f.planRevision(t, "owner")
	calls := map[string]map[string]any{
		"update_scripted_step": {"workflow_id": "invoices", "expected_revision": revision, "existing_step_id": "fetch-invoices", "title": "Denied", "reason": "Attempt change"},
		"update_step_config":   {"workflow_id": "invoices", "expected_revision": revision, "step_id": "fetch-invoices", "reason": "Attempt change"},
		"write_file":           {"workflow_id": "invoices", "path": "docs/new.md", "expected_revision": "missing", "content": "Denied"},
		"patch_file":           {"workflow_id": "invoices", "path": "docs/process.md", "expected_revision": "anything", "diff": "@@ -1 +1 @@\n-old\n+new\n"},
		"builder_chat":         {"workflow_id": "invoices", "message": "Denied"},
		"builder_status":       {"workflow_id": "invoices", "session_id": "anything"},
		"builder_cancel":       {"workflow_id": "invoices", "session_id": "anything"},
	}
	for name, args := range calls {
		body := externalTestBody(t, f.call(t, "owner", name, args), 404)
		if body["error"].(map[string]any)["code"] != "unknown_tool" {
			t.Fatalf("wrong error for %s: %v", name, body)
		}
	}
	if f.read(t, "Workflow/invoices/planning/plan.json") != before {
		t.Fatal("unexposed mutation changed plan")
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
		externalTestBody(t, f.call(t, user, "get_plan", map[string]any{"workflow_id": "invoices"}), 200)
		externalTestBody(t, f.call(t, user, "read_file", map[string]any{"workflow_id": "invoices", "path": "docs/process.md"}), 200)
	}
	externalTestBody(t, f.call(t, "outsider", "get_plan", map[string]any{"workflow_id": "invoices"}), 404)
	externalTestBody(t, f.call(t, "", "list_workflows", nil), 401)
	if strings.Contains(f.read(t, "Workflow/invoices/planning/plan.json"), "Denied") {
		t.Fatal("unauthorized request changed plan")
	}
}

func TestExternalToolsHTTPRejectsUnknownArguments(t *testing.T) {
	f := newExternalToolsFixture(t)
	before := f.read(t, "Workflow/invoices/planning/plan.json")
	externalTestBody(t, f.call(t, "owner", "get_plan", map[string]any{"workflow_id": "invoices", "titel": "Misspelled"}), 400)
	externalTestBody(t, f.call(t, "owner", "read_file", map[string]any{"workflow_id": "invoices", "path": "docs/process.md", "managed": true}), 400)
	// Reads reach plan files; there is no write path to protect in v1.
	externalTestBody(t, f.call(t, "owner", "read_file", map[string]any{"workflow_id": "invoices", "path": "planning/plan.json"}), 200)
	if f.read(t, "Workflow/invoices/planning/plan.json") != before {
		t.Fatal("invalid request changed plan")
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
// authentication, external HTTP handlers, and disk-backed workspace
// operations. Only workflow discovery is stubbed by the fixture.
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
	found := map[string]bool{}
	for _, tool := range catalog {
		found[tool.Name] = true
	}
	for _, name := range []string{"get_plan", "read_file", "get_agent_context"} {
		if !found[name] {
			t.Fatalf("read tool %s missing from client catalog", name)
		}
	}
	for _, name := range []string{"update_scripted_step", "write_file", "builder_chat"} {
		if found[name] {
			t.Fatalf("write tool %s exposed in client catalog", name)
		}
	}
	raw, err := client.Call(ctx, "get_plan", map[string]any{"workflow_id": "invoices"})
	if err != nil {
		t.Fatalf("get_plan: %v", err)
	}
	var plan struct {
		Revision string `json:"revision"`
	}
	if err := json.Unmarshal(raw, &plan); err != nil || plan.Revision == "" {
		t.Fatalf("invalid get_plan result %s: %v", raw, err)
	}
	raw, err = client.Call(ctx, "read_file", map[string]any{"workflow_id": "invoices", "path": "docs/process.md"})
	if err != nil {
		t.Fatalf("read_file: %v", err)
	}
	var file struct {
		Content string `json:"content"`
	}
	if err := json.Unmarshal(raw, &file); err != nil || file.Content == "" {
		t.Fatalf("invalid read result %s: %v", raw, err)
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
