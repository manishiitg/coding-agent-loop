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

func TestValidateCrewAttachmentBinding(t *testing.T) {
	// Binding validation is filesystem-free: a well-shaped attachment
	// passes even when its crew directory does not exist here. Server
	// paths pair it with a live crew access check instead.
	valid := CrewAttachment{Alias: "rts", CrewProfileID: "work", CrewProjectID: "rts", CrewWorkspacePath: "_users/owner/Chats/Work/projects/rts"}
	if err := ValidateCrewAttachmentBinding(valid); err != nil {
		t.Fatalf("valid binding rejected: %v", err)
	}
	for name, mutate := range map[string]func(*CrewAttachment){
		"retargeted crew": func(a *CrewAttachment) { a.CrewWorkspacePath = "_users/other/Chats/Work/projects/evil" },
		"arbitrary path":  func(a *CrewAttachment) { a.CrewWorkspacePath = "_users/owner/secrets" },
		"missing project": func(a *CrewAttachment) { a.CrewProjectID = "" },
		"bad alias":       func(a *CrewAttachment) { a.Alias = "Bad Alias!" },
	} {
		bad := valid
		mutate(&bad)
		if err := ValidateCrewAttachmentBinding(bad); err == nil {
			t.Fatalf("%s: expected rejection", name)
		}
	}
}

func TestValidateCrewAttachmentRoot(t *testing.T) {
	root := t.TempDir()
	crewDir := filepath.Join(root, "_users", "owner", "Chats", "Work", "projects", "rts")
	if err := os.MkdirAll(crewDir, 0o755); err != nil {
		t.Fatal(err)
	}
	valid := CrewAttachment{Alias: "rts", CrewProfileID: "work", CrewProjectID: "rts", CrewWorkspacePath: "_users/owner/Chats/Work/projects/rts"}
	if err := ValidateCrewAttachmentRoot(valid, root); err != nil {
		t.Fatalf("valid attachment rejected: %v", err)
	}
	for name, mutate := range map[string]func(*CrewAttachment){
		"retargeted crew": func(a *CrewAttachment) { a.CrewWorkspacePath = "_users/other/Chats/Work/projects/evil" },
		"arbitrary path":  func(a *CrewAttachment) { a.CrewWorkspacePath = "_users/owner/secrets" },
		"missing project": func(a *CrewAttachment) { a.CrewProjectID = "" },
		"bad alias":       func(a *CrewAttachment) { a.Alias = "Bad Alias!" },
		"deleted crew dir": func(a *CrewAttachment) {
			a.CrewWorkspacePath = "_users/owner/Chats/Work/projects/gone"
			a.CrewProjectID = "gone"
		},
	} {
		bad := valid
		mutate(&bad)
		if err := ValidateCrewAttachmentRoot(bad, root); err == nil {
			t.Fatalf("%s: expected rejection", name)
		}
	}
}

func TestCrewAttachmentEnvKeys(t *testing.T) {
	env := CrewAttachmentEnvKeys([]CrewAttachment{
		{Alias: "rts", CrewWorkspacePath: "_users/owner/Chats/Work/projects/rts"},
		{Alias: "rts-again", CrewWorkspacePath: "_users/owner/Chats/Work/projects/rts2"},
		{Alias: "rts_again", CrewWorkspacePath: "_users/owner/Chats/Work/projects/rts3"},
	})
	if env["WORKFLOW_CREW_RTS"] != "_users/owner/Chats/Work/projects/rts" {
		t.Fatalf("env = %v", env)
	}
	if len(env) != 3 {
		t.Fatalf("env = %v, want 3 disambiguated keys", env)
	}
	seen := map[string]bool{}
	for key := range env {
		if !strings.HasPrefix(key, "WORKFLOW_CREW_") || seen[key] {
			t.Fatalf("env = %v, want unique WORKFLOW_CREW_* keys", env)
		}
		seen[key] = true
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
