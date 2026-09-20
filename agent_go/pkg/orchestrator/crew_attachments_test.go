package orchestrator

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"
)

func crewAttachmentTestOrchestrator(t *testing.T, manifest string) *BaseOrchestrator {
	t.Helper()
	root := t.TempDir()
	t.Setenv("WORKSPACE_DOCS_PATH", root)
	dir := filepath.Join(root, "Workflow", "demo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "workflow.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	return &BaseOrchestrator{workspacePath: "Workflow/demo", logger: loggerv2.NewNoop()}
}

const crewAttachmentTestManifest = `{"id":"wf-1","crew_attachments":[{"id":"a1","alias":"rts","crew_profile_id":"work","crew_project_id":"rts","crew_workspace_path":"_users/owner/Chats/Work/projects/rts"}]}`

func TestResolveWorkspacePathMapsCrewAttachmentAlias(t *testing.T) {
	bo := crewAttachmentTestOrchestrator(t, crewAttachmentTestManifest)
	if got := bo.resolveWorkspacePath("rts/reports/pr-87.md"); got != "_users/owner/Chats/Work/projects/rts/reports/pr-87.md" {
		t.Fatalf("alias path = %q", got)
	}
	if got := bo.resolveWorkspacePath("runs/iteration-0/execution/out.md"); got != "Workflow/demo/runs/iteration-0/execution/out.md" {
		t.Fatalf("ordinary path = %q", got)
	}
	if got := bo.resolveWorkspacePath("rts/../../escape.md"); strings.HasPrefix(got, "_users/") {
		t.Fatalf("escape resolved into crew root: %q", got)
	}
}

func TestCrewAttachmentReadsSkippedOutsideWorkflows(t *testing.T) {
	bo := &BaseOrchestrator{workspacePath: "Chats/general", logger: loggerv2.NewNoop()}
	if got := bo.resolveWorkspacePath("rts/file.md"); got != "Chats/general/rts/file.md" {
		t.Fatalf("non-workflow path = %q", got)
	}
}

func TestCrewAttachmentMutationsBlocked(t *testing.T) {
	bo := crewAttachmentTestOrchestrator(t, crewAttachmentTestManifest)
	ctx := context.Background()
	if err := bo.WriteWorkspaceFile(ctx, "rts/notes.md", "x"); err == nil || !strings.Contains(err.Error(), "read-only") {
		t.Fatalf("write err = %v", err)
	}
	if err := bo.DeleteWorkspaceFile(ctx, "rts/notes.md"); err == nil || !strings.Contains(err.Error(), "read-only") {
		t.Fatalf("delete err = %v", err)
	}
	if err := bo.MoveWorkspaceFile(ctx, "rts/a.md", "runs/b.md"); err == nil || !strings.Contains(err.Error(), "read-only") {
		t.Fatalf("move-from err = %v", err)
	}
	if err := bo.MoveWorkspaceFile(ctx, "runs/a.md", "rts/b.md"); err == nil || !strings.Contains(err.Error(), "read-only") {
		t.Fatalf("move-to err = %v", err)
	}
	if err := bo.CleanupDirectory(ctx, "rts", "crew"); err == nil || !strings.Contains(err.Error(), "read-only") {
		t.Fatalf("cleanup err = %v", err)
	}
}
