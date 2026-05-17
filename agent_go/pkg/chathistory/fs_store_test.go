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
