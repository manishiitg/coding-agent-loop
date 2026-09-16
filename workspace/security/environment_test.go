package security

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestNativeEnvironmentRepairsPath(t *testing.T) {
	t.Setenv("NATIVE_WORKSPACE", "true")
	t.Setenv("HOME", "/tmp/native-home")
	t.Setenv("PATH", "/custom/bin")

	env := BuildSafeEnvironment()
	pathValue := ""
	for _, kv := range env {
		if strings.HasPrefix(kv, "PATH=") {
			pathValue = strings.TrimPrefix(kv, "PATH=")
			break
		}
	}
	if pathValue == "" {
		t.Fatalf("expected PATH in native environment")
	}

	required := []string{
		"/custom/bin",
		"/tmp/native-home/.local/bin",
		"/tmp/native-home/go/bin",
		"/opt/homebrew/bin",
		"/usr/local/bin",
		"/usr/bin",
	}
	for _, path := range required {
		if !pathInList(pathValue, path) {
			t.Fatalf("expected PATH to contain %q, got %q", path, pathValue)
		}
	}
}

func TestNativeEnvironmentDoesNotExposeWorkspaceExecutionToken(t *testing.T) {
	t.Setenv("NATIVE_WORKSPACE", "true")
	t.Setenv("WORKSPACE_API_TOKEN", "server-only-token")
	for _, entry := range BuildSafeEnvironment() {
		if strings.HasPrefix(entry, "WORKSPACE_API_TOKEN=") {
			t.Fatal("workspace execution token leaked into shell environment")
		}
	}
}

func TestNativeEnvironmentAllowsPipInstallOnExternallyManagedPython(t *testing.T) {
	t.Setenv("NATIVE_WORKSPACE", "true")

	found := false
	for _, entry := range BuildSafeEnvironment() {
		if entry == "PIP_BREAK_SYSTEM_PACKAGES=1" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected native environment to opt out of PEP 668's externally-managed-environment guard")
	}
}

func TestDockerEnvironmentUsesConfiguredBrowserExecutable(t *testing.T) {
	t.Setenv("NATIVE_WORKSPACE", "")
	t.Setenv("AGENT_BROWSER_EXECUTABLE_PATH", "/usr/bin/google-chrome")

	foundAgentBrowser := false
	foundHyperFramesBrowser := false
	for _, entry := range BuildSafeEnvironment() {
		if entry == "AGENT_BROWSER_EXECUTABLE_PATH=/usr/bin/google-chrome" {
			foundAgentBrowser = true
		}
		if entry == "HYPERFRAMES_BROWSER_PATH=/usr/bin/google-chrome" {
			foundHyperFramesBrowser = true
		}
	}
	if !foundAgentBrowser || !foundHyperFramesBrowser {
		t.Fatalf("configured browser executable was not preserved for both runtimes: agent-browser=%v hyperframes=%v", foundAgentBrowser, foundHyperFramesBrowser)
	}
}

func TestSafeEnvironmentAllowsOnlyCurrentUsersRootlessDockerSocket(t *testing.T) {
	t.Setenv("NATIVE_WORKSPACE", "")
	expected := fmt.Sprintf("unix:///run/user/%d/docker.sock", os.Getuid())
	t.Setenv("DOCKER_HOST", expected)

	if !environmentContains(BuildSafeEnvironment(), "DOCKER_HOST="+expected) {
		t.Fatal("expected current user's rootless Docker socket to be preserved")
	}

	t.Setenv("DOCKER_HOST", "unix:///var/run/docker.sock")
	if environmentHasPrefix(BuildSafeEnvironment(), "DOCKER_HOST=") {
		t.Fatal("privileged host Docker socket leaked into workspace environment")
	}

	t.Setenv("DOCKER_HOST", "tcp://127.0.0.1:2375")
	if environmentHasPrefix(BuildSafeEnvironment(), "DOCKER_HOST=") {
		t.Fatal("arbitrary Docker endpoint leaked into workspace environment")
	}
}

func environmentContains(env []string, target string) bool {
	for _, entry := range env {
		if entry == target {
			return true
		}
	}
	return false
}

func environmentHasPrefix(env []string, prefix string) bool {
	for _, entry := range env {
		if strings.HasPrefix(entry, prefix) {
			return true
		}
	}
	return false
}

func pathInList(pathValue, target string) bool {
	for _, path := range strings.Split(pathValue, ":") {
		if path == target {
			return true
		}
	}
	return false
}
