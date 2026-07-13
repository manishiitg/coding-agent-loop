package utils

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- IsValidUserID ---

func TestIsValidUserID(t *testing.T) {
	tests := []struct {
		name   string
		userID string
		want   bool
	}{
		{"empty", "", false},
		{"simple", "user1", true},
		{"with-hyphens", "my-user", true},
		{"with-underscores", "my_user", true},
		{"alphanumeric", "abc123", true},
		{"special-chars", "user@domain", false},
		{"slashes", "user/other", false},
		{"dots", "user.name", false},
		{"spaces", "user name", false},
		{"path-traversal", "../etc", false},
		{"max-length", strings.Repeat("a", 128), true},
		{"too-long", strings.Repeat("a", 129), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsValidUserID(tt.userID); got != tt.want {
				t.Errorf("IsValidUserID(%q) = %v, want %v", tt.userID, got, tt.want)
			}
		})
	}
}

// --- SanitizeUserID ---

func TestSanitizeUserID(t *testing.T) {
	tests := []struct {
		name   string
		userID string
		want   string
	}{
		{"empty-returns-default", "", GetDefaultUserID()},
		{"valid-passthrough", "user1", "user1"},
		{"invalid-returns-default", "user@domain", GetDefaultUserID()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SanitizeUserID(tt.userID); got != tt.want {
				t.Errorf("SanitizeUserID(%q) = %q, want %q", tt.userID, got, tt.want)
			}
		})
	}
}

// --- IsPerUserPath ---

func TestIsPerUserPath(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"Chats", true},
		{"Chats/session.json", true},
		{"Downloads", true},
		{"Downloads/file.pdf", true},
		{"skills", false},
		{"skills/my-skill.json", false},
		{"Workflow", false},
		{"Workflow/project/step.json", false},
		{"_users", false},
		{"config", false},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			if got := IsPerUserPath(tt.path); got != tt.want {
				t.Errorf("IsPerUserPath(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

// --- ResolveUserPath ---

func TestResolveUserPath(t *testing.T) {
	docsDir, err := os.MkdirTemp("", "resolve-path-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(docsDir)

	t.Run("ChatsRoutesToUserDir", func(t *testing.T) {
		resolved, err := ResolveUserPath(docsDir, "Chats/session.json", "user1")
		if err != nil {
			t.Fatalf("ResolveUserPath failed: %v", err)
		}
		expected := filepath.Join(docsDir, "_users", "user1", "Chats", "session.json")
		if resolved != expected {
			t.Errorf("got %q, want %q", resolved, expected)
		}
	})

	t.Run("DownloadsRoutesToUserDir", func(t *testing.T) {
		resolved, err := ResolveUserPath(docsDir, "Downloads/file.pdf", "user1")
		if err != nil {
			t.Fatalf("ResolveUserPath failed: %v", err)
		}
		expected := filepath.Join(docsDir, "_users", "user1", "Downloads", "file.pdf")
		if resolved != expected {
			t.Errorf("got %q, want %q", resolved, expected)
		}
	})

	t.Run("SharedPathResolvesToRoot", func(t *testing.T) {
		resolved, err := ResolveUserPath(docsDir, "skills/my-skill.json", "user1")
		if err != nil {
			t.Fatalf("ResolveUserPath failed: %v", err)
		}
		expected := filepath.Join(docsDir, "skills", "my-skill.json")
		if resolved != expected {
			t.Errorf("got %q, want %q", resolved, expected)
		}
	})

	t.Run("DifferentUsersGetDifferentPaths", func(t *testing.T) {
		resolved1, err := ResolveUserPath(docsDir, "Chats/session.json", "user1")
		if err != nil {
			t.Fatalf("ResolveUserPath failed for user1: %v", err)
		}
		resolved2, err := ResolveUserPath(docsDir, "Chats/session.json", "user2")
		if err != nil {
			t.Fatalf("ResolveUserPath failed for user2: %v", err)
		}
		if resolved1 == resolved2 {
			t.Errorf("Different users should resolve to different paths: both got %q", resolved1)
		}
	})

	t.Run("SharedPathsSameForAllUsers", func(t *testing.T) {
		resolved1, err := ResolveUserPath(docsDir, "skills/shared.json", "user1")
		if err != nil {
			t.Fatalf("ResolveUserPath failed for user1: %v", err)
		}
		resolved2, err := ResolveUserPath(docsDir, "skills/shared.json", "user2")
		if err != nil {
			t.Fatalf("ResolveUserPath failed for user2: %v", err)
		}
		if resolved1 != resolved2 {
			t.Errorf("Shared paths should be identical: user1=%q, user2=%q", resolved1, resolved2)
		}
	})

	t.Run("EmptyUserIDFallsToDefault", func(t *testing.T) {
		resolved, err := ResolveUserPath(docsDir, "Chats/session.json", "")
		if err != nil {
			t.Fatalf("ResolveUserPath failed: %v", err)
		}
		expected := filepath.Join(docsDir, "_users", GetDefaultUserID(), "Chats", "session.json")
		if resolved != expected {
			t.Errorf("got %q, want %q", resolved, expected)
		}
	})

	t.Run("FullPathInputSanitized", func(t *testing.T) {
		fullPath := filepath.Join(docsDir, "Chats", "session.json")
		resolved, err := ResolveUserPath(docsDir, fullPath, "user1")
		if err != nil {
			t.Fatalf("ResolveUserPath failed: %v", err)
		}
		expected := filepath.Join(docsDir, "_users", "user1", "Chats", "session.json")
		if resolved != expected {
			t.Errorf("got %q, want %q", resolved, expected)
		}
	})

	t.Run("TraversalEscapeRejected", func(t *testing.T) {
		for _, p := range []string{
			"../sibling/source.go",
			"..",
			"Workflow/../../outside.txt",
			"a/b/../../../etc/passwd",
		} {
			if resolved, err := ResolveUserPath(docsDir, p, "user1"); err == nil {
				t.Errorf("path %q should be rejected (escapes workspace root), resolved to %q", p, resolved)
			}
		}
	})

	t.Run("InternalDotDotAllowed", func(t *testing.T) {
		// ".." segments that stay inside the root are fine after cleaning.
		resolved, err := ResolveUserPath(docsDir, "Workflow/demo/../other/file.md", "user1")
		if err != nil {
			t.Fatalf("ResolveUserPath failed: %v", err)
		}
		expected := filepath.Join(docsDir, "Workflow", "other", "file.md")
		if resolved != expected {
			t.Errorf("got %q, want %q", resolved, expected)
		}
	})
}

// --- ConvertToUserRelativePath ---

func TestConvertToUserRelativePath(t *testing.T) {
	docsDir := "/app/workspace-docs"

	tests := []struct {
		name     string
		fullPath string
		want     string
	}{
		{
			"per-user-chats-path",
			"/app/workspace-docs/_users/user1/Chats/session.json",
			"Chats/session.json",
		},
		{
			"per-user-downloads-path",
			"/app/workspace-docs/_users/default/Downloads/file.pdf",
			"Downloads/file.pdf",
		},
		{
			"shared-path",
			"/app/workspace-docs/skills/my-skill.json",
			"skills/my-skill.json",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ConvertToUserRelativePath(tt.fullPath, docsDir)
			if err != nil {
				t.Fatalf("ConvertToUserRelativePath failed: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

// --- SanitizeInputPath ---

func TestSanitizeInputPath(t *testing.T) {
	docsDir := "/app/workspace-docs"

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"already-relative", "Chats/session.json", "Chats/session.json"},
		{"full-path-stripped", "/app/workspace-docs/Chats/session.json", "Chats/session.json"},
		{"dotdot-cleaned", "Chats/../skills/x.json", "skills/x.json"},
		{"dot-path", "./Chats/session.json", "Chats/session.json"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SanitizeInputPath(tt.input, docsDir)
			if got != tt.want {
				t.Errorf("SanitizeInputPath(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// --- IsValidFilePath ---

func TestIsValidFilePath(t *testing.T) {
	docsDir := "/app/workspace-docs"

	tests := []struct {
		name     string
		filePath string
		want     bool
	}{
		{"valid-subpath", "/app/workspace-docs/Chats/session.json", true},
		{"traversal-attack", "/app/workspace-docs/../etc/passwd", false},
		{"outside-docsdir", "/etc/passwd", false},
		{"exactly-docsdir", "/app/workspace-docs", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsValidFilePath(tt.filePath, docsDir); got != tt.want {
				t.Errorf("IsValidFilePath(%q) = %v, want %v", tt.filePath, got, tt.want)
			}
		})
	}
}

func TestIsValidFilePathRejectsSymlinkEscape(t *testing.T) {
	docsDir := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(docsDir, "escape")); err != nil {
		t.Fatal(err)
	}

	if IsValidFilePath(filepath.Join(docsDir, "escape", "secret.txt"), docsDir) {
		t.Fatal("existing file through an escaping symlink must be rejected")
	}
	if IsValidFilePath(filepath.Join(docsDir, "escape", "new.txt"), docsDir) {
		t.Fatal("new file under an escaping symlink must be rejected")
	}
}

func TestIsValidFilePathAllowsSymlinkInsideWorkspace(t *testing.T) {
	docsDir := t.TempDir()
	target := filepath.Join(docsDir, "target")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(docsDir, "inside")); err != nil {
		t.Fatal(err)
	}

	if !IsValidFilePath(filepath.Join(docsDir, "inside", "new.txt"), docsDir) {
		t.Fatal("symlink that remains inside the workspace should be allowed")
	}
}
