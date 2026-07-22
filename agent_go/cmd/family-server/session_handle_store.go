package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/manishiitg/coding-agent-loop/agent_go/internal/agentsession"
)

// Session-handle persistence — the durable, cross-restart context mechanism,
// mirroring AgentWorks. After each turn the coding agent yields a provider-native
// continuation handle (for Claude Code: its own `--resume` session UUID). We
// write it to disk beside the conversation and reload it on the next turn, so a
// fresh process resumes the CLI's own on-disk session and gets full prior context
// WITHOUT replaying the transcript. The warm tmux session is only a same-process
// speed path and dies on restart; this file is what survives it.
//
// Stored at <scope>/conversations/<id>.session.json (same folder + id
// sanitization as the conversation transcript itself), one handle per
// conversation. Best-effort: a persistence hiccup must never break a reply, so
// all failures are swallowed — worst case is one turn re-establishing a handle.

func sessionHandlePath(scope, id string) (string, bool) {
	id = strings.TrimSpace(id)
	if id == "" || (scope != "parent" && scope != "child") {
		return "", false
	}
	id = strings.NewReplacer("/", "_", "\\", "_", "..", "_").Replace(id)
	dir := filepath.Join(workspaceRoot(), scope, "conversations")
	return filepath.Join(dir, id+".session.json"), true
}

// loadSessionHandle reads the persisted continuation handle for a conversation,
// or nil if none exists yet (brand-new conversation, or handle not captured).
func loadSessionHandle(scope, id string) *agentsession.Handle {
	path, ok := sessionHandlePath(scope, id)
	if !ok {
		return nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var h agentsession.Handle
	if json.Unmarshal(b, &h) != nil || h.Empty() {
		return nil
	}
	return &h
}

// saveSessionHandle persists the continuation handle captured after a turn. A nil
// or empty handle is ignored (keeps any previously-saved good handle rather than
// clobbering it — the provider occasionally returns an empty handle on a turn
// that did no coding-agent work).
func saveSessionHandle(scope, id string, h *agentsession.Handle) {
	if h == nil || h.Empty() {
		return
	}
	path, ok := sessionHandlePath(scope, id)
	if !ok {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	b, err := json.MarshalIndent(h, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(path, b, 0o600)
}
