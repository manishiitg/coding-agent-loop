package server

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	mcpagent "github.com/manishiitg/mcpagent/agent"
)

// TestSetupWorkflowPhaseToolsForRestoreSkipsNonWorkflowChats verifies that the
// restore helper returns nil (no error, no registration) for chats that aren't
// a workflow phase — e.g. a plain multi-agent chat with no WorkspacePath and no
// WorkshopMode. Without this check the helper would attempt manifest reads and
// possibly register against an empty workspace.
func TestSetupWorkflowPhaseToolsForRestoreSkipsNonWorkflowChats(t *testing.T) {
	api := &StreamingAPI{}
	ag := &mcpagent.Agent{}
	runtime := &ChatHistoryAgentRuntime{
		Kind:     "coding_agent",
		Provider: "cursor-cli",
		// WorkspacePath, WorkshopMode, PhaseID all empty: not a workflow chat
	}

	before := len(ag.GetCustomTools())
	if err := api.setupWorkflowPhaseToolsForRestore(context.Background(), "session-1", "user-1", runtime, ag); err != nil {
		t.Fatalf("setupWorkflowPhaseToolsForRestore returned unexpected error: %v", err)
	}
	after := len(ag.GetCustomTools())
	if before != after {
		t.Fatalf("custom tool count changed for non-workflow chat: before=%d after=%d", before, after)
	}
}

// TestSetupWorkflowPhaseToolsForRestoreSkipsWhenWorkspacePathEmpty covers a
// related skip path: PhaseID is set but the runtime has no WorkspacePath — the
// helper has nothing to read a manifest against, so it must return cleanly
// rather than crash on an empty path.
func TestSetupWorkflowPhaseToolsForRestoreSkipsWhenWorkspacePathEmpty(t *testing.T) {
	api := &StreamingAPI{}
	ag := &mcpagent.Agent{}
	runtime := &ChatHistoryAgentRuntime{
		Kind:    "coding_agent",
		PhaseID: "workflow-builder",
	}

	if err := api.setupWorkflowPhaseToolsForRestore(context.Background(), "session-1", "user-1", runtime, ag); err != nil {
		t.Fatalf("setupWorkflowPhaseToolsForRestore returned unexpected error: %v", err)
	}
}

// TestSetupWorkflowPhaseToolsForRestoreNoOpForNilInputs guards against a NPE
// path: a nil runtime or nil agent should be a no-op, not a panic, because the
// caller's defensive checks could legitimately race.
func TestSetupWorkflowPhaseToolsForRestoreNoOpForNilInputs(t *testing.T) {
	api := &StreamingAPI{}
	if err := api.setupWorkflowPhaseToolsForRestore(context.Background(), "s", "u", nil, &mcpagent.Agent{}); err != nil {
		t.Fatalf("nil runtime returned error: %v", err)
	}
	if err := api.setupWorkflowPhaseToolsForRestore(context.Background(), "s", "u", &ChatHistoryAgentRuntime{}, nil); err != nil {
		t.Fatalf("nil agent returned error: %v", err)
	}
}

// TestCaptureChatHistoryAgentRuntimePersistsPhaseID verifies the chat-history
// runtime persists the phase ID so the auto-restore path can replay
// phase-specific tool registration before the CLI launches. Without this the
// restore helper has no signal to decide whether to register
// run_full_workflow / execute_step / etc.
func TestCaptureChatHistoryAgentRuntimePersistsPhaseID(t *testing.T) {
	api := &StreamingAPI{}
	runtime := api.captureChatHistoryAgentRuntime(
		"session-phase",
		"agy-cli",
		"agy-cli",
		"Workflow/example",
		"workflow-builder",
		&mcpagent.Agent{AgySessionID: "agy-conv-1"},
	)
	if runtime == nil {
		t.Fatal("expected runtime metadata")
	}
	if runtime.PhaseID != "workflow-builder" {
		t.Fatalf("PhaseID = %q, want workflow-builder", runtime.PhaseID)
	}

	// And: it survives JSON round-trip (this is how chat_history is persisted
	// and read back at restore time).
	data, err := json.Marshal(runtime)
	if err != nil {
		t.Fatalf("marshal runtime: %v", err)
	}
	var roundTrip ChatHistoryAgentRuntime
	if err := json.Unmarshal(data, &roundTrip); err != nil {
		t.Fatalf("unmarshal runtime: %v", err)
	}
	if roundTrip.PhaseID != "workflow-builder" {
		t.Fatalf("round-trip PhaseID = %q, want workflow-builder; raw=%s", roundTrip.PhaseID, string(data))
	}
}

// TestCaptureChatHistoryAgentRuntimeOmitsPhaseIDWhenEmpty makes sure the new
// field doesn't leak as "" into JSON for non-phase chats — the field is
// marked omitempty so legacy consumers see no extra key.
func TestCaptureChatHistoryAgentRuntimeOmitsPhaseIDWhenEmpty(t *testing.T) {
	api := &StreamingAPI{}
	runtime := api.captureChatHistoryAgentRuntime(
		"chat-session",
		"claude-code",
		"claude-code",
		"_users/default/Chats",
		"",
		&mcpagent.Agent{ClaudeCodeSessionID: "claude-1"},
	)
	if runtime == nil {
		t.Fatal("expected runtime")
	}
	if runtime.PhaseID != "" {
		t.Fatalf("PhaseID = %q, want empty for non-phase chat", runtime.PhaseID)
	}
	data, err := json.Marshal(runtime)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, present := raw["phase_id"]; present {
		t.Fatalf("phase_id key present in JSON output for empty value: %s", string(data))
	}
}

// TestSetupWorkflowPhaseToolsForRestoreInfersWorkflowBuilderForLegacyRuntime
// verifies the inference rule for legacy chat-history saves that pre-date the
// PhaseID field. If the runtime has WorkspacePath + WorkshopMode set, the
// helper should treat it as workflow-builder (the only phase that auto-restores
// via the tmux launch path today). The test exercises this by pointing the
// helper at a stub workspace API and confirming the manifest read happens
// (i.e. the inference path advances past the skip guard).
func TestSetupWorkflowPhaseToolsForRestoreInfersWorkflowBuilderForLegacyRuntime(t *testing.T) {
	const workspacePath = "Workflow/test-phase-restore"
	ws := &mockWorkspaceAPI{files: map[string]string{
		// Minimal manifest so ReadWorkflowManifest returns found=true. The
		// helper only reads capabilities (servers, secrets, LLM config); none
		// of those are required to be populated for the inference test.
		workspacePath + "/workflow.json": `{
  "schema_version": 1,
  "id": "test-phase-restore",
  "label": "Test phase restore",
  "capabilities": {
    "selected_servers": [],
    "selected_tools": [],
    "selected_skills": [],
    "selected_secrets": [],
    "browser_mode": "",
    "use_code_execution_mode": true
  },
  "execution_defaults": {},
  "ownership": {},
  "schedules": []
}`,
	}}
	server := httptest.NewServer(ws)
	defer server.Close()
	t.Setenv("WORKSPACE_API_URL", server.URL)

	api := &StreamingAPI{}
	ag := &mcpagent.Agent{}
	runtime := &ChatHistoryAgentRuntime{
		Kind:          "coding_agent",
		Provider:      "agy-cli",
		WorkspacePath: workspacePath,
		WorkshopMode:  "workshop",
		// PhaseID intentionally empty — exercises the legacy inference path.
	}

	// The helper may surface an error or panic from buildWorkshopConfig /
	// NewWorkshopChatSession in the test environment (no real LLM keys, no
	// MCP server config). What we're asserting here is that it gets PAST the
	// skip guard — i.e. it attempted the registration, not that the
	// registration fully succeeded. Catch any downstream panic so we can
	// still inspect the agent's custom-tool map below.
	func() {
		defer func() { _ = recover() }()
		_ = api.setupWorkflowPhaseToolsForRestore(context.Background(), "session-legacy", "user-test", runtime, ag)
	}()

	// Verify the helper progressed past the skip guard. Plan modification
	// tools register first in installWorkflowPhaseTools' switch arm and don't
	// depend on workshop config — they should land in the agent's custom tool
	// map even when downstream workshop-session setup blows up.
	tools := ag.GetCustomTools()
	if len(tools) == 0 {
		t.Fatalf("no custom tools registered — helper appears to have short-circuited the legacy inference path")
	}
}
