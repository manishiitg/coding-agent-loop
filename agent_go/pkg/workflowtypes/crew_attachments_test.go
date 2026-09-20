package workflowtypes

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateCrewAttachmentAlias(t *testing.T) {
	for _, alias := range []string{"rts-reviewer", "rts_reviewer", "r1", "a"} {
		if err := ValidateCrewAttachmentAlias(alias); err != nil {
			t.Fatalf("alias %q rejected: %v", alias, err)
		}
	}
	for _, tt := range []struct {
		alias string
		want  string
	}{
		{"", "required"},
		{"RTS", "lowercase"},
		{"rts reviewer", "lowercase"},
		{"rts/reviewer", "lowercase"},
		{"runs", "shadow"},
		{"planning", "shadow"},
		{"workflow.json", "shadow"},
		{strings.Repeat("a", 65), "lowercase"},
	} {
		if err := ValidateCrewAttachmentAlias(tt.alias); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Fatalf("alias %q err = %v, want containing %q", tt.alias, err, tt.want)
		}
	}
}

func TestResolveCrewAttachmentPath(t *testing.T) {
	attachments := []CrewAttachment{
		{ID: "a1", Alias: "rts-reviewer", CrewProfileID: "work", CrewProjectID: "rts", CrewWorkspacePath: "_users/owner/Chats/Work/projects/rts"},
	}
	if got, ok := ResolveCrewAttachmentPath(attachments, "rts-reviewer/reports/pr-87.md"); !ok || got != "_users/owner/Chats/Work/projects/rts/reports/pr-87.md" {
		t.Fatalf("nested = %q ok=%v", got, ok)
	}
	if got, ok := ResolveCrewAttachmentPath(attachments, "rts-reviewer"); !ok || got != "_users/owner/Chats/Work/projects/rts" {
		t.Fatalf("alias-only = %q ok=%v", got, ok)
	}
	if _, ok := ResolveCrewAttachmentPath(attachments, "other/file.md"); ok {
		t.Fatal("unknown alias resolved")
	}
	if _, ok := ResolveCrewAttachmentPath(attachments, "rts-reviewer/../../escape.md"); ok {
		t.Fatal("path escape resolved")
	}
	if _, ok := ResolveCrewAttachmentPath(nil, "rts-reviewer/file.md"); ok {
		t.Fatal("empty attachments resolved")
	}
	if _, ok := ResolveCrewAttachmentPath([]CrewAttachment{{Alias: "broken"}}, "broken/file.md"); ok {
		t.Fatal("empty crew root resolved")
	}
}

func TestReadCrewAttachments(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "Workflow", "demo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "workflow.json"), []byte(`{"id":"wf-1","crew_attachments":[{"id":"a1","alias":"rts","crew_profile_id":"work","crew_project_id":"rts","crew_workspace_path":"_users/owner/Chats/Work/projects/rts"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	got, found := ReadCrewAttachments(root, "Workflow/demo")
	if !found || len(got) != 1 || got[0].Alias != "rts" {
		t.Fatalf("attachments = %+v found=%v", got, found)
	}
	if _, found := ReadCrewAttachments(root, "Workflow/missing"); found {
		t.Fatal("missing manifest reported found")
	}
}
