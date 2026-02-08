package security

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestEnvironment holds test fixtures
type TestEnvironment struct {
	TempDir       string
	ReadOnlyDir   string
	ReadWriteDir  string
	ForbiddenDir  string
	DownloadsDir  string
	CleanupFuncs  []func()
	t             *testing.T
}

// Setup creates test directories and files
func (te *TestEnvironment) Setup(t *testing.T) error {
	te.t = t

	// Create temp directory for tests
	tempDir, err := os.MkdirTemp("", "folder-guard-test-*")
	if err != nil {
		return fmt.Errorf("failed to create temp dir: %w", err)
	}
	te.TempDir = tempDir
	te.CleanupFuncs = append(te.CleanupFuncs, func() {
		os.RemoveAll(tempDir)
	})

	// Create test directories
	te.ReadOnlyDir = filepath.Join(tempDir, "readonly")
	te.ReadWriteDir = filepath.Join(tempDir, "readwrite")
	te.ForbiddenDir = filepath.Join(tempDir, "forbidden")
	te.DownloadsDir = filepath.Join(tempDir, "Downloads")

	for _, dir := range []string{te.ReadOnlyDir, te.ReadWriteDir, te.ForbiddenDir, te.DownloadsDir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create test dir %s: %w", dir, err)
		}
	}

	// Create test files
	testFiles := map[string]string{
		filepath.Join(te.ReadOnlyDir, "public.txt"):    "public data",
		filepath.Join(te.ReadWriteDir, "data.txt"):     "writable data",
		filepath.Join(te.ForbiddenDir, "secret.txt"):   "secret data",
		filepath.Join(te.DownloadsDir, "download.txt"): "download data",
	}

	for path, content := range testFiles {
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			return fmt.Errorf("failed to write test file %s: %w", path, err)
		}
	}

	t.Logf("Test environment created at: %s", tempDir)
	return nil
}

// Cleanup removes all test resources
func (te *TestEnvironment) Cleanup() {
	for _, cleanup := range te.CleanupFuncs {
		cleanup()
	}
}

// TestIsolatorOSDetection tests that the right isolation method is chosen
func TestIsolatorOSDetection(t *testing.T) {
	env := &TestEnvironment{}
	if err := env.Setup(t); err != nil {
		t.Fatalf("Failed to setup test environment: %v", err)
	}
	defer env.Cleanup()

	isolator := &Isolator{
		ReadPaths:  []string{env.ReadOnlyDir},
		WritePaths: []string{env.ReadWriteDir},
		WorkDir:    env.TempDir,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd, cleanup, err := isolator.ExecuteIsolated(ctx, "echo", []string{"test"})
	if cleanup != nil {
		defer cleanup()
	}

	if err != nil {
		t.Fatalf("ExecuteIsolated failed: %v", err)
	}

	// Verify the right command is used based on OS
	if runtime.GOOS == "darwin" {
		if cmd.Args[0] != "sandbox-exec" {
			t.Errorf("Expected sandbox-exec on macOS, got: %s", cmd.Args[0])
		}
		t.Logf("✓ macOS: Using sandbox-exec")
	} else if runtime.GOOS == "linux" {
		if cmd.Args[0] != "unshare" {
			t.Errorf("Expected unshare on Linux, got: %s", cmd.Args[0])
		}
		t.Logf("✓ Linux: Using unshare")
	}

	// Verify safe environment is set
	if len(cmd.Env) == 0 {
		t.Error("Expected safe environment to be set, but Env is empty")
	}

	hasPath := false
	for _, envVar := range cmd.Env {
		if strings.HasPrefix(envVar, "PATH=") {
			hasPath = true
		}
		// Check for leaked secrets
		if strings.Contains(strings.ToUpper(envVar), "DATABASE") ||
			strings.Contains(strings.ToUpper(envVar), "SECRET") ||
			strings.Contains(strings.ToUpper(envVar), "API_KEY") {
			t.Errorf("SECURITY VIOLATION: Found secret in environment: %s", envVar)
		}
	}
	if !hasPath {
		t.Error("Safe environment missing PATH variable")
	}
	t.Logf("✓ Safe environment configured with %d variables", len(cmd.Env))
}

// TestMacOSSandboxProfile tests the macOS sandbox profile generation
func TestMacOSSandboxProfile(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Skipping macOS-specific test")
	}

	env := &TestEnvironment{}
	if err := env.Setup(t); err != nil {
		t.Fatalf("Failed to setup test environment: %v", err)
	}
	defer env.Cleanup()

	isolator := &Isolator{
		ReadPaths:  []string{env.ReadOnlyDir},
		WritePaths: []string{env.ReadWriteDir},
		WorkDir:    env.TempDir,
	}

	profile := isolator.generateSandboxProfile()

	// Verify profile structure
	if !strings.Contains(profile, "(version 1)") {
		t.Error("Sandbox profile missing version declaration")
	}
	if !strings.Contains(profile, "(allow default)") {
		t.Error("Sandbox profile missing default allow")
	}
	// CRITICAL: Should deny BOTH read and write access by default for security
	if !strings.Contains(profile, "(deny file-read* file-write*") {
		t.Error("Sandbox profile missing workspace read+write denial")
	}

	// Verify paths are included
	if !strings.Contains(profile, env.ReadOnlyDir) {
		t.Errorf("Read-only path not in profile: %s", env.ReadOnlyDir)
	}
	if !strings.Contains(profile, env.ReadWriteDir) {
		t.Errorf("Read-write path not in profile: %s", env.ReadWriteDir)
	}

	t.Logf("✓ Sandbox profile generated correctly:\n%s", profile)
}

// TestEnvironmentIsolation tests that BuildSafeEnvironment is applied
func TestEnvironmentIsolation(t *testing.T) {
	env := &TestEnvironment{}
	if err := env.Setup(t); err != nil {
		t.Fatalf("Failed to setup test environment: %v", err)
	}
	defer env.Cleanup()

	// Set a secret environment variable in the parent process
	os.Setenv("DATABASE_URL", "postgresql://secret")
	os.Setenv("API_KEY", "super-secret-key")
	defer func() {
		os.Unsetenv("DATABASE_URL")
		os.Unsetenv("API_KEY")
	}()

	isolator := &Isolator{
		ReadPaths:  []string{},
		WritePaths: []string{env.TempDir},
		WorkDir:    env.TempDir,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Execute command that prints environment
	cmd, cleanup, err := isolator.ExecuteIsolated(ctx, "sh", []string{"-c", "env"})
	if cleanup != nil {
		defer cleanup()
	}
	if err != nil {
		t.Fatalf("ExecuteIsolated failed: %v", err)
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		// Command may fail due to isolation, but we can still check output
		t.Logf("Command exited with error (may be expected): %v", err)
	}

	outputStr := string(output)
	t.Logf("Environment output:\n%s", outputStr)

	// CRITICAL: Verify secrets are NOT leaked
	if strings.Contains(outputStr, "DATABASE_URL") {
		t.Error("SECURITY VIOLATION: DATABASE_URL leaked to subprocess")
	}
	if strings.Contains(outputStr, "API_KEY") {
		t.Error("SECURITY VIOLATION: API_KEY leaked to subprocess")
	}
	if strings.Contains(outputStr, "super-secret-key") {
		t.Error("SECURITY VIOLATION: Secret value leaked to subprocess")
	}

	// Verify safe environment is present
	if !strings.Contains(outputStr, "PATH=") {
		t.Error("Safe environment missing PATH")
	}

	t.Log("✓ Environment isolation working correctly")
}

// TestLinuxMountIsolation tests Linux-specific mount namespace isolation
func TestLinuxMountIsolation(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Skipping Linux-specific test")
	}

	env := &TestEnvironment{}
	if err := env.Setup(t); err != nil {
		t.Fatalf("Failed to setup test environment: %v", err)
	}
	defer env.Cleanup()

	// Check if unshare is available and we have permissions
	if _, err := exec.LookPath("unshare"); err != nil {
		t.Skip("unshare not available")
	}

	// Test that unshare works (requires SYS_ADMIN capability in Docker)
	testCmd := exec.Command("unshare", "-m", "echo", "test")
	if err := testCmd.Run(); err != nil {
		t.Skip("unshare requires SYS_ADMIN capability (add --cap-add=SYS_ADMIN to Docker)")
	}

	isolator := &Isolator{
		ReadPaths:  []string{env.ReadOnlyDir},
		WritePaths: []string{env.ReadWriteDir},
		WorkDir:    env.TempDir,
	}

	// Test reading from read-only path
	t.Run("ReadFromReadOnlyPath", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		cmd, cleanup, err := isolator.ExecuteIsolated(ctx, "sh", []string{"-c", fmt.Sprintf("cat %s/public.txt", env.ReadOnlyDir)})
		if cleanup != nil {
			defer cleanup()
		}
		if err != nil {
			t.Fatalf("ExecuteIsolated failed: %v", err)
		}

		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Errorf("Failed to read from read-only path: %v\nOutput: %s", err, output)
		}

		if !strings.Contains(string(output), "public data") {
			t.Errorf("Expected to read 'public data', got: %s", output)
		}
		t.Log("✓ Read from read-only path succeeded")
	})

	// Test writing to read-write path
	t.Run("WriteToReadWritePath", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		testFile := filepath.Join(env.ReadWriteDir, "test_write.txt")
		cmd, cleanup, err := isolator.ExecuteIsolated(ctx, "sh", []string{"-c", fmt.Sprintf("echo 'test' > %s", testFile)})
		if cleanup != nil {
			defer cleanup()
		}
		if err != nil {
			t.Fatalf("ExecuteIsolated failed: %v", err)
		}

		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Errorf("Failed to write to read-write path: %v\nOutput: %s", err, output)
		}

		// Verify file was created
		if _, err := os.Stat(testFile); os.IsNotExist(err) {
			t.Error("File was not created in read-write path")
		} else {
			t.Log("✓ Write to read-write path succeeded")
		}
	})

	// Test that writing to read-only path fails
	t.Run("WriteToReadOnlyPath", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		cmd, cleanup, err := isolator.ExecuteIsolated(ctx, "sh", []string{"-c", fmt.Sprintf("echo 'test' > %s/test_write.txt", env.ReadOnlyDir)})
		if cleanup != nil {
			defer cleanup()
		}
		if err != nil {
			t.Fatalf("ExecuteIsolated failed: %v", err)
		}

		output, err := cmd.CombinedOutput()
		// Should fail
		if err == nil {
			t.Errorf("SECURITY VIOLATION: Write to read-only path succeeded!\nOutput: %s", output)
		} else {
			t.Logf("✓ Write to read-only path correctly blocked: %v", err)
		}
	})
}

// TestMacOSSandboxIsolation tests macOS sandbox-exec isolation
func TestMacOSSandboxIsolation(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Skipping macOS-specific test")
	}

	env := &TestEnvironment{}
	if err := env.Setup(t); err != nil {
		t.Fatalf("Failed to setup test environment: %v", err)
	}
	defer env.Cleanup()

	// Override base dir for macOS testing (use temp dir instead of /app/workspace-docs)
	// We'll need to modify the isolator to accept base dir as parameter
	// For now, we test that the mechanism works

	isolator := &Isolator{
		ReadPaths:  []string{env.ReadOnlyDir},
		WritePaths: []string{env.ReadWriteDir},
		WorkDir:    env.TempDir,
	}

	// Test basic command execution
	t.Run("BasicExecution", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		cmd, cleanup, err := isolator.ExecuteIsolated(ctx, "echo", []string{"test"})
		if cleanup != nil {
			defer cleanup()
		}
		if err != nil {
			t.Fatalf("ExecuteIsolated failed: %v", err)
		}

		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Errorf("Command execution failed: %v\nOutput: %s", err, output)
		}

		if !strings.Contains(string(output), "test") {
			t.Errorf("Expected output 'test', got: %s", output)
		}
		t.Log("✓ Basic command execution works with sandbox-exec")
	})

	// Test file operations
	t.Run("FileOperations", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		// Test reading
		cmd, cleanup, err := isolator.ExecuteIsolated(ctx, "sh", []string{"-c", fmt.Sprintf("cat %s/data.txt", env.ReadWriteDir)})
		if cleanup != nil {
			defer cleanup()
		}
		if err != nil {
			t.Fatalf("ExecuteIsolated failed: %v", err)
		}

		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Logf("Read test output: %s", output)
		}

		t.Log("✓ File operations completed with sandbox-exec")
	})
}

// TestDownloadsFolderException tests that Downloads is always accessible
func TestDownloadsFolderException(t *testing.T) {
	env := &TestEnvironment{}
	if err := env.Setup(t); err != nil {
		t.Fatalf("Failed to setup test environment: %v", err)
	}
	defer env.Cleanup()

	// Note: This test checks the profile/script generation
	// Actual enforcement requires /app/workspace-docs structure

	isolator := &Isolator{
		ReadPaths:  []string{}, // No paths allowed
		WritePaths: []string{}, // No paths allowed
		WorkDir:    env.TempDir,
	}

	if runtime.GOOS == "darwin" {
		profile := isolator.generateSandboxProfile()
		if !strings.Contains(profile, "Downloads") {
			t.Error("Downloads folder not found in macOS sandbox profile")
		} else {
			t.Log("✓ Downloads folder included in macOS sandbox profile")
		}
	} else if runtime.GOOS == "linux" {
		script := isolator.generateMountScript("echo", []string{"test"})
		if !strings.Contains(script, "Downloads") {
			t.Error("Downloads folder not found in Linux mount script")
		} else {
			t.Log("✓ Downloads folder included in Linux mount script")
		}
	}
}

// TestDenyListSymlinkFixup tests that Mode 1 (deny-list) preserves symlinks
// pointing into blocked paths by generating fixup mount/allow commands.
func TestDenyListSymlinkFixup(t *testing.T) {
	// Create a temp directory simulating /app/workspace-docs
	tempDir, err := os.MkdirTemp("", "isolator-symlink-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create _users/default/Chats, _users/default/Plans, _users/default/Downloads
	usersDir := filepath.Join(tempDir, "_users", "default")
	for _, folder := range []string{"Chats", "Plans", "Downloads"} {
		if err := os.MkdirAll(filepath.Join(usersDir, folder), 0755); err != nil {
			t.Fatalf("Failed to create dir: %v", err)
		}
		// Create a test file in each
		if err := os.WriteFile(filepath.Join(usersDir, folder, "test.txt"), []byte("data"), 0644); err != nil {
			t.Fatalf("Failed to write file: %v", err)
		}
	}

	// Create symlinks: Chats -> _users/default/Chats, etc.
	for _, folder := range []string{"Chats", "Plans", "Downloads"} {
		symlinkPath := filepath.Join(tempDir, folder)
		target := filepath.Join("_users", "default", folder)
		if err := os.Symlink(target, symlinkPath); err != nil {
			t.Fatalf("Failed to create symlink %s -> %s: %v", folder, target, err)
		}
	}

	// Also create a user2 directory (should remain hidden)
	user2Dir := filepath.Join(tempDir, "_users", "user2", "Chats")
	if err := os.MkdirAll(user2Dir, 0755); err != nil {
		t.Fatalf("Failed to create user2 dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(user2Dir, "secret.txt"), []byte("user2 secret"), 0644); err != nil {
		t.Fatalf("Failed to write user2 file: %v", err)
	}

	// Create a non-symlink directory (should not be affected)
	skillsDir := filepath.Join(tempDir, "skills")
	if err := os.MkdirAll(skillsDir, 0755); err != nil {
		t.Fatalf("Failed to create skills dir: %v", err)
	}

	isolator := &Isolator{
		BlockedPaths: []string{"_users"},
		WorkDir:      tempDir,
		BaseDir:      tempDir,
	}

	t.Run("LinuxMountScriptFixesSymlinks", func(t *testing.T) {
		script := isolator.generateMountScript("ls Chats/", nil)

		// Should preserve workspace first (bind mount to temp)
		if !strings.Contains(script, "mount --bind") {
			t.Error("Mount script should preserve workspace with bind mount for symlink fixup")
		}

		// Should hide _users with tmpfs
		if !strings.Contains(script, "mount -t tmpfs tmpfs") {
			t.Error("Mount script should hide _users/ with tmpfs")
		}

		// Should fix each symlink target
		for _, folder := range []string{"Chats", "Plans", "Downloads"} {
			expectedTarget := filepath.Join("_users", "default", folder)
			if !strings.Contains(script, expectedTarget) {
				t.Errorf("Mount script should fix symlink for %s (target: %s)", folder, expectedTarget)
			}
		}

		// Should NOT reference user2 (only current user's symlink targets)
		if strings.Contains(script, "user2") {
			t.Error("Mount script should NOT reference user2 directory")
		}

		t.Logf("✓ Linux mount script correctly fixes symlinks:\n%s", script)
	})

	t.Run("MacOSSandboxAllowsSymlinkTargets", func(t *testing.T) {
		profile := isolator.generateSandboxProfile()

		// Should deny _users
		if !strings.Contains(profile, "deny file-read* file-write*") {
			t.Error("Sandbox profile should deny access to blocked paths")
		}

		// Should re-allow symlink targets within _users/default/
		for _, folder := range []string{"Chats", "Plans", "Downloads"} {
			expectedPath := filepath.Join(tempDir, "_users", "default", folder)
			if !strings.Contains(profile, expectedPath) {
				t.Errorf("Sandbox profile should allow symlink target: %s", expectedPath)
			}
		}

		// Should NOT allow user2's paths
		if strings.Contains(profile, "user2") {
			t.Error("Sandbox profile should NOT allow user2 paths")
		}

		t.Logf("✓ macOS sandbox profile allows symlink targets:\n%s", profile)
	})
}

// TestDenyListNoSymlinks tests Mode 1 when there are no symlinks to fix
func TestDenyListNoSymlinks(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "isolator-nosymlink-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create _users/ directory (blocked) but no symlinks pointing into it
	if err := os.MkdirAll(filepath.Join(tempDir, "_users", "default"), 0755); err != nil {
		t.Fatalf("Failed to create dir: %v", err)
	}

	// Create a regular directory (not a symlink)
	if err := os.MkdirAll(filepath.Join(tempDir, "skills"), 0755); err != nil {
		t.Fatalf("Failed to create dir: %v", err)
	}

	isolator := &Isolator{
		BlockedPaths: []string{"_users"},
		WorkDir:      tempDir,
		BaseDir:      tempDir,
	}

	script := isolator.generateMountScript("echo test", nil)

	// Should still hide _users with tmpfs
	if !strings.Contains(script, "mount -t tmpfs") {
		t.Error("Mount script should hide _users/ with tmpfs")
	}

	// Should NOT preserve workspace (no symlinks to fix)
	if strings.Contains(script, "workspace-original") {
		t.Error("Mount script should NOT preserve workspace when no symlinks need fixing")
	}

	t.Logf("✓ No-symlink case handled correctly:\n%s", script)
}

// TestDenyListWithMultiUser tests Mode 1 with multiple users
func TestDenyListWithMultiUser(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "isolator-multiuser-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create multiple user directories
	for _, user := range []string{"default", "alice", "bob"} {
		for _, folder := range []string{"Chats", "Plans", "Downloads"} {
			if err := os.MkdirAll(filepath.Join(tempDir, "_users", user, folder), 0755); err != nil {
				t.Fatalf("Failed to create dir: %v", err)
			}
		}
	}

	// Symlinks point to default user (single-user mode)
	for _, folder := range []string{"Chats", "Plans", "Downloads"} {
		target := filepath.Join("_users", "default", folder)
		if err := os.Symlink(target, filepath.Join(tempDir, folder)); err != nil {
			t.Fatalf("Failed to create symlink: %v", err)
		}
	}

	isolator := &Isolator{
		BlockedPaths: []string{"_users"},
		WorkDir:      tempDir,
		BaseDir:      tempDir,
	}

	script := isolator.generateMountScript("ls Chats/", nil)

	// Should only expose default user's paths (from symlinks), not alice or bob
	if strings.Contains(script, "alice") {
		t.Error("Mount script should NOT expose alice's data")
	}
	if strings.Contains(script, "bob") {
		t.Error("Mount script should NOT expose bob's data")
	}

	// Should expose default user's symlink targets
	for _, folder := range []string{"Chats", "Plans", "Downloads"} {
		target := filepath.Join("_users", "default", folder)
		if !strings.Contains(script, target) {
			t.Errorf("Mount script should expose default user's %s", folder)
		}
	}

	t.Logf("✓ Multi-user isolation correct - only default user's symlink targets exposed")
}

// BenchmarkIsolatorOverhead measures the performance overhead of isolation
func BenchmarkIsolatorOverhead(b *testing.B) {
	env := &TestEnvironment{}
	if err := env.Setup(&testing.T{}); err != nil {
		b.Fatalf("Failed to setup test environment: %v", err)
	}
	defer env.Cleanup()

	isolator := &Isolator{
		ReadPaths:  []string{env.ReadOnlyDir},
		WritePaths: []string{env.ReadWriteDir},
		WorkDir:    env.TempDir,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		cmd, cleanup, err := isolator.ExecuteIsolated(ctx, "echo", []string{"test"})
		if err != nil {
			b.Fatalf("ExecuteIsolated failed: %v", err)
		}

		if err := cmd.Run(); err != nil {
			b.Logf("Command failed: %v", err)
		}

		if cleanup != nil {
			cleanup()
		}
		cancel()
	}
}
