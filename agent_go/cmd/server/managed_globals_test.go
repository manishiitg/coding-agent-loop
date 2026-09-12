package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func managedGlobalTestAPI(t *testing.T) *StreamingAPI {
	t.Helper()
	api, _ := sharedSecretsTestAPI(t)
	withMemoryUserDirectory(t, `{"users":[{"id":"admin","username":"admin","admin":true,"can_create":true,"products":[]},{"id":"a1","username":"owner","can_create":true,"products":[]},{"id":"c3","username":"reader","can_create":false,"products":[]}]}`)
	previous := managedGlobals
	managedGlobals = map[string]string{}
	t.Cleanup(func() { managedGlobals = previous })
	return api
}

func TestManagedGlobalPromotionPermissionsPersistenceAndResolution(t *testing.T) {
	api := managedGlobalTestAPI(t)
	ctx := context.Background()
	const name = "PROMOTED_TOKEN"
	const value = "sensitive-test-value"
	if err := api.upsertSharedWorkflowSecret(ctx, sharedSecretsTestWorkflow, name, value); err != nil {
		t.Fatal(err)
	}
	for _, uid := range []string{"a1", "c3"} {
		rec := httptest.NewRecorder()
		api.handleManageGlobalSecret(rec, sharedSecretsRequest(http.MethodPost, "/api/secrets/global", uid, map[string]string{"name": name, "workspace_path": sharedSecretsTestWorkflow}))
		if rec.Code != http.StatusForbidden {
			t.Fatalf("%s status %d", uid, rec.Code)
		}
	}
	rec := httptest.NewRecorder()
	api.handleManageGlobalSecret(rec, sharedSecretsRequest(http.MethodPost, "/api/secrets/global", "admin", map[string]string{"name": name, "workspace_path": sharedSecretsTestWorkflow}))
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), value) {
		t.Fatalf("promotion: %d %s", rec.Code, rec.Body.String())
	}
	source, err := api.ensureSharedWorkflowSecrets(ctx, sharedSecretsTestWorkflow, "admin")
	if err != nil || len(source) != 0 {
		t.Fatalf("source still contains a local copy: %v %v", source, err)
	}
	stored, err := api.chatStore.ListUserSecrets(ctx, managedGlobalSecretsUserID)
	if err != nil || len(stored) != 1 || strings.Contains(stored[0].EncryptedValue, value) {
		t.Fatalf("expected encrypted global persistence")
	}
	managedGlobals = map[string]string{}
	if err := api.loadManagedGlobalSecrets(ctx); err != nil {
		t.Fatal(err)
	}
	selected := api.loadSelectedSecrets(ctx, "a1", sharedSecretsTestWorkflow, []string{name})
	if len(selected) != 1 || selected[0].Value != value {
		t.Fatal("source attachment did not survive promotion/reload")
	}
	names := []string{name}
	merged := mergeGlobalSecrets(nil, &names)
	if len(merged) != 1 || merged[0].Value != value {
		t.Fatal("other workflows cannot resolve selected global")
	}
	rec = httptest.NewRecorder()
	api.handleGetGlobalSecrets(rec, sharedSecretsRequest(http.MethodGet, "/api/secrets/global", "c3", nil))
	if strings.Contains(rec.Body.String(), value) || !strings.Contains(rec.Body.String(), `"managed":true`) {
		t.Fatal("list must expose metadata only")
	}
	if err := api.saveManagedGlobalSecret(ctx, "admin", name, "rotated-value", false); err != nil {
		t.Fatal(err)
	}
	selected = api.loadSelectedSecrets(ctx, "a1", sharedSecretsTestWorkflow, []string{name})
	if len(selected) != 1 || selected[0].Value != "rotated-value" {
		t.Fatal("source did not receive global rotation")
	}
	if err := api.deleteManagedGlobalSecret(ctx, "a1", name); !errors.Is(err, errGlobalAdmin) {
		t.Fatal("owner can delete global")
	}
	if err := api.deleteManagedGlobalSecret(ctx, "admin", name); err != nil {
		t.Fatal(err)
	}
	if err := api.loadManagedGlobalSecrets(ctx); err != nil {
		t.Fatal(err)
	}
	if _, exists := managedGlobals[name]; exists {
		t.Fatal("deleted global survived reload")
	}
}

func TestManagedGlobalConflictsDoNotRemoveSource(t *testing.T) {
	api := managedGlobalTestAPI(t)
	ctx := context.Background()
	if err := api.upsertSharedWorkflowSecret(ctx, sharedSecretsTestWorkflow, "COLLISION", "source"); err != nil {
		t.Fatal(err)
	}
	if err := api.saveManagedGlobalSecret(ctx, "admin", "COLLISION", "original", false); err != nil {
		t.Fatal(err)
	}
	if err := api.promoteWorkflowSecret(ctx, "admin", sharedSecretsTestWorkflow, "COLLISION"); !errors.Is(err, errGlobalConflict) {
		t.Fatalf("conflict: %v", err)
	}
	source, _ := api.ensureSharedWorkflowSecrets(ctx, sharedSecretsTestWorkflow, "admin")
	if len(source) != 1 || managedGlobals["COLLISION"] != "original" {
		t.Fatal("conflict changed a secret")
	}
	previous := globalSecrets
	globalSecrets = append(globalSecrets, globalSecretEntry{Name: "ENV_LOCKED", Value: "environment"})
	t.Cleanup(func() { globalSecrets = previous })
	if err := api.saveManagedGlobalSecret(ctx, "admin", "ENV_LOCKED", "replacement", false); err == nil {
		t.Fatal("environment global overwritten")
	}
	if err := api.deleteManagedGlobalSecret(ctx, "admin", "ENV_LOCKED"); err == nil {
		t.Fatal("environment global deleted")
	}
}

func TestManagedGlobalToolGateAndNoValueOutput(t *testing.T) {
	api := managedGlobalTestAPI(t)
	ordinary := &recordingRegistrar{}
	if err := api.registerSecretManagementTools(ordinary, "a1", sharedSecretsTestWorkflow, "secrets", false, nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, exists := ordinary.tools["manage_global_secret"]; exists {
		t.Fatal("non-admin sees global mutation tool")
	}
	admin := &recordingRegistrar{}
	if err := api.registerSecretManagementTools(admin, "admin", sharedSecretsTestWorkflow, "secrets", false, nil, nil); err != nil {
		t.Fatal(err)
	}
	tool, exists := admin.tools["manage_global_secret"]
	if !exists {
		t.Fatal("admin tool missing")
	}
	output, err := tool.exec(context.Background(), map[string]interface{}{"action": "set", "name": "TOOL_TOKEN", "value": "private-value"})
	if err != nil || strings.Contains(output, "private-value") {
		t.Fatalf("unsafe tool output: %v", err)
	}
	withMemoryUserDirectory(t, `{"users":[{"id":"admin","username":"admin","can_create":true,"products":[]}]}`)
	_, err = tool.exec(context.Background(), map[string]interface{}{"action": "delete", "name": "TOOL_TOKEN"})
	if !errors.Is(err, errGlobalAdmin) {
		t.Fatal("demoted admin retained tool authority")
	}
}

func TestManagedGlobalRejectsUnreadableCiphertext(t *testing.T) {
	api := managedGlobalTestAPI(t)
	request := sharedSecretsRequest(http.MethodPut, "/api/secrets/global", "admin", map[string]string{"name": "WRONG", "encrypted_value": "invalid"})
	rec := httptest.NewRecorder()
	api.handleManageGlobalSecret(rec, request)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d", rec.Code)
	}
}

func TestManagedGlobalToolPromotesFromAnotherWorkflow(t *testing.T) {
	api := managedGlobalTestAPI(t)
	ctx := context.Background()
	const name = "CROSS_WORKFLOW_TOKEN"
	const value = "private-source-value"
	if err := api.upsertSharedWorkflowSecret(ctx, sharedSecretsTestWorkflow, name, value); err != nil {
		t.Fatal(err)
	}
	registrar := &recordingRegistrar{}
	if err := api.registerSecretManagementTools(registrar, "admin", "Workflow/destination", "secrets", false, nil, nil); err != nil {
		t.Fatal(err)
	}
	list := registrar.tools["list_secrets"]
	promote := registrar.tools["manage_global_secret"]
	output, err := list.exec(ctx, map[string]interface{}{"source_workflow_path": sharedSecretsTestWorkflow})
	if err != nil || !strings.Contains(output, name) || strings.Contains(output, value) {
		t.Fatalf("source listing failed or exposed value: %v", err)
	}
	for _, invalid := range []interface{}{"", "Workflow/missing", "Workflow/renewals/../destination", 123} {
		args := map[string]interface{}{"action": "promote", "name": name, "source_workflow_path": invalid}
		if _, err := promote.exec(ctx, args); err == nil {
			t.Fatalf("accepted invalid source %v", invalid)
		}
		if _, err := list.exec(ctx, args); err == nil {
			t.Fatalf("listed invalid source %v", invalid)
		}
	}
	if _, err := promote.exec(ctx, map[string]interface{}{"action": "promote", "name": name}); err == nil {
		t.Fatal("omitted source unexpectedly found another workflow's secret")
	}
	output, err = promote.exec(ctx, map[string]interface{}{"action": "promote", "name": name, "source_workflow_path": sharedSecretsTestWorkflow})
	if err != nil || strings.Contains(output, value) {
		t.Fatalf("cross-workflow promotion failed or exposed value: %v", err)
	}
	selected := api.loadSelectedSecrets(ctx, "a1", sharedSecretsTestWorkflow, []string{name})
	if len(selected) != 1 || selected[0].Value != value {
		t.Fatal("source attachment broken")
	}
	names := []string{name}
	if resolved := mergeGlobalSecrets(nil, &names); len(resolved) != 1 || resolved[0].Value != value {
		t.Fatal("destination cannot use promoted global")
	}
	withMemoryUserDirectory(t, `{"users":[{"id":"admin","username":"admin","can_create":true,"products":[]}]}`)
	if _, err := list.exec(ctx, map[string]interface{}{"source_workflow_path": sharedSecretsTestWorkflow}); !errors.Is(err, errGlobalAdmin) {
		t.Fatal("demoted admin could list another source")
	}
	if _, err := promote.exec(ctx, map[string]interface{}{"action": "promote", "name": name, "source_workflow_path": sharedSecretsTestWorkflow}); !errors.Is(err, errGlobalAdmin) {
		t.Fatal("demoted admin could promote from another source")
	}
}
