package main

import (
	"os"
	"strings"
	"testing"
)

// Ctrl+C normally gives the Go server time to close its registered provider
// sessions. The launcher's tmux sweep is the backstop for a compiled `go run`
// child that exits before graceful cleanup, so its provider list must stay in
// sync with the adapters. This pins the exact omission that left Muse panes
// alive after stopping a local stack.
func TestRunServerCtrlCCleanupCoversEveryCodingCLI(t *testing.T) {
	script, err := os.ReadFile("run_server_with_logging.sh")
	if err != nil {
		t.Fatalf("read launcher: %v", err)
	}
	text := string(script)
	for _, pattern := range []string{
		"mlp-claude-code-*",
		"mlp-codex-cli-*",
		"mlp-cursor-cli-*",
		"mlp-pi-cli-*",
		"mlp-muse-*",
	} {
		if !strings.Contains(text, pattern) {
			t.Errorf("Ctrl+C cleanup is missing provider tmux pattern %q", pattern)
		}
	}
}

func TestRunServerRuntimeDiagnosticsAreControlledOnlyByFlag(t *testing.T) {
	script, err := os.ReadFile("run_server_with_logging.sh")
	if err != nil {
		t.Fatalf("read launcher: %v", err)
	}
	text := string(script)

	// The gate must be applied after .env loading and explicitly clear both
	// variables in the ordinary (no-flag) path. Otherwise a legacy .env entry
	// silently enables the diagnostic terminal rail.
	envLoad := strings.Index(text, "source_exported_env_file ../agent_go/.env")
	gate := strings.Index(text, "# Runtime diagnostics are controlled only by the explicit command-line switch.")
	if envLoad < 0 || gate < 0 || gate <= envLoad {
		t.Fatal("runtime diagnostics gate must be applied after loading .env")
	}
	gateText := text[gate:]
	for _, expected := range []string{
		"if [ \"$ENABLE_CHAT_TERMINAL_DEBUGS\" = true ]; then",
		"export AGENTWORKS_RUNTIME_DEBUG=1",
		"export VITE_RUNTIME_DEBUG=1",
		"unset AGENTWORKS_RUNTIME_DEBUG",
		"unset VITE_RUNTIME_DEBUG",
	} {
		if !strings.Contains(gateText, expected) {
			t.Errorf("runtime diagnostics gate is missing %q", expected)
		}
	}
}
