package chathistory

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func newWorkspaceAPITestServer(t *testing.T, initialFiles map[string]string) *httptest.Server {
	t.Helper()

	var mu sync.Mutex
	files := make(map[string]string, len(initialFiles))
	for k, v := range initialFiles {
		files[k] = v
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/api/documents/")
		path = strings.ReplaceAll(path, "%2F", "/")
		switch r.Method {
		case http.MethodGet:
			mu.Lock()
			content, ok := files[path]
			mu.Unlock()
			if !ok {
				_ = json.NewEncoder(w).Encode(map[string]any{
					"success": true,
					"message": "File does not exist",
					"data":    map[string]any{},
					"error":   "File not found",
				})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"success": true,
				"message": "Document retrieved successfully",
				"data": map[string]any{
					"filepath": path,
					"content":  content,
				},
			})
		case http.MethodPut:
			var req struct {
				Content string `json:"content"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			mu.Lock()
			files[path] = req.Content
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{
				"success": true,
				"message": "Document updated successfully",
				"data": map[string]any{
					"filepath": path,
				},
			})
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})

	return httptest.NewServer(handler)
}

func TestWorkspaceAPIStoreBotConnectorConfigRoundTrip(t *testing.T) {
	server := newWorkspaceAPITestServer(t, nil)
	defer server.Close()

	store, err := NewWorkspaceAPIStore(server.URL)
	if err != nil {
		t.Fatalf("NewWorkspaceAPIStore() error = %v", err)
	}

	_, err = store.UpsertBotConnectorConfig(context.Background(), &CreateBotConnectorConfigRequest{
		ID:              "slack",
		Enabled:         true,
		BotMode:         true,
		ConfigJSON:      `{"token":"abc"}`,
		DefaultPresetID: "preset-1",
		AutoConfirm:     true,
		AllowedChannels: `["ops"]`,
	})
	if err != nil {
		t.Fatalf("UpsertBotConnectorConfig() error = %v", err)
	}

	cfg, err := store.GetBotConnectorConfig(context.Background(), "slack")
	if err != nil {
		t.Fatalf("GetBotConnectorConfig() error = %v", err)
	}
	if cfg.ConfigJSON != `{"token":"abc"}` {
		t.Fatalf("GetBotConnectorConfig().ConfigJSON = %q, want %q", cfg.ConfigJSON, `{"token":"abc"}`)
	}

	configs, err := store.ListBotConnectorConfigs(context.Background())
	if err != nil {
		t.Fatalf("ListBotConnectorConfigs() error = %v", err)
	}
	if len(configs) != 1 || configs[0].ID != "slack" {
		t.Fatalf("ListBotConnectorConfigs() = %+v, want one slack config", configs)
	}
}

func TestWorkspaceAPIStoreMigratesLegacyBotConfigFile(t *testing.T) {
	legacyJSON := `{"discord":{"id":"discord","enabled":true,"bot_mode":false,"config_json":"{}","created_at":"2026-04-13T00:00:00Z","updated_at":"2026-04-13T00:00:00Z"}}`
	server := newWorkspaceAPITestServer(t, map[string]string{
		workspaceAPIBotConfigLegacyFile: legacyJSON,
	})
	defer server.Close()

	store, err := NewWorkspaceAPIStore(server.URL)
	if err != nil {
		t.Fatalf("NewWorkspaceAPIStore() error = %v", err)
	}

	cfg, err := store.GetBotConnectorConfig(context.Background(), "discord")
	if err != nil {
		t.Fatalf("GetBotConnectorConfig() error = %v", err)
	}
	if cfg.ID != "discord" {
		t.Fatalf("GetBotConnectorConfig().ID = %q, want discord", cfg.ID)
	}
}

func TestWorkspaceAPIStoreUserSecretsRoundTrip(t *testing.T) {
	server := newWorkspaceAPITestServer(t, nil)
	defer server.Close()

	store, err := NewWorkspaceAPIStore(server.URL)
	if err != nil {
		t.Fatalf("NewWorkspaceAPIStore() error = %v", err)
	}

	ctx := context.Background()
	if err := store.UpsertUserSecret(ctx, "alice", "SLACK_TOKEN", "ciphertext-1"); err != nil {
		t.Fatalf("UpsertUserSecret(create) error = %v", err)
	}
	if err := store.UpsertUserSecret(ctx, "alice", "SLACK_TOKEN", "ciphertext-2"); err != nil {
		t.Fatalf("UpsertUserSecret(update) error = %v", err)
	}
	if err := store.UpsertUserSecret(ctx, "alice", "GITHUB_TOKEN", "ciphertext-3"); err != nil {
		t.Fatalf("UpsertUserSecret(second) error = %v", err)
	}

	secrets, err := store.ListUserSecrets(ctx, "alice")
	if err != nil {
		t.Fatalf("ListUserSecrets() error = %v", err)
	}
	if len(secrets) != 2 {
		t.Fatalf("ListUserSecrets() len = %d, want 2", len(secrets))
	}
	if secrets[1].EncryptedValue != "ciphertext-2" {
		t.Fatalf("updated secret ciphertext = %q, want ciphertext-2", secrets[1].EncryptedValue)
	}

	if err := store.DeleteUserSecret(ctx, "alice", "GITHUB_TOKEN"); err != nil {
		t.Fatalf("DeleteUserSecret() error = %v", err)
	}
	secrets, err = store.ListUserSecrets(ctx, "alice")
	if err != nil {
		t.Fatalf("ListUserSecrets(after delete) error = %v", err)
	}
	if len(secrets) != 1 || secrets[0].Name != "SLACK_TOKEN" {
		t.Fatalf("ListUserSecrets(after delete) = %+v, want one SLACK_TOKEN secret", secrets)
	}
}

func TestWorkspaceAPIStoreWorkflowSecretsRoundTrip(t *testing.T) {
	server := newWorkspaceAPITestServer(t, nil)
	defer server.Close()

	store, err := NewWorkspaceAPIStore(server.URL)
	if err != nil {
		t.Fatalf("NewWorkspaceAPIStore() error = %v", err)
	}

	ctx := context.Background()
	const workflowA = "Workflow/customer-renewals"
	const workflowB = "Workflow/support-triage"

	if err := store.UpsertWorkflowSecret(ctx, "alice", workflowA, "CRM_TOKEN", "ciphertext-a1"); err != nil {
		t.Fatalf("UpsertWorkflowSecret(create) error = %v", err)
	}
	if err := store.UpsertWorkflowSecret(ctx, "alice", workflowA, "CRM_TOKEN", "ciphertext-a2"); err != nil {
		t.Fatalf("UpsertWorkflowSecret(update) error = %v", err)
	}
	if err := store.UpsertWorkflowSecret(ctx, "alice", workflowB, "CRM_TOKEN", "ciphertext-b1"); err != nil {
		t.Fatalf("UpsertWorkflowSecret(second workflow) error = %v", err)
	}

	secretsA, err := store.ListWorkflowSecrets(ctx, "alice", workflowA)
	if err != nil {
		t.Fatalf("ListWorkflowSecrets(workflowA) error = %v", err)
	}
	if len(secretsA) != 1 || secretsA[0].Name != "CRM_TOKEN" || secretsA[0].EncryptedValue != "ciphertext-a2" {
		t.Fatalf("ListWorkflowSecrets(workflowA) = %+v, want updated CRM_TOKEN", secretsA)
	}
	if secretsA[0].WorkflowPath != workflowA {
		t.Fatalf("WorkflowPath = %q, want %q", secretsA[0].WorkflowPath, workflowA)
	}

	secretsB, err := store.ListWorkflowSecrets(ctx, "alice", workflowB)
	if err != nil {
		t.Fatalf("ListWorkflowSecrets(workflowB) error = %v", err)
	}
	if len(secretsB) != 1 || secretsB[0].EncryptedValue != "ciphertext-b1" {
		t.Fatalf("ListWorkflowSecrets(workflowB) = %+v, want isolated ciphertext-b1", secretsB)
	}

	if err := store.DeleteWorkflowSecret(ctx, "alice", workflowA, "CRM_TOKEN"); err != nil {
		t.Fatalf("DeleteWorkflowSecret() error = %v", err)
	}
	secretsA, err = store.ListWorkflowSecrets(ctx, "alice", workflowA)
	if err != nil {
		t.Fatalf("ListWorkflowSecrets(after delete) error = %v", err)
	}
	if len(secretsA) != 0 {
		t.Fatalf("ListWorkflowSecrets(after delete) = %+v, want none", secretsA)
	}
}
