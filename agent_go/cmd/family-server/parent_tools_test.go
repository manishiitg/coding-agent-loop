package main

import (
	"strings"
	"testing"
)

// wantParentTools is the canonical Parent-Mode manifest, in the order
// parentTools builds it. This list is the contract every parent-scope surface
// (web chat, WhatsApp bot, direct WhatsApp, Pulse) shares.
var wantParentTools = []string{
	"set_child_profile",
	"set_parent_label",
	"open_file",
	"open_activity",
	"create_learning_activity",
	"suggest_actions",
	"web_search",
	"read_image",
	"generate_image",
	"notify_user",
	"execute_shell_command",
	"diff_patch_workspace_file",
	"agent_browser",
	"send_whatsapp_file",
	"list_secrets",
	"set_secret",
}

// TestParentToolsManifestIsComplete pins the shared manifest. Before
// parent_tools.go existed, each parent surface passed its own hand-written
// list (16 / 6 / 4 / 2-7 tools) into what is the SAME warm "parent" session,
// so whichever surface happened to launch the session silently decided what
// tools every other surface got. This test is the guard that they stay one
// list.
func TestParentToolsManifestIsComplete(t *testing.T) {
	got := parentTools("claude-code", "Ada", parentToolSinks{})

	if len(got) != len(wantParentTools) {
		t.Fatalf("parentTools returned %d tools, want %d", len(got), len(wantParentTools))
	}
	seen := map[string]bool{}
	for i, tool := range got {
		if tool.Name != wantParentTools[i] {
			t.Errorf("tool[%d] = %q, want %q", i, tool.Name, wantParentTools[i])
		}
		if seen[tool.Name] {
			t.Errorf("tool %q registered twice — a duplicate name in one manifest is a bridge-registration bug", tool.Name)
		}
		seen[tool.Name] = true
		if tool.Handler == nil {
			t.Errorf("tool %q has a nil Handler", tool.Name)
		}
	}
}

// TestParentToolsTolerateNilSinks proves the property that makes ONE shared
// manifest possible: a surface with nowhere to render suggestions or track
// sent files (WhatsApp, Pulse) passes an empty parentToolSinks and every
// handler must still run without panicking. If this regressed, those surfaces
// would be pushed back toward registering a narrower list — reintroducing the
// exact bug this file fixes.
func TestParentToolsTolerateNilSinks(t *testing.T) {
	sinks := parentToolSinks{}
	// Direct sink calls — the paths every tool handler funnels through.
	sinks.event(toolEvent{Tool: "open_file", Path: "x"})
	sinks.suggestions([]suggestion{{Label: "a", Message: "b"}})
	sinks.sentFile("/tmp/x.pdf")
	sinks.secretSet("NAME", "value")

	// suggest_actions is the one whose whole purpose is a sink write — run its
	// real handler with no sink attached and confirm it reports success rather
	// than panicking.
	for _, tool := range parentTools("claude-code", "Ada", sinks) {
		if tool.Name != "suggest_actions" {
			continue
		}
		out, err := tool.Handler(t.Context(), map[string]interface{}{
			"actions": []interface{}{
				map[string]interface{}{"label": "Do a thing", "message": "please do a thing"},
			},
		})
		if err != nil {
			t.Fatalf("suggest_actions with nil sink returned error: %v", err)
		}
		if !strings.Contains(out, `"count":1`) {
			t.Errorf("suggest_actions output = %q, want it to report count 1", out)
		}
		return
	}
	t.Fatal("suggest_actions not found in the parent manifest")
}

// TestParentSystemPromptAdvertisesExactlyTheManifest catches drift in the
// direction that is otherwise invisible: the system prompt tells the model
// which tools it has by NAME, so a tool added to (or removed from) the
// manifest without updating that sentence leaves the model either unaware of a
// real tool or confidently calling one that does not exist.
func TestParentSystemPromptAdvertisesExactlyTheManifest(t *testing.T) {
	prompt := parentSystemPrompt(nil, "", PulseConfig{})

	const marker = "Your tools — "
	idx := strings.Index(prompt, marker)
	if idx < 0 {
		t.Fatalf("parent system prompt no longer contains the %q tool-list sentence", marker)
	}
	rest := prompt[idx+len(marker):]
	end := strings.Index(rest, " — are already natively available")
	if end < 0 {
		t.Fatal("parent system prompt tool-list sentence is not in the expected form")
	}
	advertised := map[string]bool{}
	for _, name := range strings.Split(rest[:end], ",") {
		if n := strings.TrimSpace(name); n != "" {
			advertised[n] = true
		}
	}

	for _, tool := range parentTools("claude-code", "Ada", parentToolSinks{}) {
		if !advertised[tool.Name] {
			t.Errorf("tool %q is in the manifest but NOT advertised in the parent system prompt — the model won't know it exists", tool.Name)
		}
		delete(advertised, tool.Name)
	}
	for name := range advertised {
		t.Errorf("parent system prompt advertises %q but it is NOT in the manifest — the model may call a tool that doesn't exist", name)
	}
}
