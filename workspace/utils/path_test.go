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
		name string
		path string
		want bool
	}{
		{"chats-folder", "Chats", true},
		{"chats-subpath", "Chats/session.json", true},
		{"downloads-folder", "Downloads", true},
		{"downloads-subpath", "Downloads/file.txt", true},
		{"skills-shared", "skills", false},
		{"skills-subpath", "skills/my-skill.json", false},
		{"workflow-shared", "Workflow", false},
		{"root-file", "readme.txt", false},
		{"empty", "", false},
		{"leading-slash-chats", "/Chats", true},
		{"leading-slash-chats-sub", "/Chats/foo.json", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsPerUserPath(tt.path); got != tt.want {
				t.Errorf("IsPerUserPath(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

// --- ResolveUserPath ---

func TestResolveUserPath(t *testing.T) {
	// Create temp docsDir
	docsDir, err := os.MkdirTemp("", "resolve-path-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(docsDir)

	t.Run("PerUserPathRouting", func(t *testing.T) {
		resolved, err := ResolveUserPath(docsDir, "Chats/session.json", "user1")
		if err != nil {
			t.Fatalf("ResolveUserPath failed: %v", err)
		}
		expected := filepath.Join(docsDir, "_users", "user1", "Chats", "session.json")
		if resolved != expected {
			t.Errorf("got %q, want %q", resolved, expected)
		}
	})

	t.Run("SharedPathPassthrough", func(t *testing.T) {
		resolved, err := ResolveUserPath(docsDir, "skills/my-skill.json", "user1")
		if err != nil {
			t.Fatalf("ResolveUserPath failed: %v", err)
		}
		expected := filepath.Join(docsDir, "skills", "my-skill.json")
		if resolved != expected {
			t.Errorf("got %q, want %q", resolved, expected)
		}
	})

	t.Run("BlocksDirectUsersAccess", func(t *testing.T) {
		_, err := ResolveUserPath(docsDir, "_users", "user1")
		if err == nil {
			t.Error("Expected error for direct _users/ access, got nil")
		}
	})

	t.Run("BlocksUsersSubpath", func(t *testing.T) {
		_, err := ResolveUserPath(docsDir, "_users/other-user/Chats", "user1")
		if err == nil {
			t.Error("Expected error for _users/other-user/ access, got nil")
		}
	})

	t.Run("BlocksUsersWithTrailingSlash", func(t *testing.T) {
		_, err := ResolveUserPath(docsDir, "_users/", "user1")
		if err == nil {
			t.Error("Expected error for _users/ access, got nil")
		}
	})

	t.Run("CreatesUserDirectories", func(t *testing.T) {
		_, err := ResolveUserPath(docsDir, "Chats/test.json", "newuser")
		if err != nil {
			t.Fatalf("ResolveUserPath failed: %v", err)
		}
		// Verify directories were created
		for _, folder := range PerUserFolders {
			folderPath := filepath.Join(docsDir, "_users", "newuser", folder)
			if _, statErr := os.Stat(folderPath); os.IsNotExist(statErr) {
				t.Errorf("Expected directory %s to be created", folderPath)
			}
		}
	})

	t.Run("InvalidUserIDFallsToDefault", func(t *testing.T) {
		resolved, err := ResolveUserPath(docsDir, "Chats/session.json", "user@invalid")
		if err != nil {
			t.Fatalf("ResolveUserPath failed: %v", err)
		}
		expected := filepath.Join(docsDir, "_users", GetDefaultUserID(), "Chats", "session.json")
		if resolved != expected {
			t.Errorf("got %q, want %q (should fall back to default user)", resolved, expected)
		}
	})

	t.Run("EmptyUserIDFallsToDefault", func(t *testing.T) {
		resolved, err := ResolveUserPath(docsDir, "Chats/plan.md", "")
		if err != nil {
			t.Fatalf("ResolveUserPath failed: %v", err)
		}
		if !strings.Contains(resolved, filepath.Join("_users", GetDefaultUserID())) {
			t.Errorf("Expected default user path, got %q", resolved)
		}
	})

	t.Run("FullPathInputSanitized", func(t *testing.T) {
		// If user passes full internal path, it should be sanitized
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
			"per-user-path",
			"/app/workspace-docs/_users/user1/Chats/session.json",
			"Chats/session.json",
		},
		{
			"shared-path",
			"/app/workspace-docs/skills/my-skill.json",
			"skills/my-skill.json",
		},
		{
			"users-dir-only",
			"/app/workspace-docs/_users/user1",
			"",
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

// --- EnsurePerUserSymlinks ---

func TestEnsurePerUserSymlinks(t *testing.T) {
	docsDir, err := os.MkdirTemp("", "symlink-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(docsDir)

	// Create user directories first
	if err := EnsureUserDirectories(docsDir, "default"); err != nil {
		t.Fatalf("EnsureUserDirectories failed: %v", err)
	}

	t.Run("CreatesSymlinks", func(t *testing.T) {
		if err := EnsurePerUserSymlinks(docsDir, "default"); err != nil {
			t.Fatalf("EnsurePerUserSymlinks failed: %v", err)
		}

		for _, folder := range PerUserFolders {
			symlinkPath := filepath.Join(docsDir, folder)
			info, err := os.Lstat(symlinkPath)
			if err != nil {
				t.Errorf("Symlink %s does not exist: %v", folder, err)
				continue
			}
			if info.Mode()&os.ModeSymlink == 0 {
				t.Errorf("%s is not a symlink", folder)
				continue
			}
			target, err := os.Readlink(symlinkPath)
			if err != nil {
				t.Errorf("Failed to read symlink %s: %v", folder, err)
				continue
			}
			expectedTarget := filepath.Join("_users", "default", folder)
			if target != expectedTarget {
				t.Errorf("Symlink %s -> %s, want -> %s", folder, target, expectedTarget)
			}
		}
	})

	t.Run("IdempotentRerun", func(t *testing.T) {
		// Running again should not fail
		if err := EnsurePerUserSymlinks(docsDir, "default"); err != nil {
			t.Errorf("Second EnsurePerUserSymlinks call failed: %v", err)
		}
	})

	t.Run("UpdatesWrongTarget", func(t *testing.T) {
		// Point Chats to wrong user, then fix
		symlinkPath := filepath.Join(docsDir, "Chats")
		os.Remove(symlinkPath)
		os.Symlink(filepath.Join("_users", "wrong-user", "Chats"), symlinkPath)

		if err := EnsurePerUserSymlinks(docsDir, "default"); err != nil {
			t.Fatalf("EnsurePerUserSymlinks failed: %v", err)
		}

		target, err := os.Readlink(symlinkPath)
		if err != nil {
			t.Fatalf("Failed to read symlink: %v", err)
		}
		expected := filepath.Join("_users", "default", "Chats")
		if target != expected {
			t.Errorf("Symlink not updated: got %s, want %s", target, expected)
		}
	})

	t.Run("ReplacesEmptyDirectory", func(t *testing.T) {
		// Remove existing symlink, create empty dir instead
		testFolder := "Downloads"
		symlinkPath := filepath.Join(docsDir, testFolder)
		os.Remove(symlinkPath)
		os.MkdirAll(symlinkPath, 0755) // Create empty real dir

		if err := EnsurePerUserSymlinks(docsDir, "default"); err != nil {
			t.Fatalf("EnsurePerUserSymlinks failed: %v", err)
		}

		info, err := os.Lstat(symlinkPath)
		if err != nil {
			t.Fatalf("Lstat failed: %v", err)
		}
		if info.Mode()&os.ModeSymlink == 0 {
			t.Error("Empty directory should have been replaced with symlink")
		}
	})

	t.Run("SkipsNonEmptyDirectory", func(t *testing.T) {
		// Remove existing symlink, create non-empty dir
		testFolder := "Chats"
		symlinkPath := filepath.Join(docsDir, testFolder)
		os.Remove(symlinkPath)
		os.MkdirAll(symlinkPath, 0755)
		os.WriteFile(filepath.Join(symlinkPath, "existing.txt"), []byte("data"), 0644)

		if err := EnsurePerUserSymlinks(docsDir, "default"); err != nil {
			t.Fatalf("EnsurePerUserSymlinks failed: %v", err)
		}

		info, err := os.Lstat(symlinkPath)
		if err != nil {
			t.Fatalf("Lstat failed: %v", err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			t.Error("Non-empty directory should NOT have been replaced with symlink")
		}

		// Cleanup: restore symlink for other tests
		os.RemoveAll(symlinkPath)
		os.Symlink(filepath.Join("_users", "default", testFolder), symlinkPath)
	})
}

// --- MigratePerUserFolders ---

func TestMigratePerUserFolders(t *testing.T) {
	t.Run("MigratesRootFoldersToUsersDefault", func(t *testing.T) {
		docsDir, err := os.MkdirTemp("", "migrate-test-*")
		if err != nil {
			t.Fatalf("Failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(docsDir)

		// Create root-level Chats with content (simulates pre-migration state)
		chatsDir := filepath.Join(docsDir, "Chats")
		os.MkdirAll(chatsDir, 0755)
		os.WriteFile(filepath.Join(chatsDir, "old-session.json"), []byte(`{"id":"old"}`), 0644)

		count, err := MigratePerUserFolders(docsDir)
		if err != nil {
			t.Fatalf("MigratePerUserFolders failed: %v", err)
		}
		if count != 1 {
			t.Errorf("Expected 1 migrated folder, got %d", count)
		}

		// Verify file moved to _users/default/Chats
		migratedFile := filepath.Join(docsDir, "_users", GetDefaultUserID(), "Chats", "old-session.json")
		if _, err := os.Stat(migratedFile); os.IsNotExist(err) {
			t.Error("File was not migrated to _users/default/Chats/")
		}

		// Verify original root Chats/ was removed
		if _, err := os.Stat(chatsDir); !os.IsNotExist(err) {
			t.Error("Original root Chats/ should have been removed after migration")
		}
	})

	t.Run("SkipsAlreadyMigrated", func(t *testing.T) {
		docsDir, err := os.MkdirTemp("", "migrate-skip-test-*")
		if err != nil {
			t.Fatalf("Failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(docsDir)

		// Create symlink (already migrated state)
		userChats := filepath.Join(docsDir, "_users", GetDefaultUserID(), "Chats")
		os.MkdirAll(userChats, 0755)
		os.Symlink(filepath.Join("_users", GetDefaultUserID(), "Chats"), filepath.Join(docsDir, "Chats"))

		count, err := MigratePerUserFolders(docsDir)
		if err != nil {
			t.Fatalf("MigratePerUserFolders failed: %v", err)
		}
		if count != 0 {
			t.Errorf("Expected 0 migrated (already done), got %d", count)
		}
	})

	t.Run("SkipsEmptyRootFolders", func(t *testing.T) {
		docsDir, err := os.MkdirTemp("", "migrate-empty-test-*")
		if err != nil {
			t.Fatalf("Failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(docsDir)

		// Create empty root-level Chats (nothing to migrate)
		os.MkdirAll(filepath.Join(docsDir, "Chats"), 0755)

		count, err := MigratePerUserFolders(docsDir)
		if err != nil {
			t.Fatalf("MigratePerUserFolders failed: %v", err)
		}
		if count != 0 {
			t.Errorf("Expected 0 migrated (empty folder), got %d", count)
		}
	})

	t.Run("MergesPartialMigration", func(t *testing.T) {
		docsDir, err := os.MkdirTemp("", "migrate-merge-test-*")
		if err != nil {
			t.Fatalf("Failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(docsDir)

		// Create both root-level and _users/default/ Chats with different files
		rootChats := filepath.Join(docsDir, "Chats")
		userChats := filepath.Join(docsDir, "_users", GetDefaultUserID(), "Chats")
		os.MkdirAll(rootChats, 0755)
		os.MkdirAll(userChats, 0755)
		os.WriteFile(filepath.Join(rootChats, "root-file.json"), []byte("from-root"), 0644)
		os.WriteFile(filepath.Join(userChats, "user-file.json"), []byte("from-user"), 0644)

		count, err := MigratePerUserFolders(docsDir)
		if err != nil {
			t.Fatalf("MigratePerUserFolders failed: %v", err)
		}
		if count != 1 {
			t.Errorf("Expected 1 migrated, got %d", count)
		}

		// Both files should exist in destination
		if _, err := os.Stat(filepath.Join(userChats, "root-file.json")); os.IsNotExist(err) {
			t.Error("root-file.json not merged into _users/default/Chats/")
		}
		if _, err := os.Stat(filepath.Join(userChats, "user-file.json")); os.IsNotExist(err) {
			t.Error("user-file.json should still exist in _users/default/Chats/")
		}
	})
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

// --- Cross-User Isolation (Security) ---

func TestCrossUserIsolation(t *testing.T) {
	docsDir, err := os.MkdirTemp("", "cross-user-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(docsDir)

	t.Run("User1CannotAccessUser2Files", func(t *testing.T) {
		// Create user2's data
		user2Chats := filepath.Join(docsDir, "_users", "user2", "Chats")
		os.MkdirAll(user2Chats, 0755)
		os.WriteFile(filepath.Join(user2Chats, "secret.json"), []byte("user2 secret"), 0644)

		// user1 requests Chats/secret.json — should resolve to user1's dir, not user2's
		resolved, err := ResolveUserPath(docsDir, "Chats/secret.json", "user1")
		if err != nil {
			t.Fatalf("ResolveUserPath failed: %v", err)
		}
		expectedPrefix := filepath.Join(docsDir, "_users", "user1", "Chats")
		if !strings.HasPrefix(resolved, expectedPrefix) {
			t.Errorf("user1 path should resolve to user1 dir, got %q", resolved)
		}

		// Directly requesting _users/user2 path should be blocked
		_, err = ResolveUserPath(docsDir, "_users/user2/Chats/secret.json", "user1")
		if err == nil {
			t.Error("User1 should NOT be able to access _users/user2/ paths directly")
		}
	})

	t.Run("SharedFoldersSameForAllUsers", func(t *testing.T) {
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
}
