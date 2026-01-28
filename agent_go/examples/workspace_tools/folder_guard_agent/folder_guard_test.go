package main_test

import (
	"context"
	"testing"

	"mcp-agent-builder-go/agent_go/pkg/workspace"
)

func TestFolderGuard_BlocksOutsidePaths(t *testing.T) {
	guard := &workspace.FolderGuardConfig{
		Enabled:    true,
		ReadPaths:  []string{"docs"},
		WritePaths: []string{"output"},
	}
	client := workspace.NewClient("http://localhost:8083", workspace.WithFolderGuard(guard))
	ctx := context.Background()

	_, err := client.ListWorkspaceFiles(ctx, workspace.ListWorkspaceFilesParams{Folder: "."})
	if err == nil {
		t.Error("ListWorkspaceFiles(folder=\".\") should be blocked by folder guard")
	}
	if err != nil && err.Error() == "" {
		t.Error("expected non-empty error message")
	}

	_, err = client.ReadWorkspaceFile(ctx, workspace.ReadWorkspaceFileParams{Filepath: "outside.txt"})
	if err == nil {
		t.Error("ReadWorkspaceFile(outside.txt) should be blocked by folder guard")
	}

	_, err = client.UpdateWorkspaceFile(ctx, workspace.UpdateWorkspaceFileParams{
		Filepath: "forbidden.txt",
		Content:  "x",
	})
	if err == nil {
		t.Error("UpdateWorkspaceFile(forbidden.txt) should be blocked by folder guard")
	}
}

func TestFolderGuard_AllowsInsidePaths(t *testing.T) {
	guard := &workspace.FolderGuardConfig{
		Enabled:    true,
		ReadPaths:  []string{"docs"},
		WritePaths: []string{"output"},
	}
	client := workspace.NewClient("http://localhost:8083", workspace.WithFolderGuard(guard))

	if err := client.ValidatePath("docs", false); err != nil {
		t.Errorf("ValidatePath(docs, read) should be allowed: %v", err)
	}
	if err := client.ValidatePath("docs/sample.txt", false); err != nil {
		t.Errorf("ValidatePath(docs/sample.txt, read) should be allowed: %v", err)
	}
	if err := client.ValidatePath("output", true); err != nil {
		t.Errorf("ValidatePath(output, write) should be allowed: %v", err)
	}
	if err := client.ValidatePath("output/summary.txt", true); err != nil {
		t.Errorf("ValidatePath(output/summary.txt, write) should be allowed: %v", err)
	}

	if err := client.ValidatePath(".", false); err == nil {
		t.Error("ValidatePath(., read) should be blocked")
	}
	if err := client.ValidatePath("outside.txt", false); err == nil {
		t.Error("ValidatePath(outside.txt, read) should be blocked")
	}
	if err := client.ValidatePath("forbidden.txt", true); err == nil {
		t.Error("ValidatePath(forbidden.txt, write) should be blocked")
	}
}
