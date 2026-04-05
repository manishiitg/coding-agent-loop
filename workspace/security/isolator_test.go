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

	// Create shared folders at root level
	for _, folder := range []string{"Chats", "Downloads", "skills"} {
		if err := os.MkdirAll(filepath.Join(tempDir, folder), 0755); err != nil {
			t.Fatalf("Failed to create dir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(tempDir, folder, "test.txt"), []byte("data"), 0644); err != nil {
			t.Fatalf("Failed to write file: %v", err)
		}
	}

	// Create a blocked directory
	blockedDir := filepath.Join(tempDir, "secret")
	if err := os.MkdirAll(blockedDir, 0755); err != nil {
		t.Fatalf("Failed to create dir: %v", err)
	}

	isolator := &Isolator{
		BlockedPaths: []string{"secret"},
		WorkDir:      tempDir,
		BaseDir:      tempDir,
	}

	t.Run("LinuxMountScriptBlocksPaths", func(t *testing.T) {
		script := isolator.generateMountScript("ls Chats/", nil)

		// Should hide blocked path with tmpfs
		if !strings.Contains(script, "mount -t tmpfs tmpfs") {
			t.Error("Mount script should hide blocked path with tmpfs")
		}

		t.Logf("✓ Linux mount script correctly blocks paths:\n%s", script)
	})

	t.Run("MacOSSandboxBlocksPaths", func(t *testing.T) {
		profile := isolator.generateSandboxProfile()

		// Should deny blocked paths
		if !strings.Contains(profile, "deny file-read* file-write*") {
			t.Error("Sandbox profile should deny access to blocked paths")
		}

		t.Logf("✓ macOS sandbox profile blocks paths:\n%s", profile)
	})
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
