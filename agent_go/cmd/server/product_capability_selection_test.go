package server

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProductSelectedSecretsUsesSharedCapabilityContractAndPreservesManifest(t *testing.T) {
	const workspacePath = "_users/user-1/Chats/Work/projects/site"
	const manifestPath = workspacePath + "/product.json"
	const runtimePath = workspacePath + "/workflow.json"
	workspace := &mockWorkspaceAPI{files: map[string]string{
		manifestPath: `{"schema_version":1,"product":"work","id":"site","title":"Site","custom":{"keep":true},"capabilities":{"selected_skills":["review"],"workflow_context_paths":["Workflow/legacy"]},"schedules":[{"id":"daily"}],"triggers":[{"id":"hook"}]}`,
	}}
	host := httptest.NewServer(workspace)
	defer host.Close()
	t.Setenv("WORKSPACE_API_URL", host.URL)

	names, initialized, err := productSelectedSecrets(context.Background(), "work", workspacePath)
	if err != nil || initialized || len(names) != 0 {
		t.Fatalf("before migration names=%v initialized=%v err=%v", names, initialized, err)
	}
	if err := updateProductSelectedSecrets(context.Background(), "work", workspacePath, func(current []string) []string {
		return append(current, "GITHUB_TOKEN", "GITHUB_TOKEN", " SENTRY_TOKEN ")
	}); err != nil {
		t.Fatal(err)
	}
	names, initialized, err = productSelectedSecrets(context.Background(), "work", workspacePath)
	if err != nil || !initialized || len(names) != 2 || names[0] != "GITHUB_TOKEN" || names[1] != "SENTRY_TOKEN" {
		t.Fatalf("after update names=%v initialized=%v err=%v", names, initialized, err)
	}

	workspace.mu.Lock()
	productSaved := workspace.files[manifestPath]
	saved := workspace.files[runtimePath]
	workspace.mu.Unlock()
	var manifest map[string]interface{}
	if err := json.Unmarshal([]byte(saved), &manifest); err != nil {
		t.Fatal(err)
	}
	var product map[string]interface{}
	if json.Unmarshal([]byte(productSaved), &product) != nil || product["custom"] == nil {
		t.Fatalf("product metadata was changed: %s", productSaved)
	}
	if _, exists := product["capabilities"]; exists {
		t.Fatalf("legacy runtime fields remained in product metadata: %s", productSaved)
	}
	capabilities := manifest["capabilities"].(map[string]interface{})
	if _, ok := capabilities["selected_skills"]; !ok {
		t.Fatalf("existing capabilities were lost: %s", saved)
	}
	if !strings.Contains(saved, `"workflow_context_paths"`) || !strings.Contains(saved, `"schedules"`) || !strings.Contains(saved, `"triggers"`) {
		t.Fatalf("legacy runtime fields were not migrated: %s", saved)
	}
}

func TestProductManifestRoundTripPreservesExplicitEmptySecretSelection(t *testing.T) {
	var manifest productProjectManifest
	if err := json.Unmarshal([]byte(`{"capabilities":{"selected_secrets":[]}}`), &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Capabilities.SelectedSecrets == nil || len(*manifest.Capabilities.SelectedSecrets) != 0 {
		t.Fatalf("explicit empty selection was not preserved: %#v", manifest.Capabilities.SelectedSecrets)
	}
	encoded, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) == "" || !jsonContainsField(encoded, "capabilities", "selected_secrets") {
		t.Fatalf("explicit empty selection disappeared during rewrite: %s", encoded)
	}

	var legacy productProjectManifest
	if err := json.Unmarshal([]byte(`{"capabilities":{}}`), &legacy); err != nil {
		t.Fatal(err)
	}
	encoded, err = json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if jsonContainsField(encoded, "capabilities", "selected_secrets") {
		t.Fatalf("legacy missing selection became explicit during rewrite: %s", encoded)
	}
}

func jsonContainsField(encoded []byte, parent, child string) bool {
	var value map[string]interface{}
	if json.Unmarshal(encoded, &value) != nil {
		return false
	}
	nested, _ := value[parent].(map[string]interface{})
	_, ok := nested[child]
	return ok
}
