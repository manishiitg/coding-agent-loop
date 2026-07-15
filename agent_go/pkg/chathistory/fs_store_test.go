package chathistory

import (
	"context"
	"testing"
)

func TestFilesystemStoreWorkflowSecretsRoundTrip(t *testing.T) {
	store, err := NewFilesystemStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewFilesystemStore() error = %v", err)
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

func TestFilesystemStoreWorkflowProviderCredentialsAreIsolated(t *testing.T) {
	store, err := NewFilesystemStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewFilesystemStore() error = %v", err)
	}
	ctx := context.Background()
	const workflowA = "Workflow/customer-renewals"
	const workflowB = "Workflow/support-triage"

	if err := store.UpsertWorkflowProviderCredential(ctx, "alice", workflowA, "claude-code", "cipher-a"); err != nil {
		t.Fatalf("UpsertWorkflowProviderCredential() error = %v", err)
	}
	if err := store.UpsertWorkflowProviderCredential(ctx, "bob", workflowA, "claude-code", "cipher-b"); err != nil {
		t.Fatalf("UpsertWorkflowProviderCredential(second user) error = %v", err)
	}

	alice, err := store.GetWorkflowProviderCredential(ctx, "alice", workflowA, "claude-code")
	if err != nil || alice == nil || alice.EncryptedValue != "cipher-a" {
		t.Fatalf("alice credential = %+v, err=%v", alice, err)
	}
	bob, err := store.GetWorkflowProviderCredential(ctx, "bob", workflowA, "claude-code")
	if err != nil || bob == nil || bob.EncryptedValue != "cipher-b" {
		t.Fatalf("bob credential = %+v, err=%v", bob, err)
	}
	other, err := store.GetWorkflowProviderCredential(ctx, "alice", workflowB, "claude-code")
	if err != nil || other != nil {
		t.Fatalf("other workflow credential = %+v, err=%v; want nil", other, err)
	}
	if err := store.DeleteWorkflowProviderCredential(ctx, "alice", workflowA, "claude-code"); err != nil {
		t.Fatalf("DeleteWorkflowProviderCredential() error = %v", err)
	}
	alice, err = store.GetWorkflowProviderCredential(ctx, "alice", workflowA, "claude-code")
	if err != nil || alice != nil {
		t.Fatalf("deleted credential = %+v, err=%v; want nil", alice, err)
	}
}
