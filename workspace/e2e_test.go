package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"workspace/models"
)

// TestE2EFolderGuardMacOS is a REAL end-to-end test that validates folder guard
// This test actually starts the server, creates files, and verifies isolation works
func TestE2EFolderGuardMacOS(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("This test is for macOS only (uses sandbox-exec)")
	}

	// Setup test environment
	testDir, cleanup := setupE2ETestEnvironment(t)
	defer cleanup()

	// Start the workspace server
	serverURL, stopServer := startTestServer(t, testDir)
	defer stopServer()

	// Give server time to start
	time.Sleep(1 * time.Second)

	// Verify server is running
	if !waitForServer(t, serverURL, 10*time.Second) {
		t.Fatal("Server failed to start")
	}
	t.Log("✓ Server started successfully")

	// Run the actual feature tests
	t.Run("EnvironmentIsolation", func(t *testing.T) {
		testEnvironmentIsolationE2E(t, serverURL, testDir)
	})

	t.Run("FolderGuardBlocks", func(t *testing.T) {
		testFolderGuardBlocksUnauthorizedAccess(t, serverURL, testDir)
	})

	t.Run("FolderGuardAllows", func(t *testing.T) {
		testFolderGuardAllowsAuthorizedAccess(t, serverURL, testDir)
	})

	t.Run("ReadOnlyEnforcement", func(t *testing.T) {
		testReadOnlyPathEnforcement(t, serverURL, testDir)
	})
}

// setupE2ETestEnvironment creates a real test workspace
func setupE2ETestEnvironment(t *testing.T) (string, func()) {
	testDir, err := os.MkdirTemp("", "e2e-folder-guard-*")
	if err != nil {
		t.Fatalf("Failed to create test directory: %v", err)
	}

	// Create realistic directory structure
	dirs := []string{
		"allowed",
		"forbidden",
		"readwrite",
		"Downloads",
	}

	for _, dir := range dirs {
		path := filepath.Join(testDir, dir)
		if err := os.MkdirAll(path, 0755); err != nil {
			t.Fatalf("Failed to create directory %s: %v", dir, err)
		}
	}

	// Create test files
	testFiles := map[string]string{
		"allowed/public.txt":    "This is public data",
		"forbidden/secret.txt":  "This is secret data that should NOT be accessible",
		"readwrite/data.txt":    "This is writable data",
		"Downloads/file.txt":    "Downloads are always accessible",
	}

	for path, content := range testFiles {
		fullPath := filepath.Join(testDir, path)
		if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
			t.Fatalf("Failed to create file %s: %v", path, err)
		}
	}

	t.Logf("Test environment created at: %s", testDir)

	cleanup := func() {
		os.RemoveAll(testDir)
		t.Log("Test environment cleaned up")
	}

	return testDir, cleanup
}

// startTestServer starts the workspace API server for testing
func startTestServer(t *testing.T, docsDir string) (string, func()) {
	port := "18081" // Use different port to avoid conflicts
	serverURL := fmt.Sprintf("http://localhost:%s", port)

	// Build the server binary first
	buildCmd := exec.Command("go", "build", "-o", "/tmp/workspace-test-server", ".")
	if output, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("Failed to build server: %v\nOutput: %s", err, output)
	}

	// Start server in background
	cmd := exec.Command("/tmp/workspace-test-server", "server", "--docs-dir", docsDir, "--port", port)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}

	t.Logf("Server started with PID %d", cmd.Process.Pid)

	stopServer := func() {
		if cmd.Process != nil {
			cmd.Process.Kill()
			cmd.Wait()
		}
		os.Remove("/tmp/workspace-test-server")
		t.Log("Server stopped")
	}

	return serverURL, stopServer
}

// waitForServer waits for the server to become available
func waitForServer(t *testing.T, serverURL string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 1 * time.Second}

	for time.Now().Before(deadline) {
		resp, err := client.Get(serverURL + "/health")
		if err == nil && resp.StatusCode == http.StatusOK {
			resp.Body.Close()
			return true
		}
		time.Sleep(500 * time.Millisecond)
	}
	return false
}

// testEnvironmentIsolationE2E verifies that secrets are NOT leaked to subprocesses
func testEnvironmentIsolationE2E(t *testing.T, serverURL, testDir string) {
	// Set secrets in the environment (simulating real scenario)
	os.Setenv("DATABASE_URL", "postgresql://secret-connection-string")
	os.Setenv("API_KEY", "super-secret-api-key-12345")
	defer func() {
		os.Unsetenv("DATABASE_URL")
		os.Unsetenv("API_KEY")
	}()

	// Execute command that tries to access environment
	req := map[string]interface{}{
		"command": "env | sort",
		"folder_guard": map[string]interface{}{
			"enabled":     true,
			"read_paths":  []string{},
			"write_paths": []string{},
		},
	}

	resp := executeShellCommandE2E(t, serverURL, req)

	// Verify secrets are NOT in the output
	output := resp.Data.Stdout + resp.Data.Stderr

	if strings.Contains(output, "DATABASE_URL") {
		t.Error("SECURITY FAILURE: DATABASE_URL leaked to subprocess!")
		t.Logf("Output: %s", output)
	}

	if strings.Contains(output, "API_KEY") {
		t.Error("SECURITY FAILURE: API_KEY leaked to subprocess!")
		t.Logf("Output: %s", output)
	}

	if strings.Contains(output, "super-secret") {
		t.Error("SECURITY FAILURE: Secret value leaked to subprocess!")
		t.Logf("Output: %s", output)
	}

	// Verify safe environment IS present
	if !strings.Contains(output, "PATH=") {
		t.Error("Safe environment missing PATH variable")
	}

	t.Log("✓ Environment isolation working - no secrets leaked")
}

// testFolderGuardBlocksUnauthorizedAccess verifies folder guard blocks access to forbidden paths
func testFolderGuardBlocksUnauthorizedAccess(t *testing.T, serverURL, testDir string) {
	allowedPath := filepath.Join(testDir, "allowed")
	forbiddenPath := filepath.Join(testDir, "forbidden")

	// Try to access forbidden path when only 'allowed' is permitted
	req := map[string]interface{}{
		"command": fmt.Sprintf("cat %s/secret.txt", forbiddenPath),
		"folder_guard": map[string]interface{}{
			"enabled":     true,
			"read_paths":  []string{allowedPath},
			"write_paths": []string{},
		},
	}

	resp := executeShellCommandE2E(t, serverURL, req)

	// Should fail - file should not be readable
	if resp.Data.ExitCode == 0 {
		t.Errorf("SECURITY FAILURE: Forbidden path was accessible!")
		t.Logf("Stdout: %s", resp.Data.Stdout)
		t.Logf("Stderr: %s", resp.Data.Stderr)
	} else {
		t.Logf("✓ Folder guard blocked unauthorized access (exit code: %d)", resp.Data.ExitCode)
	}

	// Verify output doesn't contain secret data
	output := resp.Data.Stdout + resp.Data.Stderr
	if strings.Contains(output, "secret data that should NOT") {
		t.Error("SECURITY FAILURE: Secret data was read from forbidden path!")
	}
}

// testFolderGuardAllowsAuthorizedAccess verifies folder guard allows access to permitted paths
func testFolderGuardAllowsAuthorizedAccess(t *testing.T, serverURL, testDir string) {
	allowedPath := filepath.Join(testDir, "allowed")

	// Access allowed path
	req := map[string]interface{}{
		"command": fmt.Sprintf("cat %s/public.txt", allowedPath),
		"folder_guard": map[string]interface{}{
			"enabled":     true,
			"read_paths":  []string{allowedPath},
			"write_paths": []string{},
		},
	}

	resp := executeShellCommandE2E(t, serverURL, req)

	if resp.Data.ExitCode != 0 {
		t.Errorf("Failed to read allowed path (exit code: %d)", resp.Data.ExitCode)
		t.Logf("Stderr: %s", resp.Data.Stderr)
	}

	if !strings.Contains(resp.Data.Stdout, "This is public data") {
		t.Errorf("Expected to read public data, got: %s", resp.Data.Stdout)
	} else {
		t.Log("✓ Folder guard allowed access to permitted path")
	}
}

// testReadOnlyPathEnforcement verifies that read-only paths cannot be written to
func testReadOnlyPathEnforcement(t *testing.T, serverURL, testDir string) {
	allowedPath := filepath.Join(testDir, "allowed")
	readwritePath := filepath.Join(testDir, "readwrite")

	// Try to write to read-only path (should FAIL)
	t.Run("WriteToReadOnlyFails", func(t *testing.T) {
		req := map[string]interface{}{
			"command": fmt.Sprintf("echo 'hacked' > %s/hacked.txt", allowedPath),
			"folder_guard": map[string]interface{}{
				"enabled":     true,
				"read_paths":  []string{allowedPath},
				"write_paths": []string{}, // No write permission
			},
		}

		resp := executeShellCommandE2E(t, serverURL, req)

		if resp.Data.ExitCode == 0 {
			t.Error("SECURITY FAILURE: Write to read-only path succeeded!")
		} else {
			t.Logf("✓ Write to read-only path blocked (exit code: %d)", resp.Data.ExitCode)
		}

		// Verify file was NOT created
		hackedFile := filepath.Join(allowedPath, "hacked.txt")
		if _, err := os.Stat(hackedFile); !os.IsNotExist(err) {
			t.Error("SECURITY FAILURE: File was created in read-only path!")
			os.Remove(hackedFile)
		}
	})

	// Write to read-write path (should SUCCEED)
	t.Run("WriteToReadWriteSucceeds", func(t *testing.T) {
		testFile := filepath.Join(readwritePath, "test_write.txt")
		req := map[string]interface{}{
			"command": fmt.Sprintf("echo 'test data' > %s && cat %s", testFile, testFile),
			"folder_guard": map[string]interface{}{
				"enabled":     true,
				"read_paths":  []string{},
				"write_paths": []string{readwritePath},
			},
		}

		resp := executeShellCommandE2E(t, serverURL, req)

		if resp.Data.ExitCode != 0 {
			t.Errorf("Failed to write to read-write path (exit code: %d)", resp.Data.ExitCode)
			t.Logf("Stderr: %s", resp.Data.Stderr)
		}

		if !strings.Contains(resp.Data.Stdout, "test data") {
			t.Errorf("Expected to read written data, got: %s", resp.Data.Stdout)
		} else {
			t.Log("✓ Write to read-write path succeeded")
		}

		// Cleanup
		os.Remove(testFile)
	})
}

// executeShellCommandE2E makes an HTTP request to execute a shell command
func executeShellCommandE2E(t *testing.T, serverURL string, requestBody map[string]interface{}) *models.APIResponse[models.ExecuteShellResponse] {
	jsonBody, err := json.Marshal(requestBody)
	if err != nil {
		t.Fatalf("Failed to marshal request: %v", err)
	}

	req, err := http.NewRequest("POST", serverURL+"/api/execute", bytes.NewBuffer(jsonBody))
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Failed to read response: %v", err)
	}

	var apiResp models.APIResponse[models.ExecuteShellResponse]
	if err := json.Unmarshal(body, &apiResp); err != nil {
		t.Fatalf("Failed to unmarshal response: %v\nBody: %s", err, string(body))
	}

	return &apiResp
}
