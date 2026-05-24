package server

import (
	"testing"

	mcpagent "github.com/manishiitg/mcpagent/agent"
	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

func TestRestoredRuntimeUsesLaunchableTransportFromHandle(t *testing.T) {
	runtime := &ChatHistoryAgentRuntime{
		Kind:     "coding_agent",
		Provider: "future-cli",
		AgentSessionHandle: &mcpagent.AgentSessionHandle{
			Provider: llmtypes.CodingProviderSessionHandle{
				Provider:        "future-cli",
				Transport:       llmtypes.CodingProviderTransportTmux,
				NativeSessionID: "native-thread-1",
				TmuxSession:     "mlp-future-1",
			},
		},
	}

	if !restoredRuntimeUsesLaunchableTerminalTransport(runtime) {
		t.Fatalf("expected launchable terminal transport")
	}
	tmuxSession, ok, reason := restoredRuntimeTmuxSession(runtime)
	if !ok || tmuxSession != "mlp-future-1" || reason != "" {
		t.Fatalf("restoredRuntimeTmuxSession = %q, %v, %q", tmuxSession, ok, reason)
	}
}

func TestRestoredRuntimeUsesLaunchableTransportFromRuntime(t *testing.T) {
	runtime := &ChatHistoryAgentRuntime{
		Kind:      "coding_agent",
		Provider:  "future-cli",
		Transport: "tmux",
		AgentSessionHandle: &mcpagent.AgentSessionHandle{
			Provider: llmtypes.CodingProviderSessionHandle{
				Provider:    "future-cli",
				TmuxSession: "mlp-future-runtime-1",
			},
		},
	}

	if !restoredRuntimeUsesLaunchableTerminalTransport(runtime) {
		t.Fatalf("expected launchable terminal transport")
	}
	tmuxSession, ok, reason := restoredRuntimeTmuxSession(runtime)
	if !ok || tmuxSession != "mlp-future-runtime-1" || reason != "" {
		t.Fatalf("restoredRuntimeTmuxSession = %q, %v, %q", tmuxSession, ok, reason)
	}
}

func TestRestoredRuntimeTmuxSessionRejectsStructuredTransport(t *testing.T) {
	runtime := &ChatHistoryAgentRuntime{
		Kind:     "coding_agent",
		Provider: "future-cli",
		AgentSessionHandle: &mcpagent.AgentSessionHandle{
			Provider: llmtypes.CodingProviderSessionHandle{
				Provider:  "future-cli",
				Transport: llmtypes.CodingProviderTransportStructured,
			},
		},
	}

	if restoredRuntimeUsesLaunchableTerminalTransport(runtime) {
		t.Fatalf("structured transport should not start tmux")
	}
	if _, ok, reason := restoredRuntimeTmuxSession(runtime); ok || reason != "not_tmux_transport" {
		t.Fatalf("restoredRuntimeTmuxSession ok=%v reason=%q, want not_tmux_transport", ok, reason)
	}
}
