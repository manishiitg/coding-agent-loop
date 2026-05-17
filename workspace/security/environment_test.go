package security

import (
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

func pathInList(pathValue, target string) bool {
	for _, path := range strings.Split(pathValue, ":") {
		if path == target {
			return true
		}
	}
	return false
}
