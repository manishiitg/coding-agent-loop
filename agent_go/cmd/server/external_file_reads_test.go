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

func TestExternalFileReadsFilterSourceAndProtectRuntimePaths(t *testing.T) {
	docs := t.TempDir()
	t.Setenv("WORKSPACE_DOCS_PATH", docs)
	root := filepath.Join(docs, "Workflow", "invoices")
	for name, content := range map[string]string{
		"code/run-basic-smoke/main.py":         "def test_auth():\n    return True\n",
		"code/run-basic-smoke/modules/auth.py": "def test_login():\n    return True\n",
		"code/run-basic-smoke/README.md":       "test login guidance\n",
		"code/.local/lib/installed.py":         "def test_hidden():\n    return True\n",
		"code/.cache/wheel.py":                 "def test_hidden():\n    return True\n",
	} {
		file := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	ctx := context.Background()
	req := wf.Request{Root: "Workflow/invoices", Operation: "list", Path: "code", Glob: "**/*.py", Depth: 8}
	listed, err := externalFileRequest(ctx, req)
	if err != nil || len(listed.Entries) != 2 {
		t.Fatalf("filtered list = %+v, %v", listed, err)
	}
	for _, entry := range listed.Entries {
		if !strings.HasPrefix(entry.Path, "code/run-basic-smoke/") {
			t.Errorf("unexpected path: %s", entry.Path)
		}
	}
	req.Operation, req.Query = "search", "test_login"
	searched, err := externalFileRequest(ctx, req)
	if err != nil || len(searched.Entries) != 1 || searched.Entries[0].Path != "code/run-basic-smoke/modules/auth.py" {
		t.Fatalf("filtered search = %+v, %v", searched, err)
	}
	for _, name := range []string{"code/.local/lib/installed.py", "code/.cache/wheel.py"} {
		if _, err := externalFileRequest(ctx, wf.Request{Root: "Workflow/invoices", Operation: "read", Path: name}); err == nil {
			t.Errorf("direct read allowed excluded path %s", name)
		}
	}
	if _, err := externalFileRequest(ctx, wf.Request{Root: "Workflow/invoices", Operation: "list", Path: "code", Glob: "../*.py"}); err == nil {
		t.Error("accepted invalid glob")
	}
}

func TestExternalFileReadsUseSharedAssetsWhenWorkspaceIsRemote(t *testing.T) {
	f := newExternalToolsFixture(t)
	f.write(t, "Workflow/invoices/docs/remote.md", "remote needle\n")
	f.write(t, "Workflow/invoices/code/run-basic-smoke/main.py", "def test_login(): pass\n")
	f.write(t, "Workflow/invoices/code/.cache/installed.py", "def test_hidden(): pass\n")
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
	filtered, err := externalRemoteFileRequest(ctx, wf.Request{Root: "Workflow/invoices", Operation: "list", Path: "code", Glob: "**/*.py", Depth: 8}, "code")
	if err != nil || len(filtered.Entries) != 1 || filtered.Entries[0].Path != "code/run-basic-smoke/main.py" {
		t.Fatalf("remote filtered list = %+v, %v", filtered, err)
	}
	_, err = externalRemoteFileRequest(ctx, wf.Request{Root: "Workflow/invoices", Operation: "list", Path: "missing"}, "missing")
	var upstream *externalUpstreamError
	if !errors.As(err, &upstream) || upstream.status != 404 {
		t.Fatalf("missing remote directory = %v", err)
	}
}
