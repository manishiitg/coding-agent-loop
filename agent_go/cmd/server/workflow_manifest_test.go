package server

import "testing"

func TestValidateManifestCDPPorts(t *testing.T) {
	manifest := NewWorkflowManifest("Multi-profile browser")
	manifest.Capabilities.BrowserMode = "cdp"
	manifest.Capabilities.CDPPorts = []int{9222, 9333}
	if err := ValidateManifest(manifest); err != nil {
		t.Fatalf("valid multi-profile CDP ports rejected: %v", err)
	}

	manifest.Capabilities.CDPPorts = []int{9222, 9222}
	if err := ValidateManifest(manifest); err == nil {
		t.Fatal("duplicate CDP ports should be rejected")
	}

	manifest.Capabilities.CDPPorts = []int{9222, 9333, 9444, 9555, 9666}
	if err := ValidateManifest(manifest); err == nil {
		t.Fatal("more than four CDP ports should be rejected")
	}
}

func TestNewWorkflowManifestDefaultsGlobalSecretsToNone(t *testing.T) {
	manifest := NewWorkflowManifest("Test workflow")
	if manifest.Version != WorkflowContractCurrentVersion {
		t.Fatalf("Version = %q, want %q", manifest.Version, WorkflowContractCurrentVersion)
	}
	if manifest.Capabilities.SelectedGlobalSecretNames == nil {
		t.Fatal("SelectedGlobalSecretNames = nil, want empty selection")
	}
	if got := len(*manifest.Capabilities.SelectedGlobalSecretNames); got != 0 {
		t.Fatalf("SelectedGlobalSecretNames length = %d, want 0", got)
	}
}

func TestWorkflowCreatorDefaultsGlobalSecretsToNone(t *testing.T) {
	cases := []struct {
		name        string
		workflowMap map[string]interface{}
	}{
		{
			name:        "missing capabilities",
			workflowMap: map[string]interface{}{},
		},
		{
			name: "null global secrets",
			workflowMap: map[string]interface{}{
				"capabilities": map[string]interface{}{
					"selected_global_secret_names": nil,
				},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defaultWorkflowCreatorGlobalSecretsToNone(tc.workflowMap)

			caps := tc.workflowMap["capabilities"].(map[string]interface{})
			names, ok := caps["selected_global_secret_names"].([]interface{})
			if !ok {
				t.Fatalf("selected_global_secret_names = %T, want []interface{}", caps["selected_global_secret_names"])
			}
			if len(names) != 0 {
				t.Fatalf("selected_global_secret_names length = %d, want 0", len(names))
			}
		})
	}
}
