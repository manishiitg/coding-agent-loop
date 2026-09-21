package server

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	wf "github.com/manishiitg/coding-agent-loop/workspace/workflowfiles"
)

func TestExternalFileReadsUseSharedFilesystemWithoutWorkflowFilesEndpoint(t *testing.T) {
	docs := t.TempDir()
	t.Setenv("WORKSPACE_DOCS_PATH", docs)
	workflow := filepath.Join(docs, "Workflow", "invoices")
	if err := os.MkdirAll(filepath.Join(workflow, "docs"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workflow, "docs", "notes.md"), []byte("first line\nneedle here\n"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	read, err := externalFileRequest(ctx, wf.Request{Root: "Workflow/invoices", Operation: "read", Path: "docs/notes.md"})
	if err != nil || !read.Exists || read.Content != "first line\nneedle here\n" {
		t.Fatalf("direct read = %+v, %v", read, err)
	}
	listed, err := externalFileRequest(ctx, wf.Request{Root: "Workflow/invoices", Operation: "list", Path: "docs"})
	if err != nil || len(listed.Entries) != 1 || listed.Entries[0].Path != "docs/notes.md" {
		t.Fatalf("direct list = %+v, %v", listed, err)
	}
	searched, err := externalFileRequest(ctx, wf.Request{Root: "Workflow/invoices", Operation: "search", Path: "docs", Query: "needle"})
	if err != nil || len(searched.Entries) != 1 || searched.Entries[0].Line != 2 {
		t.Fatalf("direct search = %+v, %v", searched, err)
	}
	for _, req := range []wf.Request{
		{Root: "Workflow/invoices", Operation: "read", Path: "../outside"},
		{Root: "Workflow/invoices", Operation: "read", Path: ".env"},
		{Root: "Workflow/invoices", Operation: "commit", Path: "docs/notes.md"},
	} {
		if _, err := externalFileRequest(ctx, req); err == nil {
			t.Fatalf("accepted invalid request %+v", req)
		}
	}
}

func TestExternalFileReadsRejectSymlinks(t *testing.T) {
	docs := t.TempDir()
	t.Setenv("WORKSPACE_DOCS_PATH", docs)
	workflow := filepath.Join(docs, "Workflow", "invoices")
	if err := os.MkdirAll(workflow, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(docs, filepath.Join(workflow, "outside")); err != nil {
		t.Fatal(err)
	}
	_, err := externalFileRequest(context.Background(), wf.Request{Root: "Workflow/invoices", Operation: "read", Path: "outside/secret"})
	var upstream *externalUpstreamError
	if !errors.As(err, &upstream) || upstream.status != 403 || !strings.Contains(upstream.message, "symbolic") {
		t.Fatalf("symlink read error = %v", err)
	}
}

func TestExternalFileReadsUseSharedAssetsWhenWorkspaceIsRemote(t *testing.T) {
	f := newExternalToolsFixture(t)
	f.write(t, "Workflow/invoices/docs/remote.md", "remote needle\n")
	ctx := context.Background()
	read, err := externalRemoteFileRequest(ctx, wf.Request{Root: "Workflow/invoices", Operation: "read", Path: "docs/remote.md"}, "docs/remote.md")
	if err != nil || !read.Exists || read.Content != "remote needle\n" {
		t.Fatalf("remote read = %+v, %v", read, err)
	}
	listed, err := externalRemoteFileRequest(ctx, wf.Request{Root: "Workflow/invoices", Operation: "list", Path: "docs"}, "docs")
	if err != nil || len(listed.Entries) != 2 {
		t.Fatalf("remote list = %+v, %v", listed, err)
	}
	searched, err := externalRemoteFileRequest(ctx, wf.Request{Root: "Workflow/invoices", Operation: "search", Path: "docs", Query: "needle"}, "docs")
	if err != nil || len(searched.Entries) != 1 || searched.Entries[0].Path != "docs/remote.md" {
		t.Fatalf("remote search = %+v, %v", searched, err)
	}
}
