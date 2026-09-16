package server

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/common"
)

func TestProtectOtherWorkflowBuilderChatsBlocksSiblingAndLegacyOwners(t *testing.T) {
	root := t.TempDir()
	t.Setenv("WORKSPACE_DOCS_PATH", root)
	const sessionID = "private-builder-guard"
	t.Cleanup(func() { common.ClearSessionShellConfig(sessionID) })

	base := filepath.Join(root, "Workflow", "demo", "builder", "conversation")
	for _, user := range []string{"alice", "bob"} {
		if err := os.MkdirAll(filepath.Join(base, "users", user, "2026-09-16"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	legacyDir := filepath.Join(base, "2026-09-15")
	if err := os.MkdirAll(legacyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacyDir, "session-alice-conversation.json"), []byte(`{"user_id":"alice"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacyDir, "session-bob-conversation.json"), []byte(`{"user_id":"bob"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	common.SetSessionFolderGuard(sessionID, []string{"Workflow/demo"}, []string{"Workflow/demo"})
	common.SetSessionFolderGuardBlockedPaths(sessionID, []string{"Workflow/demo/secrets"})
	protectOtherWorkflowBuilderChats(sessionID, "Workflow/demo", "alice")

	blocked := common.GetSessionShellConfig(sessionID).BlockedPaths
	for _, want := range []string{
		"Workflow/demo/secrets",
		"Workflow/demo/builder/conversation/system",
		"Workflow/demo/builder/conversation/users/bob",
		"Workflow/demo/builder/conversation/2026-09-15/session-bob-conversation.json",
	} {
		if !slices.Contains(blocked, want) {
			t.Fatalf("missing blocked path %q in %#v", want, blocked)
		}
	}
	if slices.Contains(blocked, "Workflow/demo/builder/conversation/users/alice") ||
		slices.Contains(blocked, "Workflow/demo/builder/conversation/2026-09-15/session-alice-conversation.json") {
		t.Fatalf("current user's transcript was blocked: %#v", blocked)
	}
}
