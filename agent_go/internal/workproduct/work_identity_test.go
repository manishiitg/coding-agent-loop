package workproduct

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/agentprofiles"
)

func TestWorkIdentityToolPersistsAndPromptVariablesReloadIt(t *testing.T) {
	const projectPath = "Chats/Work/projects/demo"
	manifest := `{"schema_version":1,"product":"work","id":"demo","title":"Demo","updated_at":"old","capabilities":{"selected_servers":[]}}`
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/documents/"+projectPath+"/product.json" {
			http.Error(w, "unexpected path", http.StatusNotFound)
			return
		}
		mu.Lock()
		defer mu.Unlock()
		switch r.Method {
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"success": true,
				"data":    map[string]interface{}{"filepath": projectPath + "/product.json", "content": manifest},
			})
		case http.MethodPut:
			body, _ := io.ReadAll(r.Body)
			var payload struct {
				Content string `json:"content"`
			}
			if err := json.Unmarshal(body, &payload); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			manifest = payload.Content
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": true})
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	defer server.Close()

	registry := agentprofiles.NewRegistry()
	if err := RegisterAgentProfileRuntime(registry, server.URL); err != nil {
		t.Fatalf("register Work runtime: %v", err)
	}
	var emitted []map[string]interface{}
	tool, err := registry.BuildTool(agentprofiles.ToolBinding{ID: "work.set-identity"}, agentprofiles.ToolRuntimeContext{
		UserID: "user-1", SessionID: "session-1", WorkspacePath: projectPath,
		Emit: func(event any) {
			if payload, ok := event.(map[string]interface{}); ok {
				emitted = append(emitted, payload)
			}
		},
	})
	if err != nil {
		t.Fatalf("build Work identity tool: %v", err)
	}
	result, err := tool.Execute(context.Background(), map[string]interface{}{
		"operation": "set",
		"icon":      "🛠️",
		"name":      "Nova",
		"role":      "Engineering partner",
	})
	if err != nil {
		t.Fatalf("set Work identity: %v", err)
	}
	if !strings.Contains(result, "Adopt it immediately") || !strings.Contains(result, "Name: Nova") {
		t.Fatalf("unexpected tool result: %q", result)
	}

	mu.Lock()
	var saved map[string]interface{}
	if err := json.Unmarshal([]byte(manifest), &saved); err != nil {
		mu.Unlock()
		t.Fatalf("decode saved manifest: %v", err)
	}
	mu.Unlock()
	if saved["title"] != "Demo" || saved["updated_at"] == "old" {
		t.Fatalf("project metadata was not preserved/updated: %+v", saved)
	}
	identity, _ := saved["identity"].(map[string]interface{})
	if identity["icon"] != "🛠️" || identity["name"] != "Nova" || identity["role"] != "Engineering partner" {
		t.Fatalf("identity was not saved: %+v", identity)
	}
	if len(emitted) != 1 || emitted[0]["type"] != "work_identity_updated" {
		t.Fatalf("identity update event was not emitted: %+v", emitted)
	}

	variables, err := registry.PromptVariables(context.Background(), "work", agentprofiles.RuntimeContext{
		UserID: "user-1", SessionID: "session-1", WorkspacePath: projectPath,
	})
	if err != nil {
		t.Fatalf("load Work prompt variables: %v", err)
	}
	if got := variables["WORK_IDENTITY"]; !strings.Contains(got, "Icon: 🛠️") || !strings.Contains(got, "Name: Nova") || !strings.Contains(got, "Role: Engineering partner") {
		t.Fatalf("prompt identity was not reloaded: %q", got)
	}

	if _, err := tool.Execute(context.Background(), map[string]interface{}{"operation": "clear"}); err != nil {
		t.Fatalf("clear Work identity: %v", err)
	}
	variables, err = registry.PromptVariables(context.Background(), "work", agentprofiles.RuntimeContext{
		UserID: "user-1", SessionID: "session-1", WorkspacePath: projectPath,
	})
	if err != nil {
		t.Fatalf("reload cleared Work prompt variables: %v", err)
	}
	if variables["WORK_IDENTITY"] != "" {
		t.Fatalf("identity should be empty after clear: %q", variables["WORK_IDENTITY"])
	}
}

func TestWorkIdentityStaysCompact(t *testing.T) {
	identity := workIdentity{
		Icon:         strings.Repeat("x", workIdentityIconLimit+1),
		Name:         "Nova",
		Role:         "Assistant",
		Instructions: "Be concise.",
	}
	if got := validateWorkIdentity(identity); got != "The identity icon must be at most 8 characters." {
		t.Fatalf("unexpected icon validation: %q", got)
	}

	identity.Icon = "N"
	identity.Instructions = strings.Repeat("x", workIdentityInstructionsLimit+1)
	if got := validateWorkIdentity(identity); got != "The identity instructions must be at most 500 characters." {
		t.Fatalf("unexpected instructions validation: %q", got)
	}
}
