package main

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/manishiitg/coding-agent-loop/agent_go/internal/agentsession"
	"github.com/manishiitg/mcpagent/llm"
)

// Session-handle persistence — the durable, cross-restart context mechanism,
// mirroring AgentWorks. After each turn the coding agent yields a provider-native
// continuation handle (for Claude Code: its own `--resume` session UUID). We
// write it to disk beside the conversation and reload it on the next turn, so a
// fresh process resumes the CLI's own on-disk session and gets full prior context
// WITHOUT replaying the transcript. The warm tmux session is only a same-process
// speed path and dies on restart; this file is what survives it.
//
// Stored alongside the conversation transcript itself (see
// conversationLocation in conversation_store.go) as <base>.session.json, one
// handle per conversation. Best-effort: a persistence hiccup must never break
// a reply, so all failures are swallowed — worst case is one turn
// re-establishing a handle.

func sessionHandlePath(scope, id string) (string, bool) {
	dirRel, base, ok := conversationLocation(scope, id)
	if !ok {
		return "", false
	}
	dir, ok := resolveWorkspacePath(dirRel)
	if !ok {
		return "", false
	}
	return filepath.Join(dir, base+".session.json"), true
}

// loadSessionHandle reads the persisted continuation handle for a conversation,
// or nil if none exists yet (brand-new conversation, or handle not captured).
//
// provider is the engine THIS turn is about to run. A handle is provider-native
// — it carries e.g. a Claude Code `--resume` UUID or a Codex thread id — so a
// handle written by a different engine is meaningless (at best) to the current
// one and is dropped here rather than passed down.
//
// This matters because the parent can switch engines at any time while the
// conversation id stays the same, so the stored handle simply does not follow.
// Passing a foreign handle down is actively harmful, not merely useless:
// mcpagent's ApplyAgentSessionHandle applies the handle's native session id via
// the OLD provider's setter and then RELABELS the handle as belonging to the
// NEW provider (agent/session_handle.go — it overwrites
// CodingProviderSessionHandle.Provider with the configured provider so a role
// change can keep one conversation). The handle saved at the end of that turn
// can therefore claim provider="codex-cli" while still carrying a Claude UUID —
// and the NEXT turn feeds that UUID to `codex exec resume`, which cannot
// resolve it. Dropping the mismatch here keeps that poisoned state from ever
// being written: the new engine cold-starts once (correct — it genuinely has no
// prior session) and saves its own valid handle.
func loadSessionHandle(scope, id string, provider llm.Provider) *agentsession.Handle {
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
	if stored := strings.TrimSpace(h.Provider.Provider); stored != "" && stored != string(provider) {
		log.Printf("[session-handle] dropping %s/%s handle from engine %q — this turn runs %q (engine changed; a provider-native handle does not carry over)", scope, id, stored, provider)
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
