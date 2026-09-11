package server

import (
	"strings"
	"testing"
	"time"
)

func TestProviderSetupSessionStreamsInteractivePTY(t *testing.T) {
	original := providerSetupCommands
	providerSetupCommands = map[string]map[string]providerSetupCommand{
		"codex-cli": {
			"authenticate": {
				command: "/bin/sh",
				args:    []string{"-c", `printf "Choose an option: "; read answer; printf "selected:%s\n" "$answer"`},
			},
		},
	}
	t.Cleanup(func() { providerSetupCommands = original })

	manager := newProviderSetupManager()
	session, err := manager.start("owner-1", "codex-cli", "authenticate", 100, 24)
	if err != nil {
		t.Fatalf("start setup: %v", err)
	}
	defer manager.remove(session.id, true)

	seed, chunks, unsubscribe := session.subscribe()
	defer unsubscribe()
	output := string(seed)
	deadline := time.After(5 * time.Second)
	inputSent := false
	for {
		if !inputSent && strings.Contains(output, "Choose an option:") {
			if err := session.write("two\n"); err != nil {
				t.Fatalf("write input: %v", err)
			}
			inputSent = true
		}
		if strings.Contains(output, "selected:two") {
			break
		}
		select {
		case chunk, ok := <-chunks:
			if !ok {
				t.Fatalf("session ended before expected output: %q", output)
			}
			output += string(chunk)
		case <-deadline:
			t.Fatalf("timed out waiting for interactive output: %q", output)
		}
	}

	for session.isRunning() {
		select {
		case <-deadline:
			t.Fatal("session did not finish")
		case <-time.After(10 * time.Millisecond):
		}
	}
	snapshot := session.snapshot()
	if snapshot.Status != "completed" || snapshot.ExitCode == nil || *snapshot.ExitCode != 0 {
		t.Fatalf("unexpected final snapshot: %+v", snapshot)
	}
}

func TestProviderSetupRejectsCommandsOutsideAllowlist(t *testing.T) {
	manager := newProviderSetupManager()
	if _, err := manager.start("owner-1", "unknown", "install", 100, 24); err == nil {
		t.Fatal("expected unsupported provider error")
	}
	if _, err := manager.start("owner-1", "codex-cli", "shell", 100, 24); err == nil {
		t.Fatal("expected unsupported action error")
	}
	if _, err := manager.start("owner-1", "codex-cli", "install", 100, 24); err == nil {
		t.Fatal("provider installation belongs to deployment, not an interactive setup session")
	}
}

func TestProviderSetupAllowlistIncludesAuthenticationOnly(t *testing.T) {
	want := map[string]providerSetupCommand{
		"claude-code": {command: "claude"},
		"codex-cli":   {command: "codex", args: []string{"login"}},
		"cursor-cli":  {command: "cursor-agent", args: []string{"login"}},
		"muse-cli":    {command: "muse", args: []string{"login"}},
	}
	for provider, expected := range want {
		actions, ok := providerSetupCommands[provider]
		if !ok {
			t.Fatalf("%s is missing from the guided setup allowlist", provider)
		}
		if _, exists := actions["install"]; exists {
			t.Fatalf("%s unexpectedly exposes installation through the user terminal", provider)
		}
		authenticate := actions["authenticate"]
		if authenticate.command != expected.command || strings.Join(authenticate.args, "\x00") != strings.Join(expected.args, "\x00") {
			t.Fatalf("unexpected %s authentication command: %#v", provider, authenticate)
		}
	}
}
